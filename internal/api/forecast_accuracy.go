package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"umirhack/internal/store"
)

// Границы ?days= у проверки точности прогноза.
const (
	accuracyDefaultDays = 7
	accuracyMaxDays     = 30
)

// AccuracySource — проверка точности прогноза спроса (store.Snapshotter). Маршрут есть, только если им
// является источник HotOffers: те же параметры окна и априора, что у горячих предложений.
type AccuracySource interface {
	ForecastAccuracy(ctx context.Context, now time.Time, days int) (store.AccuracyReport, error)
}

func (rt *Router) forecastAccuracyRoutes() {
	if _, ok := rt.deps.HotOffers.(AccuracySource); !ok {
		return
	}
	rt.Handle(http.MethodGet, "/api/analytics/forecast-accuracy", rt.getForecastAccuracy)
}

// getForecastAccuracy — GET /api/analytics/forecast-accuracy?days=7 (viewer): насколько точно движок угадывал спрос
// на прошедшие сутки (WAPE) с обучением и без него. Только чтение, пересчёт при каждом запросе.
func (rt *Router) getForecastAccuracy(w http.ResponseWriter, r *http.Request) {
	days := accuracyDefaultDays
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > accuracyMaxDays {
			validationError(w, "days", "days — целое число от 1 до 30")
			return
		}
		days = n
	}
	rep, err := rt.deps.HotOffers.(AccuracySource).ForecastAccuracy(r.Context(), rt.deps.Now().UTC(), days)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, rep)
}
