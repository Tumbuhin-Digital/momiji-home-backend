package checkout

import (
	"context"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/uszip"
)

type zipLookupStore struct {
	byZip map[string]*UsZipCode
}

func (s *zipLookupStore) GetActiveLocksForVariant(context.Context, string) (int, error) {
	return 0, nil
}

func (s *zipLookupStore) CreateLocks(context.Context, []StockLock) error { return nil }

func (s *zipLookupStore) DeleteLocksBySession(context.Context, *string, *string) error { return nil }

func (s *zipLookupStore) DeleteLocksByCheckoutReference(context.Context, string, *string, *string) error {
	return nil
}

func (s *zipLookupStore) DeleteExpiredLocks(context.Context) error { return nil }

func (s *zipLookupStore) GetUSZipCodeDetails(_ context.Context, zip string) (*UsZipCode, error) {
	normalized, ok := uszip.NormalizeUSZip(zip)
	if !ok {
		return nil, nil
	}
	if row, found := s.byZip[normalized]; found {
		return row, nil
	}
	return nil, nil
}

func TestValidateAddress_SanFrancisco94104(t *testing.T) {
	store := &zipLookupStore{
		byZip: map[string]*UsZipCode{
			"94104": {
				ZipCode:   "94104",
				City:      "San Francisco",
				StateAbbr: "CA",
				StateName: "California",
			},
		},
	}

	svc := &service{store: store}

	validReq := ValidateAddressRequest{
		Country: "United States",
		State:   "CA",
		City:    "San Francisco",
		Zip:     "94104",
	}
	if errs := svc.ValidateAddress(context.Background(), validReq); len(errs) > 0 {
		t.Fatalf("expected valid address, got errors: %#v", errs)
	}

	zipPlusFour := validReq
	zipPlusFour.Zip = "94104-1234"
	if errs := svc.ValidateAddress(context.Background(), zipPlusFour); len(errs) > 0 {
		t.Fatalf("expected ZIP+4 to validate, got errors: %#v", errs)
	}

	unknownZip := validReq
	unknownZip.Zip = "00000"
	if errs := svc.ValidateAddress(context.Background(), unknownZip); errs["zip"] != "Invalid US ZIP code" {
		t.Fatalf("expected invalid zip error, got: %#v", errs)
	}

	cityMismatch := validReq
	cityMismatch.City = "Los Angeles"
	if errs := svc.ValidateAddress(context.Background(), cityMismatch); errs["city"] != "City does not match ZIP" {
		t.Fatalf("expected city mismatch, got: %#v", errs)
	}

	nonUS := validReq
	nonUS.Country = "Canada"
	if errs := svc.ValidateAddress(context.Background(), nonUS); errs != nil {
		t.Fatalf("expected non-US to skip validation, got: %#v", errs)
	}
}
