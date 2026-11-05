package timesync

import (
	"reflect"

	"github.com/go-playground/validator/v10"
)

func NewValidator() StructValidator {
	v := validator.New(validator.WithRequiredStructEnabled())

	// `config.Duration`
	v.RegisterCustomTypeFunc(func(field reflect.Value) interface{} {
		if d, ok := field.Interface().(Duration); ok {
			return d.Duration
		}
		return nil
	}, Duration{})

	return func(a any) validator.ValidationErrors {
		err := v.Struct(a)
		if err == nil {
			return nil
		}
		if ve, ok := err.(validator.ValidationErrors); ok {
			return ve
		}
		return nil
	}
}
