package order_test

import (
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/order"
)

func dims(weight float64) map[string]order.VariantDimensions {
	return map[string]order.VariantDimensions{
		"var-1": {ShopifyVariantID: "var-1", SKU: "SKU-1", WeightKg: weight, WidthCm: 40, HeightCm: 20, DepthCm: 30},
		"var-2": {ShopifyVariantID: "var-2", SKU: "SKU-2", WeightKg: 2, WidthCm: 10, HeightCm: 10, DepthCm: 10},
	}
}

func TestBuildPackableUnits_ConsolidationSingleBox(t *testing.T) {
	itemMap := map[string]order.OrderItem{
		"line-1": {ID: "line-1", ShopifyVariantID: "var-1", Quantity: 3},
	}
	packing := []order.PackingItemDTO{{LineItemID: "line-1", BoxCount: 1, IsNested: false}}

	units, err := order.BuildPackableUnits(packing, itemMap, dims(5))
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}
	if units[0].WeightKg != 15 {
		t.Fatalf("expected 15 kg per box, got %f", units[0].WeightKg)
	}
	if units[0].BoxCount != 1 {
		t.Fatalf("expected 1 box, got %d", units[0].BoxCount)
	}
	if units[0].HeightCm != 60 {
		t.Fatalf("expected stacked height 60 cm, got %f", units[0].HeightCm)
	}
}

func TestBuildPackableUnits_OneUnitPerBox(t *testing.T) {
	itemMap := map[string]order.OrderItem{
		"line-1": {ID: "line-1", ShopifyVariantID: "var-1", Quantity: 3},
	}
	packing := []order.PackingItemDTO{{LineItemID: "line-1", BoxCount: 3, IsNested: false}}

	units, err := order.BuildPackableUnits(packing, itemMap, dims(5))
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("expected 1 unit entry, got %d", len(units))
	}
	if units[0].WeightKg != 5 {
		t.Fatalf("expected 5 kg per box, got %f", units[0].WeightKg)
	}
	if units[0].BoxCount != 3 {
		t.Fatalf("expected 3 boxes, got %d", units[0].BoxCount)
	}
}

func TestBuildPackableUnits_NestedWeightDistributed(t *testing.T) {
	itemMap := map[string]order.OrderItem{
		"line-1": {ID: "line-1", ShopifyVariantID: "var-1", Quantity: 2},
		"line-2": {ID: "line-2", ShopifyVariantID: "var-2", Quantity: 2},
	}
	packing := []order.PackingItemDTO{
		{LineItemID: "line-1", BoxCount: 2, IsNested: false},
		{LineItemID: "line-2", BoxCount: 0, IsNested: true},
	}
	d := dims(5)
	d["var-2"] = order.VariantDimensions{ShopifyVariantID: "var-2", WeightKg: 2, WidthCm: 10, HeightCm: 10, DepthCm: 10}

	units, err := order.BuildPackableUnits(packing, itemMap, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}
	// 2 boxes @ 5kg + nested 4kg total → +2kg per box → 7kg each
	if units[0].WeightKg != 7 {
		t.Fatalf("expected 7 kg per box, got %f", units[0].WeightKg)
	}
}

func TestBuildPackableUnits_NestedOnlyNoBoxes(t *testing.T) {
	itemMap := map[string]order.OrderItem{
		"line-1": {ID: "line-1", ShopifyVariantID: "var-2", Quantity: 1},
	}
	packing := []order.PackingItemDTO{{LineItemID: "line-1", BoxCount: 0, IsNested: true}}

	_, err := order.BuildPackableUnits(packing, itemMap, dims(5))
	if err == nil {
		t.Fatal("expected error for nested-only packing")
	}
}

func TestValidatePacking_BoxCountMustDivideQuantity(t *testing.T) {
	items := []order.OrderItem{{ID: "line-1", Quantity: 3}}
	packing := []order.PackingItemDTO{{LineItemID: "line-1", BoxCount: 2, IsNested: false}}

	err := order.ValidatePacking(packing, items)
	if err == nil {
		t.Fatal("expected validation error")
	}
}
