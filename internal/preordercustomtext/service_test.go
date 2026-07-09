package preordercustomtext_test

import (
	"context"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preordercustomtext"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type mockStore struct {
	items      map[string]*preordercustomtext.PreorderCustomText
	usageCount int64
}

func newMockStore() *mockStore {
	return &mockStore{items: make(map[string]*preordercustomtext.PreorderCustomText)}
}

func (m *mockStore) List(ctx context.Context, search string) ([]preordercustomtext.PreorderCustomText, error) {
	out := make([]preordercustomtext.PreorderCustomText, 0)
	for _, item := range m.items {
		if item.DeletedAt != nil {
			continue
		}
		out = append(out, *item)
	}
	return out, nil
}

func (m *mockStore) Create(ctx context.Context, label string) (*preordercustomtext.PreorderCustomText, error) {
	item := &preordercustomtext.PreorderCustomText{ID: "id-" + label, Label: label}
	m.items[item.ID] = item
	return item, nil
}

func (m *mockStore) GetByID(ctx context.Context, id string) (*preordercustomtext.PreorderCustomText, error) {
	item, ok := m.items[id]
	if !ok || item.DeletedAt != nil {
		return nil, nil
	}
	return item, nil
}

func (m *mockStore) GetByLabel(ctx context.Context, label string) (*preordercustomtext.PreorderCustomText, error) {
	for _, item := range m.items {
		if item.DeletedAt == nil && item.Label == label {
			return item, nil
		}
	}
	return nil, nil
}

func (m *mockStore) SoftDelete(ctx context.Context, id string) error {
	item, ok := m.items[id]
	if !ok {
		return nil
	}
	now := item.UpdatedAt
	item.DeletedAt = &now
	return nil
}

func (m *mockStore) CountVariantUsage(ctx context.Context, label string) (int64, error) {
	return m.usageCount, nil
}

func (m *mockStore) EnsureByLabel(ctx context.Context, label string) (*preordercustomtext.PreorderCustomText, error) {
	existing, err := m.GetByLabel(ctx, label)
	if err != nil || existing != nil {
		return existing, err
	}
	return m.Create(ctx, label)
}

func TestCreate_RejectsDuplicateLabel(t *testing.T) {
	store := newMockStore()
	store.items["id-August"] = &preordercustomtext.PreorderCustomText{ID: "id-August", Label: "August"}
	svc := preordercustomtext.NewService(store)

	_, err := svc.Create(context.Background(), "August")
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestDelete_SoftDeleteReturnsUsageCount(t *testing.T) {
	store := newMockStore()
	store.items["id-August"] = &preordercustomtext.PreorderCustomText{ID: "id-August", Label: "August"}
	store.usageCount = 3
	svc := preordercustomtext.NewService(store)

	res, err := svc.Delete(context.Background(), "id-August")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UsageCount != 3 {
		t.Fatalf("expected usage count 3, got %d", res.UsageCount)
	}
	if store.items["id-August"].DeletedAt == nil {
		t.Fatal("expected soft delete timestamp")
	}
}

func TestDelete_NotFound(t *testing.T) {
	store := newMockStore()
	svc := preordercustomtext.NewService(store)

	_, err := svc.Delete(context.Background(), "missing")
	if err != apierror.ErrNotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestList_IncludesUsageCount(t *testing.T) {
	store := newMockStore()
	store.items["id-August"] = &preordercustomtext.PreorderCustomText{ID: "id-August", Label: "August"}
	store.usageCount = 2
	svc := preordercustomtext.NewService(store)

	items, err := svc.List(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 || items[0].UsageCount != 2 {
		t.Fatalf("expected one item with usage 2, got %+v", items)
	}
}
