package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"umirhack/internal/metrics"
)

// metricsRoutes регистрирует GET /metrics (8.0а). Без Deps.Metrics или без MetricsToken ручка
// не отличается от несуществующей: 404, а не 401, чтобы не выдавать её наличие.
func (rt *Router) metricsRoutes() {
	if rt.deps.Metrics == nil {
		return
	}
	rt.HandlePublic(http.MethodGet, "/metrics", rt.metricsHandler(rt.deps.Metrics))
}

func (rt *Router) metricsHandler(reg *metrics.Registry) http.HandlerFunc {
	want := sha256.Sum256([]byte(rt.deps.MetricsToken))
	return func(w http.ResponseWriter, r *http.Request) {
		got := sha256.Sum256([]byte(bearerToken(r)))
		if rt.deps.MetricsToken == "" || subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			WriteError(w, http.StatusNotFound, CodeNotFound, "not found", nil)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_ = reg.Write(w)
	}
}
