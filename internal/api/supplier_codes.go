package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"

	"umirhack/internal/store"
)

// SupplierCodes — сопоставление кодов поставщика с продуктами. nil — маршруты не регистрируются.
type SupplierCodes interface {
	ListSupplierCodes(ctx context.Context) ([]store.SupplierCode, error)
	SetSupplierCode(ctx context.Context, c store.SupplierCode) (store.SupplierCode, error)
	DeleteSupplierCode(ctx context.Context, code string) error
}

// supplierCodeRe — GTIN, артикул поставщика: латиница, цифры и . _ - до 64 знаков. Без пробелов и кириллицы,
// чтобы код из маркировки и из накладной сравнивался побайтово.
var supplierCodeRe = regexp.MustCompile(`^[0-9A-Za-z._-]{1,64}$`)

func (rt *Router) supplierCodeRoutes() {
	if rt.deps.SupplierCodes == nil {
		return
	}
	rt.Handle(http.MethodGet, "/api/supplier-codes", rt.listSupplierCodes)
	rt.Handle(http.MethodPut, "/api/supplier-codes/{code}", rt.putSupplierCode)
	rt.Handle(http.MethodDelete, "/api/supplier-codes/{code}", rt.deleteSupplierCode)
}

// listSupplierCodes — GET /api/supplier-codes (viewer): новые первыми.
func (rt *Router) listSupplierCodes(w http.ResponseWriter, r *http.Request) {
	list, err := rt.deps.SupplierCodes.ListSupplierCodes(r.Context())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, list)
}

// putSupplierCode — PUT /api/supplier-codes/{code} {"product_id"} (operator): код поставщика → продукт.
// Повтор идемпотентен, прежний продукт заменяется.
func (rt *Router) putSupplierCode(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !supplierCodeRe.MatchString(code) {
		validationError(w, "code", "code — 1–64 знака: латиница, цифры, точка, подчёркивание, дефис")
		return
	}
	var in struct {
		ProductID string `json:"product_id"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if !validLabel(in.ProductID) {
		validationError(w, "product_id", "product_id — от 1 до 128 символов, без управляющих символов и пробелов по краям")
		return
	}
	by := ""
	if p, ok := PrincipalFrom(r.Context()); ok {
		by = p.Login
	}
	c, err := rt.deps.SupplierCodes.SetSupplierCode(r.Context(), store.SupplierCode{Code: code, ProductID: in.ProductID, SetAt: rt.deps.Now().UTC(), SetBy: by})
	if errors.Is(err, store.ErrSupplierCodeProduct) {
		WriteError(w, http.StatusNotFound, CodeUnknownProduct, "продукта нет в справочнике", map[string]any{"product_id": in.ProductID})
		return
	}
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "supplier_code_set", slog.String("code", code), slog.String("product_id", in.ProductID))
	WriteJSON(w, http.StatusOK, c)
}

// deleteSupplierCode — DELETE /api/supplier-codes/{code} (operator): повтор тоже 204.
func (rt *Router) deleteSupplierCode(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !supplierCodeRe.MatchString(code) {
		validationError(w, "code", "code — 1–64 знака: латиница, цифры, точка, подчёркивание, дефис")
		return
	}
	if err := rt.deps.SupplierCodes.DeleteSupplierCode(r.Context(), code); err != nil {
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "supplier_code_delete", slog.String("code", code))
	w.WriteHeader(http.StatusNoContent)
}
