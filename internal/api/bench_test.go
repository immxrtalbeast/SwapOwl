package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"umirhack/internal/store"
)

func TestParseBench(t *testing.T) {
	ok := parseBench([]byte(`{"available":true,"runs":200,"main_algo":"b","waste_reduction_vs_random_pct":39.0,"waste_reduction_vs_nearest_pct":27.0}`))
	if !ok.Available || ok.Runs != 200 || ok.MainAlgo != "b" || ok.WasteReductionVsRandomPct != 39 || ok.WasteReductionVsNearestPct != 27 {
		t.Fatalf("%+v", ok)
	}
	for _, raw := range []string{``, `not json`, `{"available":false,"runs":200,"main_algo":"b"}`, `{"available":true}`} {
		if got := parseBench([]byte(raw)); got != (BenchResult{}) {
			t.Fatalf("%q: %+v", raw, got)
		}
	}
	b, _ := json.Marshal(BenchResult{})
	if string(b) != `{"available":false}` {
		t.Fatalf("unavailable body: %s", b)
	}
}

func TestBenchAndShortagesEndpoints(t *testing.T) {
	now := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	rt := NewRouter(Deps{Analytics: &fakeAnalytics{}, Now: func() time.Time { return now }})
	get := func(url string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		return rec
	}
	rec := get("/api/analytics/bench")
	var b BenchResult
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &b) != nil || (b.Available && b.Runs != 200) {
		t.Fatalf("bench: %d %s", rec.Code, rec.Body)
	}
	rec = get("/api/analytics/shortages")
	var s store.ShortagesReport
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &s) != nil || s.RejectedOrders != 3 {
		t.Fatalf("shortages: %d %s", rec.Code, rec.Body)
	}
	if rec = get("/api/analytics/shortages?from=bad"); rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad from: %d", rec.Code)
	}
}
