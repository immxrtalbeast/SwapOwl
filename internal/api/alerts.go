package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"umirhack/internal/store"
)

// Monitor — мониторинг сроков и журнал движений (2.6, 7.4). nil в Deps.Monitor — маршруты не регистрируются.
type Monitor interface {
	ListAlerts(ctx context.Context, now time.Time) ([]store.AlertView, error)
	ListMovements(ctx context.Context, productID, kind string, limit int) ([]store.MovementView, error)
}

func (rt *Router) monitorRoutes() {
	if rt.deps.Monitor == nil {
		return
	}
	rt.Handle(http.MethodGet, "/api/alerts", rt.listAlerts)
	rt.Handle(http.MethodGet, "/api/movements", rt.listMovements)
}

// listAlerts — GET /api/alerts: истекающие партии и просроченные за последние 24 ч стенда.
func (rt *Router) listAlerts(w http.ResponseWriter, r *http.Request) {
	items, err := rt.deps.Monitor.ListAlerts(r.Context(), rt.deps.Now().UTC())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, items)
}

// listMovements — GET /api/movements?product_id=&kind=&limit=: журнал, новые первыми.
func (rt *Router) listMovements(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > store.MaxMovementsLimit {
			validationError(w, "limit", "limit must be an integer from 1 to "+strconv.Itoa(store.MaxMovementsLimit))
			return
		}
		limit = n
	}
	items, err := rt.deps.Monitor.ListMovements(r.Context(), q.Get("product_id"), q.Get("kind"), limit)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, items)
}
