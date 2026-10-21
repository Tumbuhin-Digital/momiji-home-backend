package order

import (
	"context"
	"log/slog"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
)

// A pre-order balance adds the real product to the deposit order, and Shopify stock for it
// may not be recorded yet. So that the customer can still pay, the app turns on "Sell when
// out of stock" (inventoryPolicy CONTINUE) for the group's variants while the balance is
// open, and turns it off again once no open balance needs it. Variants that were already
// CONTINUE are never recorded, so the app never turns them off.

// InventoryPolicyOverride records that the app turned CONTINUE on for a variant on behalf of
// one group.
type InventoryPolicyOverride struct {
	ID               string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	ShipmentID       string     `gorm:"column:shipment_id;not null"`
	ShopifyVariantID string     `gorm:"column:shopify_variant_id;not null"`
	ShopifyProductID string     `gorm:"column:shopify_product_id;not null"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	ReleasedAt       *time.Time `gorm:"column:released_at"`
	RestoredAt       *time.Time `gorm:"column:restored_at"`
}

func (InventoryPolicyOverride) TableName() string {
	return "preorder_inventory_policy_overrides"
}

// InventoryPolicyStore persists which variants the app turned CONTINUE on for.
type InventoryPolicyStore interface {
	AddInventoryPolicyOverrides(ctx context.Context, rows []InventoryPolicyOverride) error
	// VariantsWithInventoryPolicyOverride reports which variants have a row that is not yet
	// restored (openOnly=false) or not yet released (openOnly=true).
	VariantsWithInventoryPolicyOverride(ctx context.Context, variantIDs []string, openOnly bool) (map[string]bool, error)
	ReleaseInventoryPolicyOverrides(ctx context.Context, shipmentID string) error
	// ListRestorableInventoryPolicyOverrides returns released, unrestored rows of variants
	// that no group still needs.
	ListRestorableInventoryPolicyOverrides(ctx context.Context) ([]InventoryPolicyOverride, error)
	MarkInventoryPolicyOverridesRestored(ctx context.Context, variantIDs []string) error
}

// RestoreInventoryPolicies turns "Sell when out of stock" back off for variants the app
// turned on and that no open balance needs any more. Failures are logged and retried on the
// next run; nothing is lost because unrestored rows stay listed.
func RestoreInventoryPolicies(ctx context.Context, store InventoryPolicyStore, editor shopify.OrderEditor) {
	if store == nil || editor == nil {
		return
	}
	rows, err := store.ListRestorableInventoryPolicyOverrides(ctx)
	if err != nil {
		slog.WarnContext(ctx, "inventory policy restore: listing failed", slog.Any("error", err))
		return
	}
	byProduct := make(map[string][]string)
	seen := make(map[string]bool)
	for _, r := range rows {
		if seen[r.ShopifyVariantID] {
			continue
		}
		seen[r.ShopifyVariantID] = true
		byProduct[r.ShopifyProductID] = append(byProduct[r.ShopifyProductID], r.ShopifyVariantID)
	}
	for productID, variantIDs := range byProduct {
		if err := editor.SetVariantsInventoryPolicy(ctx, productID, variantIDs, shopify.InventoryPolicyDeny); err != nil {
			slog.WarnContext(ctx, "inventory policy restore: turning sell-when-out-of-stock off failed; will retry",
				slog.String("product_id", productID), slog.Any("error", err))
			continue
		}
		if err := store.MarkInventoryPolicyOverridesRestored(ctx, variantIDs); err != nil {
			slog.WarnContext(ctx, "inventory policy restore: saving failed", slog.Any("error", err))
			continue
		}
		slog.InfoContext(ctx, "inventory policy restored to DENY", slog.String("product_id", productID), slog.Any("variants", variantIDs))

		// A new balance may have joined while this ran; it needs CONTINUE back on.
		open, err := store.VariantsWithInventoryPolicyOverride(ctx, variantIDs, true)
		if err != nil {
			continue
		}
		var reopen []string
		for _, v := range variantIDs {
			if open[v] {
				reopen = append(reopen, v)
			}
		}
		if len(reopen) > 0 {
			if err := editor.SetVariantsInventoryPolicy(ctx, productID, reopen, shopify.InventoryPolicyContinue); err != nil {
				slog.ErrorContext(ctx, "inventory policy restore: re-enabling for a new open balance failed",
					slog.String("product_id", productID), slog.Any("variants", reopen), slog.Any("error", err))
			}
		}
	}
}

// releaseInventoryPoliciesForOrder releases every balance group of a cancelled pre-order, so
// "Sell when out of stock" does not stay on for a balance that will never be paid.
func (s *service) releaseInventoryPoliciesForOrder(ctx context.Context, orderID string) {
	shipments, err := s.store.GetPreorderShipments(ctx, orderID)
	if err != nil {
		slog.WarnContext(ctx, "inventory policy release on cancel: loading groups failed", slog.String("order_id", orderID), slog.Any("error", err))
		return
	}
	for _, sh := range shipments {
		if sh.IsOrderEditBalance() {
			ReleaseInventoryPolicies(ctx, s.store, s.orderEditor, sh.ID)
		}
	}
}

// ReleaseInventoryPolicies marks a group as no longer needing CONTINUE and turns it off for
// variants nothing else needs.
func ReleaseInventoryPolicies(ctx context.Context, store InventoryPolicyStore, editor shopify.OrderEditor, shipmentID string) {
	if store == nil {
		return
	}
	if err := store.ReleaseInventoryPolicyOverrides(ctx, shipmentID); err != nil {
		slog.WarnContext(ctx, "inventory policy release failed", slog.String("shipment_id", shipmentID), slog.Any("error", err))
		return
	}
	RestoreInventoryPolicies(ctx, store, editor)
}
