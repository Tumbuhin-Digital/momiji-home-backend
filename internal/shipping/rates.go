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
	LineItemID       string
	SKU              string
	ShopifyVariantID string
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
// A shippable box with no weight returns package_data instead of substituting 1 lb.
func BuildPackages(ctx context.Context, packableUnits []PackableUnit) ([]shipstation.Package, error) {
	var missingWeight []string
	for _, unit := range packableUnits {
		if unit.BoxCount <= 0 {
			continue
		}
		if units.KgToLb(unit.WeightKg) == 0 {
			missingWeight = append(missingWeight, unitLabel(unit))
		}
	}
	if len(missingWeight) > 0 {
		return nil, PackageData(missingWeight)
	}

	var packages []shipstation.Package

	for _, unit := range packableUnits {
		if unit.BoxCount <= 0 {
			continue
		}

		wt := units.KgToLb(unit.WeightKg)

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

	return packages, nil
}

func unitLabel(unit PackableUnit) string {
	switch {
	case strings.TrimSpace(unit.SKU) != "":
		return strings.TrimSpace(unit.SKU)
	case strings.TrimSpace(unit.LineItemID) != "":
		return strings.TrimSpace(unit.LineItemID)
	case strings.TrimSpace(unit.ShopifyVariantID) != "":
		return strings.TrimSpace(unit.ShopifyVariantID)
	default:
		return "unknown item"
	}
}

// MissingDimensionLabels lists shippable units that will be rated without dimensions.
func MissingDimensionLabels(packableUnits []PackableUnit) []string {
	var labels []string
	for _, unit := range packableUnits {
		if unit.BoxCount <= 0 {
			continue
		}
		if unit.DepthCm > 0 || unit.WidthCm > 0 || unit.HeightCm > 0 {
			continue
		}
		labels = append(labels, unitLabel(unit))
	}
	return labels
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
		return 0, "", PackageEmpty()
	}

	groundCode := groundServiceCode
	if groundCode == "" {
		groundCode = "ups_ground"
	}

	if missing := missingShipToFields(shipTo); len(missing) > 0 {
		return 0, "", AddressIncomplete(missing)
	}
	if err := validateShipFrom(shipFrom); err != nil {
		return 0, "", err
	}

	toState, err := resolveDestinationState(ctx, shipTo.Country, shipTo.Zip, shipTo.State, zipLookup)
	if err != nil {
		return 0, "", err
	}
	fromState, err := resolveOriginState(ctx, shipFrom.Country, shipFrom.Zip, shipFrom.State, zipLookup)
	if err != nil {
		return 0, "", err
	}

	req := shipstation.RateRequest{
		RateOptions: shipstation.RateOptions{
			CarrierIDs:   carrierCodes,
			ServiceCodes: []string{groundCode},
		},
		Shipment: shipstation.Shipment{
			ValidateAddress: "no_validation",
			// UI "Online" maps to API "none" — Rates enum rejects "online".
			Confirmation: "none",
			ShipFrom: shipstation.Address{
				Name:                        shipFrom.Name,
				Phone:                       shipFrom.Phone,
				AddressLine1:                shipFrom.Address1,
				CityLocality:                shipFrom.City,
				StateProvince:               fromState,
				PostalCode:                  shipFrom.Zip,
				CountryCode:                 shipFrom.Country,
				AddressResidentialIndicator: "unknown",
			},
			ShipTo: shipstation.Address{
				Name:                        shipTo.Name,
				Phone:                       shipTo.Phone,
				AddressLine1:                shipTo.Address1,
				CityLocality:                shipTo.City,
				StateProvince:               toState,
				PostalCode:                  shipTo.Zip,
				CountryCode:                 shipTo.Country,
				AddressResidentialIndicator: "unknown",
			},
			Packages: packages,
		},
	}

	rates, err := client.GetRates(ctx, req)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get shipping rates from shipstation", "error", err)
		return 0, "", ClassifyCarrierFailure(err, nil)
	}

	for _, r := range rates {
		if r.ServiceCode == groundCode {
			total := r.ShippingAmount.Amount + r.ConfirmationAmount.Amount + r.InsuranceAmount.Amount + r.OtherAmount.Amount
			currency := r.ShippingAmount.Currency
			if currency == "" {
				currency = "USD"
			}
			return total, currency, nil
		}
	}

	return 0, "", CarrierNoRate(groundCode, shipTo.Zip)
}

func missingShipToFields(addr ShipToAddress) []string {
	var missing []string
	if strings.TrimSpace(addr.Name) == "" {
		missing = append(missing, "name")
	}
	if strings.TrimSpace(addr.Phone) == "" {
		missing = append(missing, "phone")
	}
	if strings.TrimSpace(addr.Address1) == "" {
		missing = append(missing, "address")
	}
	if strings.TrimSpace(addr.City) == "" {
		missing = append(missing, "city")
	}
	if strings.TrimSpace(addr.State) == "" {
		missing = append(missing, "state")
	}
	if strings.TrimSpace(addr.Zip) == "" {
		missing = append(missing, "zip")
	}
	if strings.TrimSpace(addr.Country) == "" {
		missing = append(missing, "country")
	}
	return missing
}

func validateShipFrom(addr ShipFromAddress) error {
	var missing []string
	if strings.TrimSpace(addr.Address1) == "" {
		missing = append(missing, "street")
	}
	if strings.TrimSpace(addr.City) == "" {
		missing = append(missing, "city")
	}
	if strings.TrimSpace(addr.State) == "" {
		missing = append(missing, "state")
	}
	if strings.TrimSpace(addr.Zip) == "" {
		missing = append(missing, "ZIP")
	}
	if strings.TrimSpace(addr.Country) == "" {
		missing = append(missing, "country")
	}
	if len(missing) == 0 {
		return nil
	}
	return WarehouseOrigin("Warehouse origin address is incomplete. Missing: " + strings.Join(missing, ", ") + ".")
}

func isUSCountry(country string) bool {
	switch strings.ToLower(strings.TrimSpace(country)) {
	case "us", "usa", "united states":
		return true
	default:
		return false
	}
}

func resolveDestinationState(ctx context.Context, country, zip, provided string, zipLookup ZipLookup) (string, error) {
	provided = strings.TrimSpace(provided)
	if !isUSCountry(country) {
		return provided, nil
	}
	normalized, ok := uszip.NormalizeUSZip(zip)
	if !ok {
		return "", ZipInvalid(zip)
	}
	if zipLookup == nil {
		return provided, nil
	}
	abbr, ok := zipLookup(ctx, normalized)
	if !ok || strings.TrimSpace(abbr) == "" {
		return "", ZipInvalid(normalized)
	}
	abbr = strings.TrimSpace(abbr)
	if provided != "" && len(provided) <= 3 && !strings.EqualFold(provided, abbr) {
		return "", StateMismatch(normalized, provided, abbr)
	}
	return abbr, nil
}

func resolveOriginState(ctx context.Context, country, zip, provided string, zipLookup ZipLookup) (string, error) {
	provided = strings.TrimSpace(provided)
	if !isUSCountry(country) || zipLookup == nil {
		return provided, nil
	}
	normalized, ok := uszip.NormalizeUSZip(zip)
	if !ok {
		return "", WarehouseOrigin(fmt.Sprintf("Warehouse origin ZIP %s is not a valid US ZIP.", strings.TrimSpace(zip)))
	}
	abbr, ok := zipLookup(ctx, normalized)
	if !ok || strings.TrimSpace(abbr) == "" {
		return "", WarehouseOrigin(fmt.Sprintf("Warehouse origin ZIP %s is not a valid US ZIP.", normalized))
	}
	return strings.TrimSpace(abbr), nil
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
