package api

import (
	"net/http"
	"strconv"

	"umirhack/internal/audit"
	"umirhack/internal/auth"
)

// AuditLog — журнал действий в БД (internal/store).
type AuditLog interface {
	audit.Sink
}

const (
	auditDefaultLimit = 100
	auditMaxLimit     = 1000
)

// auditRoutes регистрирует чтение журнала; без Deps.Audit маршрута нет.
func (rt *Router) auditRoutes() {
	if rt.deps.Audit == nil {
		return
	}
	rt.HandleRole(http.MethodGet, "/api/audit", auth.Admin, rt.listAudit)
}

// listAudit — GET /api/audit?limit=: последние записи, новые сверху. Только admin.
func (rt *Router) listAudit(w http.ResponseWriter, r *http.Request) {
	limit := auditDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > auditMaxLimit {
			validationError(w, "limit", "limit must be an integer 1.."+strconv.Itoa(auditMaxLimit))
			return
		}
		limit = n
	}
	items, err := rt.deps.Audit.ListAudit(r.Context(), limit)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
