package api

import "net/http"

// listProducts — GET /api/products: остатки по продуктам. Время — только из часов стенда.
func (rt *Router) listProducts(w http.ResponseWriter, r *http.Request) {
	items, err := rt.deps.Stock.ListProductStock(r.Context(), rt.deps.Now().UTC())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, items)
}

// listBatches — GET /api/batches?product_id=&status=ok|expiring|expired.
func (rt *Router) listBatches(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	if status != "" && status != "ok" && status != "expiring" && status != "expired" {
		validationError(w, "status", "status must be ok, expiring or expired")
		return
	}
	items, err := rt.deps.Stock.ListBatches(r.Context(), rt.deps.Now().UTC(), q.Get("product_id"), status)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, items)
}
