package shipping

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shipstation"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/units"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/uszip"
)

// PackableUnit describes one logical box for ShipStation packages[].
type PackableUnit struct {
	WeightKg float64
	WidthCm  float64
	HeightCm float64
	DepthCm  float64 // mapped to package length (consistent with product service)
	BoxCount int

	// Metadata for logging when fallbacks apply.
	LineItemID        string
	SKU               string
	ShopifyVariantID  string
}

// ShipFromAddress is the origin warehouse for rate calculation.
type ShipFromAddress struct {
	Name     string
	Phone    string
	Address1 string
	City     string
	State    string
	Zip      string
	Country  string
}

// ShipToAddress is the destination for rate calculation.
type ShipToAddress struct {
	Name     string
	Phone    string
	Address1 string
	City     string
	State    string
	Zip      string
	Country  string
}

// ZipLookup resolves a US ZIP to state abbreviation for ShipStation.
type ZipLookup func(ctx context.Context, zip string) (stateAbbr string, ok bool)

// BuildPackages creates one ShipStation package entry per box.
// Nested items should have BoxCount=0 and are skipped entirely.
// Assumption: nested items physically fit inside another item's box and do not
// add a separate package entry to the rate request.
func BuildPackages(ctx context.Context, packableUnits []PackableUnit) []shipstation.Package {
	var packages []shipstation.Package

	for _, unit := range packableUnits {
		if unit.BoxCount <= 0 {
			continue
		}

		wt := units.KgToLb(unit.WeightKg)
		if wt == 0 {
			slog.WarnContext(ctx, "shipping package weight missing, using default 1 lb",
				"line_item_id", unit.LineItemID,
				"sku", unit.SKU,
				"shopify_variant_id", unit.ShopifyVariantID,
				"weight_kg", unit.WeightKg,
			)
			wt = units.DefaultLb
		}

		pkg := shipstation.Package{
			Weight: shipstation.Weight{
				Value: wt,
				Unit:  "pound",
			},
		}

		if unit.DepthCm > 0 || unit.WidthCm > 0 || unit.HeightCm > 0 {
			dims := []float64{unit.DepthCm, unit.WidthCm, unit.HeightCm}
			sort.Sort(sort.Reverse(sort.Float64Slice(dims)))
			pkg.Dimensions = &shipstation.Dimensions{
				Unit:   "inch",
				Length: units.CmToIn(dims[0]),
				Width:  units.CmToIn(dims[1]),
				Height: units.CmToIn(dims[2]),
			}
		} else {
			slog.WarnContext(ctx, "shipping package dimensions missing, omitting from rate request",
				"line_item_id", unit.LineItemID,
				"sku", unit.SKU,
				"shopify_variant_id", unit.ShopifyVariantID,
			)
		}

		for i := 0; i < unit.BoxCount; i++ {
			packages = append(packages, pkg)
		}
	}

	return packages
}

// TotalWeightLb sums weight across all boxes (nested items excluded).
func TotalWeightLb(packableUnits []PackableUnit) float64 {
	var total float64
	for _, unit := range packableUnits {
		if unit.BoxCount <= 0 {
			continue
		}
		wt := units.KgToLb(unit.WeightKg)
		if wt == 0 {
			wt = units.DefaultLb
		}
		total += wt * float64(unit.BoxCount)
	}
	return math.Round(total*100) / 100
}

// TotalBoxes returns the sum of box counts across units.
func TotalBoxes(packableUnits []PackableUnit) int {
	var total int
	for _, unit := range packableUnits {
		if unit.BoxCount > 0 {
			total += unit.BoxCount
		}
	}
	return total
}

// CalculateGroundRate calls ShipStation and returns the ground service rate only.
func CalculateGroundRate(
	ctx context.Context,
	client shipstation.Client,
	shipFrom ShipFromAddress,
	carrierCodes []string,
	groundServiceCode string,
	shipTo ShipToAddress,
	packages []shipstation.Package,
	zipLookup ZipLookup,
) (amount float64, currency string, err error) {
	if len(packages) == 0 {
		return 0, "", fmt.Errorf("no packages to rate")
	}

	groundCode := groundServiceCode
	if groundCode == "" {
		groundCode = "ups_ground"
	}

	if shipTo.Name == "" {
		shipTo.Name = "Recipient"
	}
	if shipTo.Phone == "" {
		shipTo.Phone = "555-555-5555"
	}
	if shipTo.Address1 == "" {
		shipTo.Address1 = "123 Unknown St"
	}
	country := shipTo.Country
	if country == "" {
		country = "US"
	}

	fromCountry := shipFrom.Country
	if fromCountry == "" {
		fromCountry = "US"
	}

	req := shipstation.RateRequest{
		RateOptions: shipstation.RateOptions{
			CarrierIDs:   carrierCodes,
			ServiceCodes: []string{groundCode},
		},
		Shipment: shipstation.Shipment{
			ValidateAddress: "no_validation",
			ShipFrom: shipstation.Address{
				Name:          shipFrom.Name,
				Phone:         shipFrom.Phone,
				AddressLine1:  shipFrom.Address1,
				CityLocality:  shipFrom.City,
				StateProvince: resolveState(ctx, fromCountry, shipFrom.Zip, shipFrom.State, zipLookup),
				PostalCode:    shipFrom.Zip,
				CountryCode:   fromCountry,
			},
			ShipTo: shipstation.Address{
				Name:          shipTo.Name,
				Phone:         shipTo.Phone,
				AddressLine1:  shipTo.Address1,
				CityLocality:  shipTo.City,
				StateProvince: resolveState(ctx, country, shipTo.Zip, shipTo.State, zipLookup),
				PostalCode:    shipTo.Zip,
				CountryCode:   country,
			},
			Packages: packages,
		},
	}

	rates, err := client.GetRates(ctx, req)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get shipping rates from shipstation", "error", err)
		return 0, "", err
	}

	for _, r := range rates {
		if r.ServiceCode == groundCode {
			return r.ShippingAmount.Amount, r.ShippingAmount.Currency, nil
		}
	}

	return 0, "", fmt.Errorf("no ground rate returned for service %s", groundCode)
}

func resolveState(ctx context.Context, country, zip, defaultState string, zipLookup ZipLookup) string {
	if zipLookup == nil {
		return defaultState
	}
	if !strings.EqualFold(country, "US") && !strings.EqualFold(country, "United States") {
		return defaultState
	}
	normalized, ok := uszip.NormalizeUSZip(zip)
	if !ok {
		return defaultState
	}
	if abbr, ok := zipLookup(ctx, normalized); ok && abbr != "" {
		return abbr
	}
	return defaultState
}

// PackableUnitFromCartItem converts cart item fields to a single-unit packable (BoxCount set separately).
func PackableUnitFromCartItem(weight float64, weightUnit string, length, width, height float64, boxCount int) PackableUnit {
	wtKg, ok := units.CartWeightToKg(weight, weightUnit)
	if !ok {
		slog.Warn("unknown cart weight unit, treating value as kg",
			"weight_unit", weightUnit,
			"weight", weight,
		)
	}
	return PackableUnit{
		WeightKg: wtKg,
		WidthCm:  width,
		HeightCm: height,
		DepthCm:  length,
		BoxCount: boxCount,
	}
}
