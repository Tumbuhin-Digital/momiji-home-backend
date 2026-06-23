package order

import (
	"fmt"
	"net/http"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
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
	}
	return nil
}

func BuildPackableUnits(packing []PackingItemDTO, itemMap map[string]OrderItem, dims map[string]VariantDimensions) []shipping.PackableUnit {
	var units []shipping.PackableUnit
	for _, p := range packing {
		it := itemMap[p.LineItemID]
		d := dims[it.ShopifyVariantID]
		boxCount := p.BoxCount
		if p.IsNested {
			boxCount = 0
		}
		units = append(units, shipping.PackableUnit{
			WeightKg: d.WeightKg,
			WidthCm:  d.WidthCm,
			HeightCm: d.HeightCm,
			DepthCm:  d.DepthCm,
			BoxCount: boxCount,
		})
	}
	return units
}

func PackingTotals(units []shipping.PackableUnit) (totalBoxes int, totalWeight float64) {
	return shipping.TotalBoxes(units), shipping.TotalWeightLb(units)
}

// BuildCheckoutPreorderShipment creates the initial shipment record from checkout estimate and default packing.
func BuildCheckoutPreorderShipment(orderID string, preItems []OrderItem, dims map[string]VariantDimensions, estimate *float64) (*PreorderShipment, []PreorderPackingItem) {
	packing := BuildDefaultPackingDTO(preItems)
	itemMap := make(map[string]OrderItem, len(preItems))
	for _, it := range preItems {
		itemMap[it.ID] = it
	}
	units := BuildPackableUnits(packing, itemMap, dims)
	totalBoxes, totalWeight := PackingTotals(units)

	shipment := &PreorderShipment{
		OrderID:    orderID,
		TotalBoxes: totalBoxes,
	}
	if estimate != nil {
		shipment.EstimatedShipping = estimate
	}
	if totalWeight > 0 {
		shipment.TotalWeightLb = &totalWeight
	}
	return shipment, PackingToDBItems(packing)
}
