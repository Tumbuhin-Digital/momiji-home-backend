package order

import "testing"

import "github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"

func TestIsWebsiteDraft(t *testing.T) {
	if !isWebsiteDraft([]shopify.AttributeInput{shopify.WholesaleSourceAttribute}) {
		t.Fatal("expected wholesale source to count as a website draft")
	}
	if isWebsiteDraft([]shopify.AttributeInput{{Key: "source", Value: "shopify"}}) {
		t.Fatal("expected non-website draft to be excluded")
	}
	if isWebsiteDraft(nil) {
		t.Fatal("expected draft without attributes to be excluded")
	}
}

func TestWebsiteDraftDetailRequiresWholesaleSource(t *testing.T) {
	_, err := websiteDraftDetail(nil)
	if err == nil {
		t.Fatal("expected missing draft to be rejected")
	}

	_, err = websiteDraftDetail(&shopify.DraftOrderDetail{
		ID:   "gid://shopify/DraftOrder/1",
		Name: "#D1",
	})
	if err == nil {
		t.Fatal("expected non-website draft to be rejected")
	}

	detail, err := websiteDraftDetail(&shopify.DraftOrderDetail{
		ID:               "gid://shopify/DraftOrder/2",
		Name:             "#D2",
		Status:           "OPEN",
		CurrencyCode:     "USD",
		Total:            "10.00",
		CustomAttributes: []shopify.AttributeInput{shopify.WholesaleSourceAttribute},
		LineItems: []shopify.DraftOrderLine{
			{Title: "Chair", Quantity: 1, UnitPrice: "10.00", LineTotal: "10.00"},
		},
	})
	if err != nil {
		t.Fatalf("expected website draft, got %v", err)
	}
	if detail.Name != "#D2" || len(detail.LineItems) != 1 {
		t.Fatalf("unexpected detail %+v", detail)
	}
}
