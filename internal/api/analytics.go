package api

import (
	"context"
	"net/http"
	"time"

	"umirhack/internal/store"
)

// Analytics — аналитика в рублях (7.4, «Аналитика»). nil в Deps.Analytics — маршруты не регистрируются.
type Analytics interface {
	WriteoffsReport(ctx context.Context, from, to time.Time, loc *time.Location) (store.WriteoffsReport, error)
	ShortfallsReport(ctx context.Context, from, to time.Time, loc *time.Location) (store.ShortfallsReport, error)
	ShortagesReport(ctx context.Context, from, to time.Time) (store.ShortagesReport, error)
}

// defaultAnalyticsDays — период по умолчанию, если from не задан: последние 7 суток до to.
const defaultAnalyticsDays = 7

func (rt *Router) analyticsRoutes() {
	rt.Handle(http.MethodGet, "/api/analytics/bench", rt.getBench)
	if rt.deps.Analytics == nil {
		return
	}
	rt.Handle(http.MethodGet, "/api/analytics/writeoffs", rt.getWriteoffs)
	rt.Handle(http.MethodGet, "/api/analytics/shortfalls", rt.getShortfalls)
	rt.Handle(http.MethodGet, "/api/analytics/shortages", rt.getShortages)
}

// analyticsPeriod разбирает ?from=&to=. Значение — дата YYYY-MM-DD (сутки по часовому поясу
// ресторана; to включительно) или момент RFC 3339 (период [from, to)). Без to — «сейчас»,
// без from — defaultAnalyticsDays суток до to.
func (rt *Router) analyticsPeriod(w http.ResponseWriter, r *http.Request) (from, to time.Time, ok bool) {
	loc := rt.deps.Location
	if loc == nil {
		loc = time.UTC
	}
	q := r.URL.Query()
	parse := func(field string, endOfDay bool) (time.Time, bool, bool) {
		v := q.Get(field)
		if v == "" {
			return time.Time{}, false, true
		}
		if d, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
			if endOfDay {
				d = d.AddDate(0, 0, 1)
			}
			return d.UTC(), true, true
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.UTC(), true, true
		}
		validationError(w, field, field+" must be a date YYYY-MM-DD or an RFC 3339 timestamp")
		return time.Time{}, false, false
	}
	var hasFrom, hasTo bool
	if to, hasTo, ok = parse("to", true); !ok {
		return
	}
	if from, hasFrom, ok = parse("from", false); !ok {
		return
	}
	if !hasTo {
		to = rt.deps.Now().UTC()
	}
	if !hasFrom {
		from = to.AddDate(0, 0, -defaultAnalyticsDays)
	}
	if !from.Before(to) {
		validationError(w, "from", "from must be before to")
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

// getWriteoffs — GET /api/analytics/writeoffs?from=&to=: списания по продуктам и причинам, копейки.
func (rt *Router) getWriteoffs(w http.ResponseWriter, r *http.Request) {
	from, to, ok := rt.analyticsPeriod(w, r)
	if !ok {
		return
	}
	rep, err := rt.deps.Analytics.WriteoffsReport(r.Context(), from, to, rt.deps.Location)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, rep)
}

// getShortfalls — GET /api/analytics/shortfalls?from=&to=: расхождения кассы по продуктам, копейки.
func (rt *Router) getShortfalls(w http.ResponseWriter, r *http.Request) {
	from, to, ok := rt.analyticsPeriod(w, r)
	if !ok {
		return
	}
	rep, err := rt.deps.Analytics.ShortfallsReport(r.Context(), from, to, rt.deps.Location)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, rep)
}

// getShortages — GET /api/analytics/shortages?from=&to=: продукты по числу отклонённых заказов и недостающему количеству.
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
