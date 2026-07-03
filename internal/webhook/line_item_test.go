package webhook

import "testing"

func TestIsPreorderShopifyLineItem_mixedCart(t *testing.T) {
	preorderCapableVariant := ShopifyOrderLineItem{
		VariantID: 123,
		Title:     "Petal Chair",
		Quantity:  10,
		Price:     "77.80",
	}

	shipReadyLine := preorderCapableVariant
	shipReadyLine.Properties = nil

	preorderLine := preorderCapableVariant
	preorderLine.Title = "[PREORDER] Petal Chair (Deposit 50%)"
	preorderLine.Price = "38.90"
	preorderLine.Properties = []ShopifyProperty{
		{Name: "type", Value: "preorder_dp"},
		{Name: "full_price", Value: "77.80"},
	}

	if isPreorderShopifyLineItem(shipReadyLine) {
		t.Fatal("ship-ready line without preorder_dp must not be classified as preorder")
	}
	if !isPreorderShopifyLineItem(preorderLine) {
		t.Fatal("preorder line with preorder_dp must be classified as preorder")
	}
}

func TestIsPreorderShopifyLineItem_preorderOnlyProduct(t *testing.T) {
	line := ShopifyOrderLineItem{
		Properties: []ShopifyProperty{
			{Name: "type", Value: "preorder_dp"},
		},
	}
	if !isPreorderShopifyLineItem(line) {
		t.Fatal("expected preorder when type=preorder_dp")
	}
}

func TestIsPreorderShopifyLineItem_shipReadyOnly(t *testing.T) {
	line := ShopifyOrderLineItem{Properties: nil}
	if isPreorderShopifyLineItem(line) {
		t.Fatal("expected ship-ready when no preorder_dp property")
	}
}
