package order

import (
	"fmt"
	"net/http"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/warehouse"
)

func BuildDefaultPackingDTO(items []OrderItem) []PackingItemDTO {
	packing := make([]PackingItemDTO, 0, len(items))
	for _, it := range items {
		packing = append(packing, PackingItemDTO{
			LineItemID: it.ID,
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
		dbPacking = append(dbPacking, PreorderPackingItem{
			OrderLineItemID: p.LineItemID,
			BoxCount:        boxCount,
			IsNested:        p.IsNested,
		})
	}
	return dbPacking
}

func ValidatePacking(packing []PackingItemDTO, preItems []OrderItem) error {
	itemMap := make(map[string]OrderItem, len(preItems))
	for _, it := range preItems {
		itemMap[it.ID] = it
	}
	for _, p := range packing {
		if _, ok := itemMap[p.LineItemID]; !ok {
			return apierror.New(http.StatusBadRequest, "invalid_request", fmt.Sprintf("Line item %s not found on order", p.LineItemID))
		}
		if p.IsNested && p.BoxCount != 0 {
			return apierror.New(http.StatusBadRequest, "invalid_request", "Nested items must have box_count 0")
		}
		if !p.IsNested && p.BoxCount < 0 {
			return apierror.New(http.StatusBadRequest, "invalid_request", "box_count cannot be negative")
		}
		if !p.IsNested && p.BoxCount > 0 {
			it := itemMap[p.LineItemID]
			if p.BoxCount > it.Quantity {
				return apierror.New(http.StatusBadRequest, "invalid_request", fmt.Sprintf("box_count cannot exceed quantity for line item %s", p.LineItemID))
			}
			if it.Quantity%p.BoxCount != 0 {
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
func BuildPackableUnits(packing []PackingItemDTO, itemMap map[string]OrderItem, dims map[string]VariantDimensions) ([]shipping.PackableUnit, error) {
	var units []shipping.PackableUnit
	var totalNestedKg float64
	totalPhysicalBoxes := 0

	for _, p := range packing {
		it := itemMap[p.LineItemID]
		d := dims[it.ShopifyVariantID]

		if p.IsNested {
			totalNestedKg += d.WeightKg * float64(it.Quantity)
			continue
		}

		if p.BoxCount <= 0 {
			continue
		}

		perBox, err := unitsPerBox(it.Quantity, p.BoxCount)
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
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "nested items require at least one shippable box")
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
func BuildCheckoutPreorderShipment(orderID string, preItems []OrderItem, dims map[string]VariantDimensions, estimate *float64, warehouseOrigin string) (*PreorderShipment, []PreorderPackingItem) {
	packing := BuildDefaultPackingDTO(preItems)
	itemMap := make(map[string]OrderItem, len(preItems))
	for _, it := range preItems {
		itemMap[it.ID] = it
	}
	units, _ := BuildPackableUnits(packing, itemMap, dims)
	totalBoxes, totalWeight := PackingTotals(units)

	shipment := &PreorderShipment{
		OrderID:         orderID,
		TotalBoxes:      totalBoxes,
		WarehouseOrigin: warehouse.NormalizeCode(warehouseOrigin),
	}
	if estimate != nil {
		shipment.EstimatedShipping = estimate
	}
	if totalWeight > 0 {
		shipment.TotalWeightLb = &totalWeight
	}
	return shipment, PackingToDBItems(packing)
}

func resolveWarehouseOrigin(shipment *PreorderShipment) string {
	if shipment != nil && shipment.WarehouseOrigin != "" {
		return warehouse.NormalizeCode(shipment.WarehouseOrigin)
	}
	return warehouse.CodeEast
}
