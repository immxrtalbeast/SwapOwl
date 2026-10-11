package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseBench(t *testing.T) {
	ok := parseBench([]byte(`{"available":true,"runs":200,"main_algo":"b","waste_reduction_vs_random_pct":10.1,"waste_reduction_vs_nearest_pct":13.3}`))
	if !ok.Available || ok.Runs != 200 || ok.MainAlgo != "b" || ok.WasteReductionVsRandomPct != 10.1 || ok.WasteReductionVsNearestPct != 13.3 {
		t.Fatalf("%+v", ok)
	}
	for _, raw := range []string{``, `{`, `{"available":false}`, `{"available":true}`} {
		if got := parseBench([]byte(raw)); got.Available || got.Runs != 0 {
			t.Fatalf("%q: %+v", raw, got)
		}
	}
}

func TestBenchEndpoint(t *testing.T) {
	rt := NewRouter(Deps{})
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/analytics/bench", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"available":true`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
