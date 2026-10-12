package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"umirhack/internal/store"
)

// Лимиты тела POST /api/supply-orders: количество в базовых единицах не больше 10^12 (2.2).
const (
	maxSupplyOrderItems = 200
	maxSupplyOrderQty   = int64(1_000_000_000_000)
)

// SupplyOrders — заказ поставщику через outbox (FS-121). nil в Deps.SupplyOrders — маршруты не регистрируются.
type SupplyOrders interface {
	EnqueueSupplyOrder(ctx context.Context, id string, items []store.SupplyOrderItem, now time.Time) (store.SupplyOrder, error)
	GetSupplyOrder(ctx context.Context, id string) (store.SupplyOrder, error)
}

func (rt *Router) supplyOrderRoutes() {
	if rt.deps.SupplyOrders == nil {
		return
	}
	rt.Handle(http.MethodPost, "/api/supply-orders", rt.postSupplyOrder)
	rt.Handle(http.MethodGet, "/api/supply-orders/{id}", rt.getSupplyOrder)
}

type supplyOrderRequest struct {
	ID    string `json:"id"`
	Items []struct {
		ProductID string `json:"product_id"`
		Qty       int64  `json:"qty"`
	} `json:"items"`
}

// postSupplyOrder — POST /api/supply-orders: 201 — новый заказ в outbox, 200 — повтор того же id и состава.
func (rt *Router) postSupplyOrder(w http.ResponseWriter, r *http.Request) {
	var in supplyOrderRequest
	if !decodeBody(w, r, &in) {
		return
	}
	if !uuidRe.MatchString(in.ID) {
		validationError(w, "id", "id must be a client-generated UUID")
		return
	}
	if len(in.Items) == 0 || len(in.Items) > maxSupplyOrderItems {
		validationError(w, "items", "items must contain from 1 to 200 positions")
		return
	}
	items := make([]store.SupplyOrderItem, 0, len(in.Items))
	for _, it := range in.Items {
		if it.ProductID == "" {
			validationError(w, "items.product_id", "product_id is required")
			return
		}
		if it.Qty < 1 || it.Qty > maxSupplyOrderQty {
			validationError(w, "items.qty", "qty must be an integer from 1 to 10^12 in the product's base unit")
			return
		}
		items = append(items, store.SupplyOrderItem{ProductID: it.ProductID, Qty: it.Qty})
	}
	o, err := rt.deps.SupplyOrders.EnqueueSupplyOrder(r.Context(), in.ID, items, rt.deps.Now().UTC())
	switch {
	case errors.Is(err, store.ErrSupplyOrderConflict):
		WriteError(w, http.StatusConflict, CodeOrderConflict, "supply order id already used with different items", nil)
	case errors.Is(err, store.ErrSupplyOrderProduct):
		WriteError(w, http.StatusBadRequest, CodeUnknownProduct, err.Error(), nil)
	case err != nil:
		rt.internalError(w, r, err)
	case o.Replayed:
		WriteJSON(w, http.StatusOK, o)
	default:
		WriteJSON(w, http.StatusCreated, o)
	}
}

// getSupplyOrder — GET /api/supply-orders/{id}: статус по подтверждению outbox.
func (rt *Router) getSupplyOrder(w http.ResponseWriter, r *http.Request) {
	o, err := rt.deps.SupplyOrders.GetSupplyOrder(r.Context(), r.PathValue("id"))
	switch {
	case errors.Is(err, store.ErrSupplyOrderNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "supply order not found", nil)
	case err != nil:
		rt.internalError(w, r, err)
	default:
		WriteJSON(w, http.StatusOK, o)
	}
}
