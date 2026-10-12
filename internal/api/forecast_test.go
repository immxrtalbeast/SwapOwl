package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func getForecast(t *testing.T, path string) (int, ForecastWriteoffsResponse) {
	t.Helper()
	snap := hotSnap() // лосось 1400 г, 200 г/ч, 5 рабочих часов → риск 400 г × 900 ₽/кг
	rec := httptest.NewRecorder()
	hotRouter(snap, "a").Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var out ForecastWriteoffsResponse
	if rec.Code == 200 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code, out
}

func TestForecastWriteoffs24hSeesOnlyNearBatch(t *testing.T) {
	code, out := getForecast(t, "/api/forecast/writeoffs?hours=24")
	if code != 200 || out.Hours != 24 || out.TotalMinor != 36000 || len(out.Products) != 1 ||
		out.Products[0].ProductID != "salmon" || out.Products[0].AtRisk.Qty != 400 || len(out.Days) != 1 {
		t.Fatalf("%d %+v", code, out)
	}
	if !out.GeneratedAt.Equal(hotNow) {
		t.Fatalf("generated_at %v", out.GeneratedAt)
	}
}

func TestForecastWriteoffsDefaultIsWeek(t *testing.T) {
	code, out := getForecast(t, "/api/forecast/writeoffs")
	// Говядина (900 г, 57,6 г/ч, срок через 72 ч) успевает разойтись — в прогнозе только лосось.
	if code != 200 || out.Hours != 168 || out.TotalMinor != 36000 || len(out.Products) != 1 {
		t.Fatalf("%d %+v", code, out)
	}
}

func TestForecastWriteoffsRejectsBadHours(t *testing.T) {
	if code, _ := getForecast(t, "/api/forecast/writeoffs?hours=5"); code != http.StatusUnprocessableEntity && code != http.StatusBadRequest {
		t.Fatalf("code %d", code)
	}
}
