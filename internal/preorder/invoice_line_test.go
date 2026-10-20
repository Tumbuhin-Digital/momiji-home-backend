package preorder

import (
	"context"
	"reflect"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
)

func TestBuildGroupInvoiceDraftInput_TagsAndNotesTheDepositOrder(t *testing.T) {
	opts := repricedGroupOptions()
	opts.DepositOrderNumber = "1323"

	input := buildGroupInvoiceDraftInput(opts)

	wantTags := []string{shopify.TagPreorderBalance, "deposit-order-1323"}
	if !reflect.DeepEqual(input.Tags, wantTags) {
		t.Fatalf("tags = %v, want %v", input.Tags, wantTags)
	}
	wantNote := "Remaining 50% balance for order #1323. Shipping and tracking are recorded on order #1323."
	if input.Note != wantNote {
		t.Fatalf("note = %q, want %q", input.Note, wantNote)
	}
}

func TestBuildGroupInvoiceDraftInput_UnknownDepositOrderOnlyTagsBalance(t *testing.T) {
	input := buildGroupInvoiceDraftInput(repricedGroupOptions())

	if !reflect.DeepEqual(input.Tags, []string{shopify.TagPreorderBalance}) {
		t.Fatalf("tags = %v, want only %s", input.Tags, shopify.TagPreorderBalance)
	}
	if input.Note != "" {
		t.Fatalf("expected no note without a deposit order number, got %q", input.Note)
	}
}

func TestUpdateGroupSecondPaymentInvoice_KeepsAdminTagsOnTheDraft(t *testing.T) {
	draft := groupDraft("OPEN", "ship-1")
	draft.Tags = []string{"vip", shopify.TagPreorderBalance}
	shop := &draftEditingShopClient{draft: draft}
	svc := &service{shopClient: shop, emailService: &trackingEmailService{}}

	opts := repricedGroupOptions()
	opts.DepositOrderNumber = "1323"
	if _, err := svc.UpdateGroupSecondPaymentInvoice(context.Background(), draft.ID, opts); err != nil {
		t.Fatalf("UpdateGroupSecondPaymentInvoice returned error: %v", err)
	}

	want := []string{"vip", shopify.TagPreorderBalance, "deposit-order-1323"}
	if !reflect.DeepEqual(shop.updatedInput.Tags, want) {
		t.Fatalf("tags = %v, want %v", shop.updatedInput.Tags, want)
	}
}

func TestInvoiceSettlementsWithShipping_TagsBalanceOrder(t *testing.T) {
	store := &invoiceTestStore{settlements: map[string]Settlement{"settle-1": pendingSettlement()}}
	shop := &recordingShopClient{}
	svc := &service{store: store, shopClient: shop, emailService: noopEmailService{}}

	if _, err := svc.InvoiceSettlementsWithShipping(context.Background(), []string{"line-1"}, InvoiceOptions{}); err != nil {
		t.Fatalf("InvoiceSettlementsWithShipping returned error: %v", err)
	}
	if !reflect.DeepEqual(shop.lastDraftInput.Tags, []string{shopify.TagPreorderBalance}) {
		t.Fatalf("tags = %v, want only %s", shop.lastDraftInput.Tags, shopify.TagPreorderBalance)
	}
}

func TestCleanPreorderDisplayTitle(t *testing.T) {
	cases := map[string]string{
		"[PREORDER] Alaya House Shelf (Deposit 50%)": "Alaya House Shelf",
		"[PREORDER] Petal Chair (DP 50%)":            "Petal Chair",
		"Alaya House Shelf (Deposit 50%)":            "Alaya House Shelf",
		"[preorder]Rattan Chair":                     "Rattan Chair",
		"Rattan Chair":                               "Rattan Chair",
		"(Deposit 50%)":                              "(Deposit 50%)",
	}
	for in, want := range cases {
		if got := cleanPreorderDisplayTitle(in); got != want {
			t.Errorf("cleanPreorderDisplayTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildGroupInvoiceDraftInput_ProductTitleAndSKU(t *testing.T) {
	opts := GroupInvoiceOptions{
		CustomerEmail: "buyer@example.com",
		OrderID:       "order-1",
		ShipmentID:    "ship-1",
		Lines: []GroupInvoiceLine{
			{Title: "[PREORDER] Alaya House Shelf (Deposit 50%)", SKU: "MU-ALAYA", Amount: 149.50, OrderLineItemID: "line-1", Quantity: 1},
			{Title: "After the Rain Shelf (Deposit 50%)", SKU: " MU-RAIN ", Amount: 525, OrderLineItemID: "line-2", Quantity: 3},
		},
	}

	input := buildGroupInvoiceDraftInput(opts)
	if len(input.LineItems) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(input.LineItems))
	}

	want := []struct{ title, sku string }{
		{"Alaya House Shelf (Remaining 50%)", "MU-ALAYA"},
		{"After the Rain Shelf (Remaining 50%, 3 pcs)", "MU-RAIN"},
	}
	for i, w := range want {
		line := input.LineItems[i]
		if line.Title != w.title {
			t.Errorf("line %d title = %q, want %q", i, line.Title, w.title)
		}
		if line.SKU != w.sku {
			t.Errorf("line %d sku = %q, want %q", i, line.SKU, w.sku)
		}
		if line.VariantID != "" {
			t.Errorf("line %d must stay a custom line item, got variant %q", i, line.VariantID)
		}
	}

	// The invoice email lists the same wording the customer sees on the Shopify checkout.
	email := groupInvoiceEmailData(opts, "https://invoice.example/1")
	for i, w := range want {
		if email.Items[i].Title != w.title {
			t.Errorf("email item %d title = %q, want %q", i, email.Items[i].Title, w.title)
		}
	}
}
