package warehouse

import (
	"context"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/config"
)

type mockStore struct {
	rows map[string]Warehouse
}

func (m *mockStore) List(ctx context.Context) ([]Warehouse, error) {
	out := make([]Warehouse, 0, len(m.rows))
	for _, row := range m.rows {
		out = append(out, row)
	}
	return out, nil
}

func (m *mockStore) GetByCode(ctx context.Context, code string) (*Warehouse, error) {
	row, ok := m.rows[code]
	if !ok {
		return nil, gormErrRecordNotFound()
	}
	return &row, nil
}

func (m *mockStore) UpdateByCode(ctx context.Context, code string, updates map[string]interface{}) (*Warehouse, error) {
	row, ok := m.rows[code]
	if !ok {
		return nil, gormErrRecordNotFound()
	}
	for k, v := range updates {
		switch k {
		case "name":
			row.Name = v.(string)
		case "phone":
			row.Phone = v.(string)
		case "address1":
			row.Address1 = v.(string)
		case "city":
			row.City = v.(string)
		case "state":
			row.State = v.(string)
		case "zip":
			row.Zip = v.(string)
		case "country":
			row.Country = v.(string)
		}
	}
	m.rows[code] = row
	return &row, nil
}

func gormErrRecordNotFound() error {
	return errRecordNotFound{}
}

type errRecordNotFound struct{}

func (errRecordNotFound) Error() string { return "record not found" }

func TestResolveOriginShipReadyAlwaysEast(t *testing.T) {
	svc := NewService(&mockStore{rows: map[string]Warehouse{}}, config.ShipStationConfig{})
	if got := svc.ResolveOrigin("ship_ready", "west"); got != CodeEast {
		t.Fatalf("expected east, got %s", got)
	}
}

func TestResolveOriginPreOrderUsesRequested(t *testing.T) {
	svc := NewService(&mockStore{rows: map[string]Warehouse{}}, config.ShipStationConfig{})
	if got := svc.ResolveOrigin("pre_order", "west"); got != CodeWest {
		t.Fatalf("expected west, got %s", got)
	}
	if got := svc.ResolveOrigin("pre_order", ""); got != CodeEast {
		t.Fatalf("expected default east, got %s", got)
	}
}

func TestGetOriginUsesCacheNotStoreOnSecondCall(t *testing.T) {
	store := &mockStore{
		rows: map[string]Warehouse{
			CodeEast: {
				Code:     CodeEast,
				Name:     "East",
				Address1: "100 Test St",
				City:     "Passaic",
				State:    "NJ",
				Zip:      "07055",
				Country:  "US",
			},
		},
	}
	svc := NewService(store, config.ShipStationConfig{GroundServiceCode: "ups_ground"})
	if err := svc.Warm(context.Background()); err != nil {
		t.Fatalf("warm: %v", err)
	}

	origin, err := svc.GetOrigin(context.Background(), CodeEast)
	if err != nil {
		t.Fatalf("get origin: %v", err)
	}
	if origin.Address1 != "100 Test St" {
		t.Fatalf("unexpected origin: %+v", origin)
	}

	store.rows[CodeEast] = Warehouse{Code: CodeEast, Address1: "changed", Zip: "00000", City: "X", State: "NJ", Country: "US"}
	origin2, err := svc.GetOrigin(context.Background(), CodeEast)
	if err != nil {
		t.Fatalf("get origin cached: %v", err)
	}
	if origin2.Address1 != "100 Test St" {
		t.Fatalf("expected cached value, got %+v", origin2)
	}
}

func TestUpdateInvalidatesCache(t *testing.T) {
	store := &mockStore{
		rows: map[string]Warehouse{
			CodeEast: {Code: CodeEast, Name: "Old", Address1: "1 St", City: "Passaic", State: "NJ", Zip: "07055", Country: "US"},
		},
	}
	svc := NewService(store, config.ShipStationConfig{})
	_ = svc.Warm(context.Background())

	_, err := svc.Update(context.Background(), CodeEast, UpdateWarehouseRequest{
		Name:     "New Name",
		Phone:    "555",
		Address1: "2 St",
		City:     "Passaic",
		State:    "NJ",
		Zip:      "07055",
		Country:  "US",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	origin, err := svc.GetOrigin(context.Background(), CodeEast)
	if err != nil {
		t.Fatalf("get origin: %v", err)
	}
	if origin.Name != "New Name" || origin.Address1 != "2 St" {
		t.Fatalf("cache not refreshed: %+v", origin)
	}
}

func TestGetOriginFallsBackToEnv(t *testing.T) {
	svc := NewService(&mockStore{rows: map[string]Warehouse{}}, config.ShipStationConfig{
		WarehouseName:     "Env Warehouse",
		WarehouseAddress1: "Env St",
		WarehouseCity:     "Passaic",
		WarehouseState:    "NJ",
		WarehouseZip:      "07055",
		WarehouseCountry:  "US",
		GroundServiceCode: "ups_ground",
	})
	_ = svc.Warm(context.Background())

	origin, err := svc.GetOrigin(context.Background(), CodeEast)
	if err != nil {
		t.Fatalf("get origin: %v", err)
	}
	if origin.Address1 != "Env St" {
		t.Fatalf("expected env fallback, got %+v", origin)
	}
}
