package shipping

import (
	"strings"
	"unicode"
)

// knownShippingMethodLabels maps carrier service codes to customer-facing names.
var knownShippingMethodLabels = map[string]string{
	"ups_ground":               "UPS Ground",
	"ups_ground_international": "UPS Ground International",
	"ups_3_day_select":         "UPS 3 Day Select",
	"ups_2nd_day_air":          "UPS 2nd Day Air",
	"ups_next_day_air":         "UPS Next Day Air",
	"usps_priority_mail":       "USPS Priority Mail",
	"fedex_ground":             "FedEx Ground",
	"fedex_home_delivery":      "FedEx Home Delivery",
}

// HumanizeShippingMethod turns a service code (e.g. ups_ground) into a readable label.
func HumanizeShippingMethod(method string) string {
	method = strings.TrimSpace(method)
	if method == "" {
		return ""
	}
	if label, ok := knownShippingMethodLabels[strings.ToLower(method)]; ok {
		return label
	}
	// Already human-readable (contains spaces / mixed case without underscores).
	if !strings.Contains(method, "_") && strings.ContainsFunc(method, unicode.IsSpace) {
		return method
	}
	parts := strings.Split(strings.ToLower(method), "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		switch p {
		case "ups", "usps", "fedex":
			parts[i] = strings.ToUpper(p)
		default:
			runes := []rune(p)
			runes[0] = unicode.ToUpper(runes[0])
			parts[i] = string(runes)
		}
	}
	return strings.Join(parts, " ")
}

// CustomerShippingLineTitle is the invoice/email line label for pre-order shipping
// charged on the second payment (due later). Always customer-friendly — never raw codes.
func CustomerShippingLineTitle(method string) string {
	method = strings.TrimSpace(method)
	if method == "" {
		return "Shipping & delivery"
	}
	lower := strings.ToLower(method)
	if strings.HasPrefix(lower, "shipping") {
		return method
	}
	label := HumanizeShippingMethod(method)
	if label == "" {
		return "Shipping & delivery"
	}
	return "Shipping & delivery (" + label + ")"
}
