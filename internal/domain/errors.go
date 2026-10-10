package domain

import "errors"

// Доменные ошибки. Проверка — через errors.Is.
var (
	ErrInsufficientStock = errors.New("insufficient stock")
	ErrUnknownProduct    = errors.New("unknown product")
	ErrUnknownDish       = errors.New("unknown dish")
	ErrDeliveryConflict  = errors.New("delivery conflict")
	ErrOrderConflict     = errors.New("order conflict")
	ErrValidation        = errors.New("validation error")
	ErrOverflow          = errors.New("quantity overflow")
)
