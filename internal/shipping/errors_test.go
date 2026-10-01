package shipping_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shipstation"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
)

func TestBuildPackages_MissingWeight(t *testing.T) {
	_, err := shipping.BuildPackages(context.Background(), []shipping.PackableUnit{
		{SKU: "SHELF-1", WeightKg: 0, BoxCount: 1},
	})
	var rateErr *shipping.RateError
	if !errors.As(err, &rateErr) {
		t.Fatalf("expected rate error, got %v", err)
	}
	if rateErr.Code != shipping.CodePackageData {
		t.Fatalf("code = %s", rateErr.Code)
	}
	if rateErr.Details["sku"] != "SHELF-1" {
		t.Fatalf("sku detail = %#v", rateErr.Details)
	}
}

func TestCalculateGroundRate_IncompleteAddress(t *testing.T) {
	client := &mockShipStationClient{}
	_, _, err := shipping.CalculateGroundRate(
		context.Background(),
		client,
		completeOrigin(),
		nil,
		"ups_ground",
		shipping.ShipToAddress{
			Name: "Customer", Phone: "555-555-5555",
			City: "Los Angeles", State: "CA", Zip: "90001", Country: "US",
		},
		[]shipstation.Package{{Weight: shipstation.Weight{Value: 2, Unit: "pound"}}},
		nil,
	)
	var rateErr *shipping.RateError
	if !errors.As(err, &rateErr) || rateErr.Code != shipping.CodeAddressIncomplete {
		t.Fatalf("expected incomplete address, got %v", err)
	}
	if rateErr.Details["address"] == "" {
		t.Fatalf("expected street detail, got %#v", rateErr.Details)
	}
	if client.lastReq.Shipment.ShipTo.AddressLine1 != "" {
		t.Fatal("carrier was called with an incomplete address")
	}
}

func TestCalculateGroundRate_InvalidZipDoesNotSendOriginalState(t *testing.T) {
	client := &mockShipStationClient{}
	_, _, err := shipping.CalculateGroundRate(
		context.Background(),
		client,
		completeOrigin(),
		nil,
		"ups_ground",
		shipping.ShipToAddress{
			Name: "Customer", Phone: "555-555-5555", Address1: "123 Main St",
			City: "Nowhere", State: "CA", Zip: "12", Country: "US",
		},
		[]shipstation.Package{{Weight: shipstation.Weight{Value: 2, Unit: "pound"}}},
		func(context.Context, string) (string, bool) { return "CA", true },
	)
	var rateErr *shipping.RateError
	if !errors.As(err, &rateErr) || rateErr.Code != shipping.CodeAddressZipInvalid {
		t.Fatalf("expected invalid zip, got %v", err)
	}
	if client.lastReq.Shipment.ShipTo.StateProvince != "" {
		t.Fatal("carrier was called after zip validation failed")
	}
}

func TestCalculateGroundRate_StateMismatch(t *testing.T) {
	client := &mockShipStationClient{}
	_, _, err := shipping.CalculateGroundRate(
		context.Background(),
		client,
		completeOrigin(),
		nil,
		"ups_ground",
		shipping.ShipToAddress{
			Name: "Customer", Phone: "555-555-5555", Address1: "123 Main St",
			City: "San Francisco", State: "NY", Zip: "94104", Country: "US",
		},
		[]shipstation.Package{{Weight: shipstation.Weight{Value: 2, Unit: "pound"}}},
		func(context.Context, string) (string, bool) { return "CA", true },
	)
	var rateErr *shipping.RateError
	if !errors.As(err, &rateErr) || rateErr.Code != shipping.CodeAddressStateMismatch {
		t.Fatalf("expected state mismatch, got %v", err)
	}
	if rateErr.Details["expected_state"] != "CA" {
		t.Fatalf("details = %#v", rateErr.Details)
	}
}

func TestCalculateGroundRate_NoGroundRate(t *testing.T) {
	client := &mockShipStationClient{
		rates: []shipstation.Rate{{
			ServiceCode:    "ups_next_day",
			ShippingAmount: shipstation.Money{Currency: "USD", Amount: 20},
		}},
	}
	_, _, err := shipping.CalculateGroundRate(
		context.Background(),
		client,
		completeOrigin(),
		nil,
		"ups_ground",
		completeDestination(),
		[]shipstation.Package{{Weight: shipstation.Weight{Value: 2, Unit: "pound"}}},
		nil,
	)
	var rateErr *shipping.RateError
	if !errors.As(err, &rateErr) || rateErr.Code != shipping.CodeCarrierNoRate {
		t.Fatalf("expected no ground rate, got %v", err)
	}
	if rateErr.Details["service_code"] != "ups_ground" {
		t.Fatalf("details = %#v", rateErr.Details)
	}
}

func TestClassifyCarrierFailure(t *testing.T) {
	addressErr := shipping.ClassifyCarrierFailure(&shipstation.RateResponseError{
		Messages: []shipstation.CarrierMessage{{
			ErrorCode: "invalid_address",
			Message:   "Invalid postal code",
		}},
	}, []string{"SHELF-1"})
	assertClassified(t, addressErr, shipping.CodeAddressRejected, "SHELF-1")

	packageErr := shipping.ClassifyCarrierFailure(&shipstation.APIError{
		StatusCode: http.StatusBadRequest,
		Message:    "package weight is below carrier minimum",
	}, nil)
	assertClassified(t, packageErr, shipping.CodePackageData, "")

	timeoutErr := shipping.ClassifyCarrierFailure(&shipstation.APIError{
		StatusCode: http.StatusBadGateway,
		Message:    "upstream timeout",
	}, nil)
	assertClassified(t, timeoutErr, shipping.CodeCarrierUnavailable, "")

	appErr := shipping.ToAPIError(addressErr)
	var apiErr *apierror.AppError
	if !errors.As(appErr, &apiErr) || apiErr.Code != shipping.CodeAddressRejected {
		t.Fatalf("api error = %#v", appErr)
	}
	if apiErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", apiErr.Status)
	}
}

func assertClassified(t *testing.T, err error, code, missingSKU string) {
	t.Helper()
	var rateErr *shipping.RateError
	if !errors.As(err, &rateErr) {
		t.Fatalf("expected rate error, got %v", err)
	}
	if rateErr.Code != code {
		t.Fatalf("code = %s, want %s (%s)", rateErr.Code, code, rateErr.Message)
	}
	if missingSKU != "" && rateErr.Details["missing_dimensions"] != missingSKU {
		t.Fatalf("missing dimensions = %#v", rateErr.Details)
	}
}

func completeOrigin() shipping.ShipFromAddress {
	return shipping.ShipFromAddress{
		Name: "Momiji Home", Phone: "555-123-4567", Address1: "100 Momiji Way",
		City: "Passaic", State: "NJ", Zip: "07055", Country: "US",
	}
}

func completeDestination() shipping.ShipToAddress {
	return shipping.ShipToAddress{
		Name: "Customer", Phone: "555-555-5555", Address1: "123 Main St",
		City: "Los Angeles", State: "CA", Zip: "90001", Country: "US",
	}
}
