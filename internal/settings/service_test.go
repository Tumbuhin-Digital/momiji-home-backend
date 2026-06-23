package settings

import (
	"context"
	"testing"
)

type mockStore struct {
	values map[string]string
}

func (m *mockStore) Get(ctx context.Context, key string) (string, error) {
	return m.values[key], nil
}

func (m *mockStore) GetMany(ctx context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		result[key] = m.values[key]
	}
	return result, nil
}

func (m *mockStore) Upsert(ctx context.Context, key, value string) error {
	m.values[key] = value
	return nil
}

func TestGetCheckoutNotes(t *testing.T) {
	store := &mockStore{
		values: map[string]string{
			KeyCheckoutDueNowNote:   "Due now text",
			KeyCheckoutDueLaterNote: "Due later text",
		},
	}
	svc := NewSettingsService(store)

	notes, err := svc.GetCheckoutNotes(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if notes.DueNowNote != "Due now text" || notes.DueLaterNote != "Due later text" {
		t.Fatalf("unexpected notes: %+v", notes)
	}
}

func TestUpdateCheckoutNotesRejectsEmpty(t *testing.T) {
	store := &mockStore{values: map[string]string{}}
	svc := NewSettingsService(store)

	_, err := svc.UpdateCheckoutNotes(context.Background(), UpdateSettingsRequest{
		DueNowNote:   " ",
		DueLaterNote: "valid",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestUpdateCheckoutNotes(t *testing.T) {
	store := &mockStore{values: map[string]string{}}
	svc := NewSettingsService(store)

	notes, err := svc.UpdateCheckoutNotes(context.Background(), UpdateSettingsRequest{
		DueNowNote:   " Updated now ",
		DueLaterNote: "Updated later",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if notes.DueNowNote != "Updated now" || notes.DueLaterNote != "Updated later" {
		t.Fatalf("unexpected notes: %+v", notes)
	}
}
