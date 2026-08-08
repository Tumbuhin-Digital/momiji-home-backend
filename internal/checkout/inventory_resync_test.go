package checkout

import (
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

func TestIsOutOfStockError(t *testing.T) {
	t.Parallel()

	if !isOutOfStockError(apierror.New(422, "out_of_stock", "gone")) {
		t.Fatal("expected out_of_stock to match")
	}
	if isOutOfStockError(apierror.New(400, "bad_request", "nope")) {
		t.Fatal("expected non-matching code")
	}
}

func TestShipReadyInventoryDepletedError(t *testing.T) {
	t.Parallel()

	err := shipReadyInventoryDepletedError(&cart.ShipReadyInventoryDepletionEvent{
		VariantID:       "gid://v/1",
		MovedToPreorder: 3,
		Available:       0,
	})
	apiErr, ok := err.(*apierror.AppError)
	if !ok {
		t.Fatal("expected AppError")
	}
	if apiErr.Status != 409 || apiErr.Code != "ship_ready_inventory_depleted" {
		t.Fatalf("unexpected error: %+v", apiErr)
	}
	details, ok := apiErr.Details.(*cart.ShipReadyInventoryDepletionEvent)
	if !ok || details.MovedToPreorder != 3 {
		t.Fatalf("unexpected details: %+v", apiErr.Details)
	}
}
