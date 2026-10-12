package api

import (
	"errors"
	"net/http"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

// openedRoutes — вскрытие упаковки (docs/opened.md). Нет Corrections — маршрут не регистрируется.
func (rt *Router) openedRoutes() {
	if rt.deps.Corrections == nil {
		return
	}
	rt.Handle(http.MethodPost, "/api/batches/{id}/open", rt.postOpen)
}

// postOpen — POST /api/batches/{id}/open {id, qty?, unit?}; роль operator, идемпотентно по id.
func (rt *Router) postOpen(w http.ResponseWriter, r *http.Request) {
	batchID, ok := parsePositiveID(r.PathValue("id"))
	if !ok {
		validationError(w, "id", "batch id in the path must be a positive integer")
		return
	}
	var in struct {
		ID   string  `json:"id"`
		Qty  *string `json:"qty"`
		Unit string  `json:"unit"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if !validLabel(in.ID) {
		validationError(w, "id", "id must be 1..128 characters without control characters or edge spaces")
		return
	}
	if in.Qty == nil && in.Unit != "" {
		validationError(w, "unit", "unit needs qty")
		return
	}
	oi := store.OpenInput{ID: in.ID, BatchID: batchID}
	if in.Qty != nil {
		_, base, err := rt.deps.Corrections.BatchProduct(r.Context(), batchID)
		if err != nil {
			if !correctionError(w, err, map[string]any{"batch_id": batchID}) {
				rt.internalError(w, r, err)
			}
			return
		}
		qty, err := parseQty(*in.Qty, in.Unit, base, false)
		if err != nil {
			validationError(w, "qty", quantityMessage(err, "qty"))
			return
		}
		oi.Qty = &qty
	}
	res, err := rt.deps.Corrections.OpenBatch(r.Context(), oi, rt.deps.Now)
	if err != nil {
		details := map[string]any{"id": in.ID, "batch_id": batchID}
		switch {
		case errors.Is(err, domain.ErrOpenNoShelf):
			WriteError(w, http.StatusBadRequest, CodeValidation, "вскрытие не меняет срок: у продукта нет shelf_after_open_hours", details)
		case errors.Is(err, domain.ErrOpenQty):
			WriteError(w, http.StatusBadRequest, CodeValidation, "qty must be positive and not exceed the batch remainder", map[string]any{"field": "qty", "batch_id": batchID})
		case errors.Is(err, domain.ErrOpenClosed):
			WriteError(w, http.StatusConflict, CodeBatchClosed, "batch is expired or empty", details)
		case !correctionError(w, err, details):
			rt.internalError(w, r, err)
		}
		return
	}
	WriteJSON(w, replayStatus(res.Replayed), res)
}
