package shipping

import (
	"fmt"
	"net/url"
	"strings"
)

// BuildTrackingURL returns a carrier tracking URL for the given tracking number.
// Returns empty string for unknown/custom carriers when no URL can be derived.
func BuildTrackingURL(carrier, trackingNumber string) string {
	num := strings.TrimSpace(trackingNumber)
	if num == "" {
		return ""
	}
	encoded := url.QueryEscape(num)

	switch strings.ToLower(strings.TrimSpace(carrier)) {
	case "ups", "unishippers":
		return fmt.Sprintf("https://www.ups.com/track?tracknum=%s", encoded)
	case "usps":
		return fmt.Sprintf("https://tools.usps.com/go/TrackConfirmAction?tLabels=%s", encoded)
	case "fedex", "fed ex":
		return fmt.Sprintf("https://www.fedex.com/fedextrack/?trknbr=%s", encoded)
	default:
		return ""
	}
}

// NormalizeCarrier returns a display-friendly carrier name.
func NormalizeCarrier(carrier string) string {
	switch strings.ToLower(strings.TrimSpace(carrier)) {
	case "ups":
		return "UPS"
	case "usps":
		return "USPS"
	case "fedex", "fed ex":
		return "FedEx"
	case "unishippers":
		return "Unishippers"
	case "custom":
		return "Custom"
	default:
		return carrier
	}
}
