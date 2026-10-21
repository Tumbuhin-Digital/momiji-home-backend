package order

import (
	"context"
	"errors"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
)

// Cancelling a pre-order whose balance was never paid turns "Sell when out of stock" back off.
func TestCancelledPreorderTurnsSellWhenOutOfStockOff(t *testing.T) {
	store, editor, svc := balanceEditFixture()
	if _, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if editor.policies[shelfVariant].Policy != shopify.InventoryPolicyContinue {
		t.Fatal("expected CONTINUE while the balance is open")
	}

	svc.releaseInventoryPoliciesForOrder(context.Background(), "order-1")

	if editor.policies[shelfVariant].Policy != shopify.InventoryPolicyDeny {
		t.Fatal("expected DENY after the pre-order was cancelled")
	}
	if rows := store.policyStore().rows; len(rows) != 1 || rows[0].RestoredAt == nil {
		t.Fatalf("expected the record restored, got %+v", rows)
	}
}

// A restore that fails stays listed and is retried by the next run.
func TestRestoreInventoryPoliciesRetriesAfterAFailure(t *testing.T) {
	store, editor, _ := balanceEditFixture()
	editor.policies[shelfVariant] = shopify.VariantInventoryPolicy{ProductID: "gid://shopify/Product/77", Policy: shopify.InventoryPolicyContinue}
	store.policyStore().rows = []InventoryPolicyOverride{{ShipmentID: "ship-1", ShopifyVariantID: shelfVariant, ShopifyProductID: "gid://shopify/Product/77"}}

	editor.policySetErr = errors.New("shopify down")
	ReleaseInventoryPolicies(context.Background(), store, editor, "ship-1")
	if rows := store.policyStore().rows; rows[0].ReleasedAt == nil || rows[0].RestoredAt != nil {
		t.Fatalf("expected released but not restored, got %+v", rows[0])
	}

	editor.policySetErr = nil
	RestoreInventoryPolicies(context.Background(), store, editor)
	if editor.policies[shelfVariant].Policy != shopify.InventoryPolicyDeny || store.policyStore().rows[0].RestoredAt == nil {
		t.Fatalf("expected the retry to restore DENY, got %+v", editor.policies[shelfVariant])
	}
}
