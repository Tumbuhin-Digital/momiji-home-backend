package apierror

import (
	"net/http"
)

type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
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

// Common errors
var (
	ErrUnauthorized = New(http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or missing token")
	ErrForbidden    = New(http.StatusForbidden, "FORBIDDEN", "You don't have permission to perform this action")
	ErrNotFound     = New(http.StatusNotFound, "NOT_FOUND", "Resource not found")
	ErrInternal     = New(http.StatusInternalServerError, "INTERNAL_ERROR", "An internal error occurred")
	ErrBadRequest   = New(http.StatusBadRequest, "BAD_REQUEST", "Invalid request body or parameters")
)
