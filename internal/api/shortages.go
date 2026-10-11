package api

import "net/http"

// getShortages — GET /api/analytics/shortages?from=&to=: продукты по числу отклонённых заказов
// и недостающему количеству.
func (rt *Router) getShortages(w http.ResponseWriter, r *http.Request) {
	from, to, ok := rt.analyticsPeriod(w, r)
	if !ok {
		return
	}
	rep, err := rt.deps.Analytics.ShortagesReport(r.Context(), from, to)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, rep)
}
