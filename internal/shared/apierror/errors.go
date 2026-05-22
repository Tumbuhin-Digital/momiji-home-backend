package apierror

import (
	"net/http"
)

type AppError struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
	Status  int         `json:"-"`
}

func (e *AppError) Error() string {
	return e.Message
}

func New(status int, code, message string) *AppError {
	return &AppError{
		Status:  status,
		Code:    code,
		Message: message,
	}
}

func NewWithDetails(status int, code, message string, details interface{}) *AppError {
	return &AppError{
		Status:  status,
		Code:    code,
		Message: message,
		Details: details,
	}
}

// Common errors
var (
	ErrUnauthorized = New(http.StatusUnauthorized, "unauthorized", "Invalid or missing token")
	ErrForbidden    = New(http.StatusForbidden, "forbidden", "You don't have permission to perform this action")
	ErrNotFound     = New(http.StatusNotFound, "not_found", "Resource not found")
	ErrInternal     = New(http.StatusInternalServerError, "internal_error", "An internal error occurred")
	ErrBadRequest   = New(http.StatusBadRequest, "validation_error", "Invalid request body or parameters")
)
