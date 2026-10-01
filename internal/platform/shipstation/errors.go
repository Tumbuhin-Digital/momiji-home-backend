package shipstation

import "fmt"

// CarrierMessage is one error entry from a ShipStation rate response.
type CarrierMessage struct {
	ErrorSource string `json:"error_source,omitempty"`
	ErrorType   string `json:"error_type,omitempty"`
	ErrorCode   string `json:"error_code,omitempty"`
	Message     string `json:"message,omitempty"`
}

// APIError is a non-2xx response from ShipStation.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e == nil {
		return "shipstation: api error"
	}
	return fmt.Sprintf("shipstation: api error status %d: %s", e.StatusCode, e.Message)
}

// RateResponseError is a 200 response whose rate_response contains errors or invalid rates.
type RateResponseError struct {
	Messages []CarrierMessage
}

func (e *RateResponseError) Error() string {
	if e == nil || len(e.Messages) == 0 {
		return "shipstation returned errors"
	}
	if e.Messages[0].Message != "" {
		return "shipstation returned errors: " + e.Messages[0].Message
	}
	return "shipstation returned errors: " + e.Messages[0].ErrorCode
}
