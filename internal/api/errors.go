// Package api — HTTP-каркас: роутер, формат ошибок, журнал запросов (.ai/solution.md, 7.4, 2.9).
package api

import (
	"encoding/json"
	"net/http"
)

// Коды ошибок из 7.4.
const (
	CodeValidation     = "validation_error"
	CodeUnknownProduct = "unknown_product"
	CodeUnknownDish    = "unknown_dish"
	CodeDeliveryConf   = "delivery_conflict"
	CodeOrderConflict  = "order_conflict"
	CodeInsufficient   = "insufficient_stock"
	CodeUnauthorized   = "unauthorized"
	CodeForbidden      = "forbidden"
	CodeTooManyTries   = "too_many_attempts"
	CodeOverloaded     = "overloaded"
	CodeInternal       = "internal"
	// CodeNotFound и CodeMethodNotAllowed в 7.4 не перечислены; каркас использует их для 404/405.
	CodeNotFound         = "not_found"
	CodeMethodNotAllowed = "method_not_allowed"
)

type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// WriteError пишет ответ-ошибку в едином формате. details может быть nil — тогда {}.
func WriteError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	WriteJSON(w, status, errorBody{Error: errorPayload{Code: code, Message: message, Details: details}})
}

// WriteJSON пишет JSON-ответ.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
