package shipping

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shipstation"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

const (
	CodeAddressIncomplete    = "address_incomplete"
	CodeAddressZipInvalid    = "address_zip_invalid"
	CodeAddressCityMismatch  = "address_city_mismatch"
	CodeAddressStateMismatch = "address_state_mismatch"
	CodeAddressRejected      = "address_rejected"
	CodePackageEmpty         = "package_empty"
	CodePackageData          = "package_data"
	CodeWarehouseOrigin      = "warehouse_origin"
	CodeCarrierNoRate        = "carrier_no_rate"
	CodeCarrierUnavailable   = "carrier_unavailable"
)

// RateError is a classified shipping failure with a stable code and field details.
type RateError struct {
	Code    string
	Message string
	Details map[string]string
}

func (e *RateError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func newRateError(code, message string, details map[string]string) *RateError {
	if details == nil {
		details = map[string]string{}
	}
	return &RateError{Code: code, Message: message, Details: details}
}

// AddressIncomplete reports ship-to fields that were empty.
func AddressIncomplete(fields []string) *RateError {
	labels := make([]string, 0, len(fields))
	details := make(map[string]string, len(fields))
	for _, field := range fields {
		label, detailKey, detail := incompleteField(field)
		labels = append(labels, label)
		details[detailKey] = detail
	}
	return newRateError(
		CodeAddressIncomplete,
		"Shipping address is incomplete. Missing: "+strings.Join(labels, ", ")+".",
		details,
	)
}

func incompleteField(field string) (label, detailKey, detail string) {
	switch field {
	case "name":
		return "name", "name", "Recipient name is required."
	case "phone":
		return "phone", "phone", "Phone number is required."
	case "address", "address1":
		return "street", "address", "Street address is required."
	case "city":
		return "city", "city", "City is required."
	case "state":
		return "state", "state", "State is required."
	case "zip":
		return "ZIP", "zip", "ZIP code is required."
	case "country":
		return "country", "country", "Country is required."
	default:
		return field, field, field + " is required."
	}
}

// ZipInvalid reports a US ZIP that is not a 5-digit code or is not in the ZIP table.
func ZipInvalid(zip string) *RateError {
	zip = strings.TrimSpace(zip)
	msg := fmt.Sprintf("ZIP %s is not a valid US ZIP.", zip)
	return newRateError(CodeAddressZipInvalid, msg, map[string]string{"zip": msg})
}

// CityMismatch reports a city that does not belong to the ZIP.
func CityMismatch(zip, city, expected string) *RateError {
	msg := fmt.Sprintf("City does not match ZIP %s. Expected %s.", zip, expected)
	return newRateError(CodeAddressCityMismatch, msg, map[string]string{
		"city":          msg,
		"zip":           zip,
		"expected_city": expected,
	})
}

// StateMismatch reports a state that does not belong to the ZIP.
func StateMismatch(zip, state, expected string) *RateError {
	msg := fmt.Sprintf("State does not match ZIP %s. Expected %s.", zip, expected)
	return newRateError(CodeAddressStateMismatch, msg, map[string]string{
		"state":          msg,
		"zip":            zip,
		"expected_state": expected,
	})
}

// PackageEmpty means there were shippable items but no boxes to rate.
func PackageEmpty() *RateError {
	return newRateError(CodePackageEmpty, "No shippable boxes.", nil)
}

// PackageData reports items whose weight is missing.
func PackageData(labels []string) *RateError {
	joined := strings.Join(labels, ", ")
	msg := "Package weight is missing for " + joined + "."
	return newRateError(CodePackageData, msg, map[string]string{"sku": joined})
}

// WarehouseOrigin reports a missing or incomplete ship-from warehouse.
func WarehouseOrigin(message string) *RateError {
	if strings.TrimSpace(message) == "" {
		message = "Warehouse origin could not be resolved."
	}
	return newRateError(CodeWarehouseOrigin, message, nil)
}

// CarrierNoRate means the carrier answered but did not return the ground service.
func CarrierNoRate(serviceCode, zip string) *RateError {
	msg := fmt.Sprintf("No ground rate for %s to ZIP %s.", serviceCode, zip)
	return newRateError(CodeCarrierNoRate, msg, map[string]string{
		"service_code": serviceCode,
		"zip":          zip,
	})
}

// ToAPIError maps a classified rate error onto the HTTP API error type.
func ToAPIError(err error) error {
	var rateErr *RateError
	if !errors.As(err, &rateErr) || rateErr == nil {
		return err
	}
	return apierror.NewWithDetails(statusForCode(rateErr.Code), rateErr.Code, rateErr.Message, rateErr.Details)
}

func statusForCode(code string) int {
	switch code {
	case CodeAddressIncomplete, CodeAddressZipInvalid, CodeAddressCityMismatch, CodeAddressStateMismatch, CodeAddressRejected, CodeCarrierNoRate:
		return http.StatusUnprocessableEntity
	case CodePackageEmpty, CodePackageData:
		return http.StatusBadRequest
	case CodeCarrierUnavailable:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

// ClassifyCarrierFailure turns a ShipStation or transport error into a RateError.
// missingDimensionSKUs are attached when the carrier rejects the shipment.
func ClassifyCarrierFailure(err error, missingDimensionSKUs []string) error {
	if err == nil {
		return nil
	}
	var rateErr *RateError
	if errors.As(err, &rateErr) {
		return attachMissingDimensions(rateErr, missingDimensionSKUs)
	}

	var apiErr *shipstation.APIError
	if errors.As(err, &apiErr) {
		return attachMissingDimensions(classifyAPIError(apiErr), missingDimensionSKUs)
	}

	var respErr *shipstation.RateResponseError
	if errors.As(err, &respErr) {
		return attachMissingDimensions(classifyMessages(respErr.Messages), missingDimensionSKUs)
	}

	if isTimeout(err) {
		return attachMissingDimensions(newRateError(
			CodeCarrierUnavailable,
			"ShipStation is unavailable. Try again.",
			map[string]string{"reason": err.Error()},
		), missingDimensionSKUs)
	}

	return attachMissingDimensions(newRateError(
		CodeCarrierUnavailable,
		"ShipStation is unavailable. Try again.",
		map[string]string{"reason": err.Error()},
	), missingDimensionSKUs)
}

func classifyAPIError(apiErr *shipstation.APIError) *RateError {
	if apiErr.StatusCode >= 500 || apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden {
		msg := "ShipStation is unavailable. Try again."
		if strings.TrimSpace(apiErr.Message) != "" {
			msg = "ShipStation is unavailable. " + strings.TrimSpace(apiErr.Message)
		}
		return newRateError(CodeCarrierUnavailable, msg, map[string]string{
			"status": fmt.Sprintf("%d", apiErr.StatusCode),
		})
	}
	return classifyText(apiErr.Message, apiErr.Message)
}

func classifyMessages(messages []shipstation.CarrierMessage) *RateError {
	var parts []string
	for _, msg := range messages {
		parts = append(parts, strings.TrimSpace(strings.Join([]string{msg.ErrorCode, msg.ErrorType, msg.Message}, " ")))
	}
	combined := strings.TrimSpace(strings.Join(parts, "; "))
	display := firstCarrierMessage(messages)
	if display == "" {
		display = combined
	}
	return classifyText(combined, display)
}

func classifyText(haystack, display string) *RateError {
	text := strings.ToLower(haystack)
	display = strings.TrimSpace(display)
	if display == "" {
		display = "The carrier rejected the shipment."
	}
	switch {
	case containsAny(text, "address", "postal", "zip", "city", "state", "province"):
		return newRateError(CodeAddressRejected, "Carrier rejected the shipping address: "+display, map[string]string{
			"carrier_message": display,
		})
	case containsAny(text, "weight", "dimension", "package"):
		return newRateError(CodePackageData, "Carrier rejected the package: "+display, map[string]string{
			"carrier_message": display,
		})
	case containsAny(text, "service", "no rate", "no rates"):
		return newRateError(CodeCarrierNoRate, "No carrier rate is available: "+display, map[string]string{
			"carrier_message": display,
		})
	default:
		return newRateError(CodeCarrierUnavailable, "ShipStation is unavailable. "+display, map[string]string{
			"carrier_message": display,
		})
	}
}

func firstCarrierMessage(messages []shipstation.CarrierMessage) string {
	for _, msg := range messages {
		if strings.TrimSpace(msg.Message) != "" {
			return strings.TrimSpace(msg.Message)
		}
	}
	return ""
}

func containsAny(text string, words ...string) bool {
	for _, word := range words {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func attachMissingDimensions(err *RateError, skus []string) *RateError {
	if err == nil || len(skus) == 0 {
		return err
	}
	switch err.Code {
	case CodeAddressRejected, CodePackageData, CodeCarrierNoRate, CodeCarrierUnavailable:
	default:
		return err
	}
	if err.Details == nil {
		err.Details = map[string]string{}
	}
	err.Details["missing_dimensions"] = strings.Join(skus, ", ")
	return err
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
