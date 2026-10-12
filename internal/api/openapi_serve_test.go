package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPIServed(t *testing.T) {
	h := NewRouter(Deps{}).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/openapi.yaml", nil))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/yaml") {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.HasPrefix(rec.Body.String(), "openapi: 3.") {
		t.Fatalf("body %.40s", rec.Body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/docs", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/api/openapi.yaml") ||
		!strings.Contains(rec.Body.String(), "cdn.jsdelivr.net/npm/redoc@2.1.5") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
