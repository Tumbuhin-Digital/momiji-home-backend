package units

import (
	"math"
	"strings"
)

const (
	KgToLbFactor = 2.20462
	CmToInFactor = 0.393701
	DefaultLb    = 1.0
)

func KgToLb(kg float64) float64 {
	return kg * KgToLbFactor
}

func CmToIn(cm float64) float64 {
	return math.Round(cm*CmToInFactor*100) / 100
}

func GToKg(g float64) float64 {
	return g * 0.001
}

func OzToKg(oz float64) float64 {
	return oz * 0.0283495
}

func LbToKg(lb float64) float64 {
	return lb * 0.453592
}

func ShopifyWeightToKg(value float64, unit string) (kg float64, recognized bool) {
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "KILOGRAMS", "KILOGRAM", "KG", "":
		return value, true
	case "GRAMS", "GRAM", "G":
		return value / 1000.0, true
	case "OUNCES", "OUNCE", "OZ":
		return OzToKg(value), true
	case "POUNDS", "POUND", "LB", "LBS":
		return LbToKg(value), true
	default:
		return value, false
	}
}

func CartWeightToKg(value float64, unit string) (kg float64, recognized bool) {
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "KILOGRAMS", "":
		return value, true
	case "GRAMS":
		return GToKg(value), true
	case "OUNCES":
		return OzToKg(value), true
	case "POUNDS":
		return LbToKg(value), true
	default:
		return value, false
	}
}
