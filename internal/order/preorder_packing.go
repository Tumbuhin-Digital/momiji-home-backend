package order

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/warehouse"
)

func BuildDefaultPackingDTO(items []OrderItem) []PackingItemDTO {
	packing := make([]PackingItemDTO, 0, len(items))
	for _, it := range items {
		packing = append(packing, PackingItemDTO{
			LineItemID: it.ID,
			Quantity:   it.Quantity,
			BoxCount:   it.Quantity,
			IsNested:   false,
		})
	}
	return packing
}

func PackingToDBItems(packing []PackingItemDTO) []PreorderPackingItem {
	dbPacking := make([]PreorderPackingItem, 0, len(packing))
	for _, p := range packing {
		boxCount := p.BoxCount
		if p.IsNested {
			boxCount = 0
		}
		qty := p.Quantity
		dbPacking = append(dbPacking, PreorderPackingItem{
			OrderLineItemID: p.LineItemID,
			Quantity:        qty,
			BoxCount:        boxCount,
			IsNested:        p.IsNested,
		})
	}
	return dbPacking
}

// packingQty returns the slice quantity for packing validation/rate calc.
// Prefer explicit PackingItemDTO.Quantity; otherwise fall back to the line item quantity.
func packingQty(p PackingItemDTO, it OrderItem) int {
	if p.Quantity > 0 {
		return p.Quantity
	}
	return it.Quantity
}

func ValidatePacking(packing []PackingItemDTO, preItems []OrderItem) error {
	itemMap := make(map[string]OrderItem, len(preItems))
	for _, it := range preItems {
		itemMap[it.ID] = it
	}
	for _, p := range packing {
		it, ok := itemMap[p.LineItemID]
		if !ok {
			return apierror.New(http.StatusBadRequest, "invalid_request", fmt.Sprintf("Line item %s not found on order", p.LineItemID))
		}
		qty := packingQty(p, it)
		if qty <= 0 {
			return apierror.New(http.StatusBadRequest, "invalid_request", fmt.Sprintf("quantity must be positive for line item %s", p.LineItemID))
		}
		if qty > it.Quantity {
			return apierror.New(http.StatusBadRequest, "invalid_request", fmt.Sprintf("packing quantity cannot exceed line quantity for line item %s", p.LineItemID))
		}
		if p.IsNested && p.BoxCount != 0 {
			return apierror.New(http.StatusBadRequest, "invalid_request", "Nested items must have box_count 0")
		}
		if !p.IsNested && p.BoxCount < 0 {
			return apierror.New(http.StatusBadRequest, "invalid_request", "box_count cannot be negative")
		}
		if !p.IsNested && p.BoxCount > 0 {
			if p.BoxCount > qty {
				return apierror.New(http.StatusBadRequest, "invalid_request", fmt.Sprintf("box_count cannot exceed quantity for line item %s", p.LineItemID))
			}
			if qty%p.BoxCount != 0 {
				return apierror.New(http.StatusBadRequest, "invalid_request", fmt.Sprintf("box_count must evenly divide quantity for line item %s", p.LineItemID))
			}
		}
	}
	return nil
}

func unitsPerBox(quantity, boxCount int) (int, error) {
	if boxCount <= 0 {
		return 0, fmt.Errorf("box_count must be positive")
	}
	if quantity%boxCount != 0 {
		return 0, fmt.Errorf("quantity must be evenly divisible by box_count")
	}
	return quantity / boxCount, nil
}

// BuildPackableUnits converts packing plan + variant dimensions into ShipStation-ready units.
//
// Consolidation (box_count < quantity): weight per box = unit_weight × units_per_box.
// Dimensions per consolidated box assume identical SKU stacked vertically:
//   - DepthCm, WidthCm: single-unit footprint (max footprint, no lateral expansion)
//   - HeightCm: unit height × units_per_box (vertical stack)
//
// Nested items: weight is pooled and distributed evenly across all non-nested physical boxes.
func BuildPackableUnits(orderID string, packing []PackingItemDTO, itemMap map[string]OrderItem, dims map[string]VariantDimensions) ([]shipping.PackableUnit, error) {
	var units []shipping.PackableUnit
	var totalNestedKg float64
	var nestedLineIDs []string
	totalPhysicalBoxes := 0

	for _, p := range packing {
		it := itemMap[p.LineItemID]
		d := dims[it.ShopifyVariantID]
		qty := packingQty(p, it)

		if p.IsNested {
			totalNestedKg += d.WeightKg * float64(qty)
			nestedLineIDs = append(nestedLineIDs, p.LineItemID)
			continue
		}

		if p.BoxCount <= 0 {
			continue
		}

		perBox, err := unitsPerBox(qty, p.BoxCount)
		if err != nil {
			return nil, apierror.New(http.StatusBadRequest, "invalid_request", fmt.Sprintf("invalid packing for line item %s: %v", p.LineItemID, err))
		}

		units = append(units, shipping.PackableUnit{
			WeightKg:         d.WeightKg * float64(perBox),
			WidthCm:          d.WidthCm,
			HeightCm:         d.HeightCm * float64(perBox),
			DepthCm:          d.DepthCm,
			BoxCount:         p.BoxCount,
			LineItemID:       p.LineItemID,
			SKU:              d.SKU,
			ShopifyVariantID: d.ShopifyVariantID,
		})
		totalPhysicalBoxes += p.BoxCount
	}

	if totalNestedKg > 0 && totalPhysicalBoxes == 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("order %s: nested items require at least one shippable box (nested_line_item_ids=%s)", orderID, strings.Join(nestedLineIDs, ",")),
		)
	}

	if totalNestedKg > 0 && totalPhysicalBoxes > 0 {
		extraPerBox := totalNestedKg / float64(totalPhysicalBoxes)
		for i := range units {
			units[i].WeightKg += extraPerBox
		}
	}

	return units, nil
}

func PackingTotals(units []shipping.PackableUnit) (totalBoxes int, totalWeight float64) {
	return shipping.TotalBoxes(units), shipping.TotalWeightLb(units)
}

// BuildCheckoutPreorderShipment creates the initial shipment record from checkout estimate and default packing.
func BuildCheckoutPreorderShipment(orderID string, preItems []OrderItem, dims map[string]VariantDimensions, estimate *float64, warehouseOrigin string) (*PreorderShipment, []PreorderPackingItem, error) {
	packing := BuildDefaultPackingDTO(preItems)
	return buildPreorderShipmentFromPacking(orderID, packing, preItems, dims, estimate, warehouseOrigin)
}

func buildPreorderShipmentFromPacking(orderID string, packing []PackingItemDTO, preItems []OrderItem, dims map[string]VariantDimensions, estimate *float64, warehouseOrigin string) (*PreorderShipment, []PreorderPackingItem, error) {
	itemMap := make(map[string]OrderItem, len(preItems))
	for _, it := range preItems {
		itemMap[it.ID] = it
	}
	units, err := BuildPackableUnits(orderID, packing, itemMap, dims)
	if err != nil {
		return nil, nil, err
	}
	totalBoxes, totalWeight := PackingTotals(units)

	shipment := &PreorderShipment{
		OrderID:         orderID,
		TotalBoxes:      totalBoxes,
		WarehouseOrigin: warehouse.NormalizeOrigin(context.Background(), warehouseOrigin, slog.String("order_id", orderID)),
	}
	if estimate != nil {
		shipment.EstimatedShipping = estimate
	}
	if totalWeight > 0 {
		shipment.TotalWeightLb = &totalWeight
	}
	return shipment, PackingToDBItems(packing), nil
}

func resolveWarehouseOrigin(shipment *PreorderShipment) string {
	if shipment != nil && shipment.WarehouseOrigin != "" {
		return warehouse.NormalizeCode(shipment.WarehouseOrigin)
	}
	return warehouse.CodeEast
}
