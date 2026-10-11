package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"umirhack/internal/metrics"
)

func getMetrics(h http.Handler, path, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMetricsToken(t *testing.T) {
	reg := metrics.New()
	h := NewRouter(Deps{Metrics: reg, MetricsToken: "s3cret"}).Handler()
	for _, auth := range []string{"", "Bearer wrong", "s3cret"} {
		if rec := getMetrics(h, "/metrics", auth); rec.Code != http.StatusNotFound {
			t.Errorf("auth %q: %d, want 404", auth, rec.Code)
		}
	}
	// Без токена в конфиге открыть нельзя даже пустым заголовком.
	h2 := NewRouter(Deps{Metrics: reg}).Handler()
	if rec := getMetrics(h2, "/metrics", "Bearer "); rec.Code != http.StatusNotFound {
		t.Errorf("empty token: %d, want 404", rec.Code)
	}
	if rec := getMetrics(h, "/metrics", "Bearer s3cret"); rec.Code != http.StatusOK {
		t.Fatalf("valid token: %d", rec.Code)
	}
}

func TestMetricsCountsRoutePattern(t *testing.T) {
	reg := metrics.New()
	rt := NewRouter(Deps{Metrics: reg, MetricsToken: "t"})
	rt.Handle(http.MethodGet, "/api/thing/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	h := rt.Handler()
	getMetrics(h, "/api/thing/42", "")
	getMetrics(h, "/api/thing/43", "")
	getMetrics(h, "/nope/123", "")
	body := getMetrics(h, "/metrics", "Bearer t").Body.String()
	for _, want := range []string{
		`http_requests_total{method="GET",route="/api/thing/{id}",status="202"} 2`,
		`http_requests_total{method="GET",route="unmatched",status="404"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("нет %q в:\n%s", want, body)
		}
	}
	if strings.Contains(body, "/api/thing/42") || strings.Contains(body, "/nope/123") {
		t.Errorf("сырой URL в метриках:\n%s", body)
	}
}
