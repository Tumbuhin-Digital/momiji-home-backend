package validator

import (
	"fmt"
	"github.com/go-playground/validator/v10"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"net/http"
)

var validate = validator.New()

func ValidateStruct(s interface{}) error {
	err := validate.Struct(s)
	if err != nil {
		if _, ok := err.(*validator.InvalidValidationError); ok {
			return err
		}

		details := make(map[string]string)
		for _, err := range err.(validator.ValidationErrors) {
			details[err.Field()] = fmt.Sprintf("failed on the '%s' tag", err.Tag())
		}
		
		return apierror.NewWithDetails(http.StatusBadRequest, "validation_error", "Validation failed", details)
	}
	return nil
}
