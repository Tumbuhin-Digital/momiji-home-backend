package order

import (
	"context"
	"time"
)

// memPolicyStore is an in-memory InventoryPolicyStore with the same rules as the Postgres one.
type memPolicyStore struct {
	rows []InventoryPolicyOverride
}

func (m *memPolicyStore) AddInventoryPolicyOverrides(_ context.Context, rows []InventoryPolicyOverride) error {
	for _, r := range rows {
		dup := false
		for _, existing := range m.rows {
			if existing.ShipmentID == r.ShipmentID && existing.ShopifyVariantID == r.ShopifyVariantID {
				dup = true
				break
			}
		}
		if !dup {
			r.CreatedAt = time.Now()
			m.rows = append(m.rows, r)
		}
	}
	return nil
}

func (m *memPolicyStore) VariantsWithInventoryPolicyOverride(_ context.Context, variantIDs []string, openOnly bool) (map[string]bool, error) {
	want := make(map[string]bool, len(variantIDs))
	for _, v := range variantIDs {
		want[v] = true
	}
	out := make(map[string]bool)
	for _, r := range m.rows {
		if !want[r.ShopifyVariantID] {
			continue
		}
		if (openOnly && r.ReleasedAt == nil) || (!openOnly && r.RestoredAt == nil) {
			out[r.ShopifyVariantID] = true
		}
	}
	return out, nil
}

func (m *memPolicyStore) ReleaseInventoryPolicyOverrides(_ context.Context, shipmentID string) error {
	now := time.Now()
	for i := range m.rows {
		if m.rows[i].ShipmentID == shipmentID && m.rows[i].ReleasedAt == nil {
			m.rows[i].ReleasedAt = &now
		}
	}
	return nil
}

func (m *memPolicyStore) ListRestorableInventoryPolicyOverrides(context.Context) ([]InventoryPolicyOverride, error) {
	open := make(map[string]bool)
	for _, r := range m.rows {
		if r.ReleasedAt == nil {
			open[r.ShopifyVariantID] = true
		}
	}
	var out []InventoryPolicyOverride
	for _, r := range m.rows {
		if r.ReleasedAt != nil && r.RestoredAt == nil && !open[r.ShopifyVariantID] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memPolicyStore) MarkInventoryPolicyOverridesRestored(_ context.Context, variantIDs []string) error {
	want := make(map[string]bool, len(variantIDs))
	for _, v := range variantIDs {
		want[v] = true
	}
	now := time.Now()
	for i := range m.rows {
		r := &m.rows[i]
		if want[r.ShopifyVariantID] && r.ReleasedAt != nil && r.RestoredAt == nil {
			r.RestoredAt = &now
		}
	}
	return nil
}

// Route balanceEditStore's policy methods to an in-memory store (its embedded shipping
// store only has no-ops).

func (s *balanceEditStore) policyStore() *memPolicyStore {
	if s.policies == nil {
		s.policies = &memPolicyStore{}
	}
	return s.policies
}

func (s *balanceEditStore) AddInventoryPolicyOverrides(ctx context.Context, rows []InventoryPolicyOverride) error {
	return s.policyStore().AddInventoryPolicyOverrides(ctx, rows)
}

func (s *balanceEditStore) VariantsWithInventoryPolicyOverride(ctx context.Context, variantIDs []string, openOnly bool) (map[string]bool, error) {
	return s.policyStore().VariantsWithInventoryPolicyOverride(ctx, variantIDs, openOnly)
}

func (s *balanceEditStore) ReleaseInventoryPolicyOverrides(ctx context.Context, shipmentID string) error {
	return s.policyStore().ReleaseInventoryPolicyOverrides(ctx, shipmentID)
}

func (s *balanceEditStore) ListRestorableInventoryPolicyOverrides(ctx context.Context) ([]InventoryPolicyOverride, error) {
	return s.policyStore().ListRestorableInventoryPolicyOverrides(ctx)
}

func (s *balanceEditStore) MarkInventoryPolicyOverridesRestored(ctx context.Context, variantIDs []string) error {
	return s.policyStore().MarkInventoryPolicyOverridesRestored(ctx, variantIDs)
}
