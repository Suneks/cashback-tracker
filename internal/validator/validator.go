// internal/validator/validator.go
package validator

import (
	"regexp"
	"time"

	"github.com/go-playground/validator/v10"
)

var Validate *validator.Validate

func init() {
	Validate = validator.New()

	// Регистрируем кастомную валидацию для месяца: "2024-12"
	_ = Validate.RegisterValidation("yearmonth", func(fl validator.FieldLevel) bool {
		s := fl.Field().String()
		_, err := time.Parse("2006-01", s)
		return err == nil
	})

	// Регистрируем валидацию: строка не пустая и не только пробелы
	_ = Validate.RegisterValidation("notblank", func(fl validator.FieldLevel) bool {
		s := fl.Field().String()
		return len(regexp.MustCompile(`\S`).FindString(s)) > 0
	})
}
