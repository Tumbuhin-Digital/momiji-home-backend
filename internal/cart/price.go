package cart

import (
	"fmt"
	"math"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
)

// effectiveWholesalePrice returns the current wholesale unit price for a variant,
// matching product mapVariantToDTO fallback when ws_price is unset.
func effectiveWholesalePrice(variant *product.VariantDTO) float64 {
	var price float64
	fmt.Sscanf(variant.WSPrice, "%f", &price)
	if price > 0 {
		return price
	}
	fmt.Sscanf(variant.RetailPrice, "%f", &price)
	return price
}

func pricesDiffer(a, b float64) bool {
	return math.Abs(a-b) > 0.001
}
