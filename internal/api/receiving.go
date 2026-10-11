package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"umirhack/internal/store"
)

// Коды ошибок подтверждения срока (в 7.4 не перечислены).
const (
	CodeBatchClosed      = "batch_closed"
	CodeExpiryNotFuture  = "expiry_not_future"
	actionExpiryConfirm  = "batch_expiry_confirmed"
	receivingMaxShelfHrs = maxShelfHours
)

// Receiving — то, что нужно маршрутам приёмки от хранилища (2.1а).
type Receiving interface {
	ConfirmExpiry(ctx context.Context, in store.ExpiryInput, now func() time.Time, loc *time.Location) (store.ExpiryResult, error)
	ListReceiving(ctx context.Context, now time.Time) ([]store.ReceivingBatch, error)
}

func (rt *Router) receivingRoutes() {
	if rt.deps.Receiving == nil {
		return
	}
	rt.Handle(http.MethodGet, "/api/receiving", rt.getReceiving)
	rt.Handle(http.MethodPatch, "/api/batches/{id}/expiry", rt.patchExpiry)
}

// getReceiving — GET /api/receiving: партии с неподтверждённым сроком за 48 ч стенда.
func (rt *Router) getReceiving(w http.ResponseWriter, r *http.Request) {
	items, err := rt.deps.Receiving.ListReceiving(r.Context(), rt.deps.Now().UTC())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, items)
}

// patchExpiry — PATCH /api/batches/{id}/expiry {"expires_on"} | {"shelf_life_hours"}; роль operator.
func (rt *Router) patchExpiry(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePositiveID(r.PathValue("id"))
	if !ok {
		validationError(w, "id", "batch id in the path must be a positive integer")
		return
	}
	var in struct {
		ExpiresOn      *string `json:"expires_on"`
		ShelfLifeHours *int    `json:"shelf_life_hours"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	var ei store.ExpiryInput
	switch {
	case (in.ExpiresOn == nil) == (in.ShelfLifeHours == nil):
		validationError(w, "expires_on", "exactly one of expires_on and shelf_life_hours is required")
		return
	case in.ExpiresOn != nil:
		if _, err := time.ParseInLocation("2006-01-02", *in.ExpiresOn, time.UTC); err != nil {
			validationError(w, "expires_on", "expires_on must be a date YYYY-MM-DD")
			return
		}
		ei.ExpiresOn = *in.ExpiresOn
	default:
		if *in.ShelfLifeHours < 1 || *in.ShelfLifeHours > receivingMaxShelfHrs {
			validationError(w, "shelf_life_hours", "shelf_life_hours must be a positive integer within the limit")
			return
		}
		ei.ShelfLifeHours = *in.ShelfLifeHours
	}
	ei.BatchID = id
	res, err := rt.deps.Receiving.ConfirmExpiry(r.Context(), ei, rt.deps.Now, rt.location())
	switch {
	case err == nil:
	case errors.Is(err, store.ErrUnknownBatch):
		WriteError(w, http.StatusNotFound, CodeUnknownBatch, "unknown batch", map[string]any{"batch_id": id})
		return
	case errors.Is(err, store.ErrBatchClosed):
		WriteError(w, http.StatusConflict, CodeBatchClosed, "batch is expired or written off", map[string]any{"batch_id": id})
		return
	case errors.Is(err, store.ErrExpiryNotFuture):
		WriteError(w, http.StatusConflict, CodeExpiryNotFuture, "new expiry must be later than the current stand time", map[string]any{"batch_id": id})
		return
	default:
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, actionExpiryConfirm,
		slog.Int64("batch_id", res.BatchID), slog.String("product_id", res.ProductID),
		slog.Time("old_expires_at", res.OldExpiresAt), slog.Time("new_expires_at", res.ExpiresAt),
		slog.String("old_expiry_source", res.OldSource))
	WriteJSON(w, http.StatusOK, res)
}

func parsePositiveID(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n >= 1
}
