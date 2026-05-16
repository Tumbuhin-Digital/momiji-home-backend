package validator

import (
	"fmt"
	"github.com/go-playground/validator/v10"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"net/http"
	"strings"
)

var validate = validator.New()

func ValidateStruct(s interface{}) error {
	err := validate.Struct(s)
	if err != nil {
		if _, ok := err.(*validator.InvalidValidationError); ok {
			return err
		}

		var errMsgs []string
		for _, err := range err.(validator.ValidationErrors) {
			errMsgs = append(errMsgs, fmt.Sprintf("%s is invalid: %s", err.Field(), err.Tag()))
		}
		
		return apierror.New(http.StatusBadRequest, "VALIDATION_ERROR", strings.Join(errMsgs, ", "))
	}
	return nil
}
