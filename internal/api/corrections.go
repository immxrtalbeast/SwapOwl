package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/domain/units"
	"umirhack/internal/store"
)

// Коды ошибок корректировок остатка (в 7.4 не перечислены).
const (
	CodeUnknownBatch        = "unknown_batch"
	CodeUnknownOrder        = "unknown_order"
	CodeCorrectionConflict  = "correction_conflict"
	CodeInsufficientBatch   = "insufficient_batch"
	CodeOrderNotCancellable = "order_not_cancellable"
	CodeAlreadyCancelled    = "already_cancelled"
)

// Corrections — то, что нужно маршрутам корректировок от хранилища.
type Corrections interface {
	ProductUnits(ctx context.Context, ids []string) (map[string]domain.Unit, error)
	BatchProduct(ctx context.Context, batchID int64) (string, domain.Unit, error)
	WriteOffBatch(ctx context.Context, in store.WriteoffInput, now func() time.Time) (store.WriteoffResult, error)
	Inventory(ctx context.Context, in store.InventoryInput, now func() time.Time) (store.InventoryResult, error)
	CancelOrder(ctx context.Context, in store.CancelInput, now func() time.Time) (store.CancelResult, error)
}

func (rt *Router) correctionRoutes() {
	if rt.deps.Corrections == nil {
		return
	}
	rt.Handle(http.MethodPost, "/api/writeoffs", rt.postWriteoff)
	rt.Handle(http.MethodPost, "/api/inventory", rt.postInventory)
	rt.Handle(http.MethodPost, "/api/orders/{id}/cancel", rt.postCancel)
}

// parseQty разбирает количество корректировки; allowZero — для пересчёта (на складе может быть 0).
func parseQty(s, unit string, base domain.Unit, allowZero bool) (int64, error) {
	if unit == "" {
		unit = string(base)
	}
	q, err := units.Parse(s, unit, units.Unit(base))
	if errors.Is(err, units.ErrZero) && allowZero {
		return 0, nil
	}
	return int64(q), err
}

func correctionError(w http.ResponseWriter, err error, details map[string]any) bool {
	switch {
	case errors.Is(err, store.ErrCorrectionConflict):
		WriteError(w, http.StatusConflict, CodeCorrectionConflict, "id already used with different content", details)
	case errors.Is(err, store.ErrUnknownBatch):
		WriteError(w, http.StatusNotFound, CodeUnknownBatch, "unknown batch", details)
	case errors.Is(err, store.ErrUnknownOrder):
		WriteError(w, http.StatusNotFound, CodeUnknownOrder, "unknown order", details)
	case errors.Is(err, store.ErrUnknownProduct):
		WriteError(w, http.StatusBadRequest, CodeUnknownProduct, "unknown product", details)
	case errors.Is(err, store.ErrInsufficientBatch):
		WriteError(w, http.StatusConflict, CodeInsufficientBatch, "batch has less than requested", details)
	case errors.Is(err, store.ErrOrderNotCancellable):
		WriteError(w, http.StatusConflict, CodeOrderNotCancellable, "only accepted orders can be cancelled", details)
	case errors.Is(err, store.ErrAlreadyCancelled):
		WriteError(w, http.StatusConflict, CodeAlreadyCancelled, "order already cancelled", details)
	case errors.Is(err, store.ErrInventoryNoShelf):
		WriteError(w, http.StatusBadRequest, CodeValidation, "product has no default shelf life: the surplus batch needs one", details)
	default:
		return false
	}
	return true
}

func replayStatus(replayed bool) int {
	if replayed {
		return http.StatusOK
	}
	return http.StatusCreated
}

func (rt *Router) postWriteoff(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID      string `json:"id"`
		BatchID *int64 `json:"batch_id"`
		Qty     string `json:"qty"`
		Unit    string `json:"unit"`
		Reason  string `json:"reason"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	switch {
	case !validLabel(in.ID):
		validationError(w, "id", "id must be 1..128 characters without control characters or edge spaces")
		return
	case in.BatchID == nil || *in.BatchID < 1:
		validationError(w, "batch_id", "batch_id must be a positive integer")
		return
	case in.Reason != store.ReasonSpoilage && in.Reason != store.ReasonDamage:
		validationError(w, "reason", `reason must be "spoilage" or "damage"`)
		return
	}
	_, base, err := rt.deps.Corrections.BatchProduct(r.Context(), *in.BatchID)
	if err != nil {
		if !correctionError(w, err, map[string]any{"batch_id": *in.BatchID}) {
			rt.internalError(w, r, err)
		}
		return
	}
	qty, err := parseQty(in.Qty, in.Unit, base, false)
	if err != nil {
		validationError(w, "qty", quantityMessage(err, "qty"))
		return
	}
	res, err := rt.deps.Corrections.WriteOffBatch(r.Context(), store.WriteoffInput{ID: in.ID, BatchID: *in.BatchID, Qty: qty, Reason: in.Reason}, rt.deps.Now)
	if err != nil {
		if !correctionError(w, err, map[string]any{"id": in.ID, "batch_id": *in.BatchID}) {
			rt.internalError(w, r, err)
		}
		return
	}
	WriteJSON(w, replayStatus(res.Replayed), res)
}

func (rt *Router) postInventory(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID        string `json:"id"`
		ProductID string `json:"product_id"`
		Counted   string `json:"counted"`
		Unit      string `json:"unit"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	switch {
	case !validLabel(in.ID):
		validationError(w, "id", "id must be 1..128 characters without control characters or edge spaces")
		return
	case !validLabel(in.ProductID):
		validationError(w, "product_id", "product_id must be 1..128 characters without control characters or edge spaces")
		return
	}
	known, err := rt.deps.Corrections.ProductUnits(r.Context(), []string{in.ProductID})
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	base, ok := known[in.ProductID]
	if !ok {
		WriteError(w, http.StatusBadRequest, CodeUnknownProduct, "product_id: unknown product", map[string]any{"field": "product_id"})
		return
	}
	counted, err := parseQty(in.Counted, in.Unit, base, true)
	if err != nil {
		validationError(w, "counted", quantityMessage(err, "counted"))
		return
	}
	res, err := rt.deps.Corrections.Inventory(r.Context(), store.InventoryInput{ID: in.ID, ProductID: in.ProductID, Counted: counted}, rt.deps.Now)
	if err != nil {
		if !correctionError(w, err, map[string]any{"id": in.ID, "product_id": in.ProductID}) {
			rt.internalError(w, r, err)
		}
		return
	}
	WriteJSON(w, replayStatus(res.Replayed), res)
}

func (rt *Router) postCancel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	orderID := strings.ToLower(r.PathValue("id"))
	if !uuidRe.MatchString(orderID) {
		validationError(w, "id", "order id in the path must be a UUID")
		return
	}
	if !validLabel(in.ID) {
		validationError(w, "id", "id must be 1..128 characters without control characters or edge spaces")
		return
	}
	res, err := rt.deps.Corrections.CancelOrder(r.Context(), store.CancelInput{ID: in.ID, OrderID: orderID}, rt.deps.Now)
	if err != nil {
		if !correctionError(w, err, map[string]any{"id": in.ID, "order_id": orderID}) {
			rt.internalError(w, r, err)
		}
		return
	}
	WriteJSON(w, replayStatus(res.Replayed), res)
}
