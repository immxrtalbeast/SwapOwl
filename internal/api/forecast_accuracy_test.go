package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"umirhack/internal/store"
)

// accuracySrc — источник снимка с проверкой точности; запоминает, с какими days его позвали.
type accuracySrc struct {
	fakeSnap
	days int
	rep  store.AccuracyReport
}

func (a *accuracySrc) ForecastAccuracy(_ context.Context, _ time.Time, days int) (store.AccuracyReport, error) {
	a.days = days
	return a.rep, nil
}

func getAccuracy(rt *Router, query string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/analytics/forecast-accuracy"+query, nil))
	return rec
}

func TestForecastAccuracyDaysValidation(t *testing.T) {
	src := &accuracySrc{fakeSnap: fakeSnap{hotSnap()}, rep: store.AccuracyReport{Dishes: []store.DishAccuracyJSON{}}}
	rt := NewRouter(Deps{HotOffers: src, Now: func() time.Time { return hotNow }})
	for q, want := range map[string]int{"": 7, "?days=1": 1, "?days=30": 30, "?days=14": 14} {
		rec := getAccuracy(rt, q)
		if rec.Code != 200 || src.days != want {
			t.Errorf("%q: %d days=%d, ждали %d", q, rec.Code, src.days, want)
		}
	}
	for _, q := range []string{"?days=0", "?days=31", "?days=abc", "?days=-3", "?days=1.5"} {
		src.days = -1
		rec := getAccuracy(rt, q)
		if rec.Code != 400 || src.days != -1 {
			t.Errorf("%q: %d (источник вызван: %v)", q, rec.Code, src.days != -1)
		}
	}
}

func TestForecastAccuracyBodyShape(t *testing.T) {
	src := &accuracySrc{fakeSnap: fakeSnap{hotSnap()}, rep: store.AccuracyReport{Days: 7, DaysUsed: 6, EnoughData: true, WAPEPct: 23.4, BaselineWAPEPct: 41,
		Dishes: []store.DishAccuracyJSON{{DishID: "steak", DishName: "Стейк", ActualPerDay: 12.3, PredictedPerDay: 11, WAPEPct: 18.2, BaselineWAPEPct: 35}}}}
	rec := getAccuracy(NewRouter(Deps{HotOffers: src, Now: func() time.Time { return hotNow }}), "")
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	for _, k := range []string{"days", "days_used", "enough_data", "wape_pct", "baseline_wape_pct", "dishes"} {
		if _, ok := got[k]; !ok {
			t.Errorf("нет поля %q в %s", k, rec.Body)
		}
	}
}

// Без источника точности (обычный HotOffers) маршрута нет — стенд без поддержки ведёт себя как раньше.
func TestForecastAccuracyAbsentWithoutSource(t *testing.T) {
	rt := NewRouter(Deps{HotOffers: fakeSnap{hotSnap()}, Now: func() time.Time { return hotNow }})
	if rec := getAccuracy(rt, ""); rec.Code != 404 {
		t.Fatalf("%d", rec.Code)
	}
}
