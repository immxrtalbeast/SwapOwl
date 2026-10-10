package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

var standTime = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func newTest(db Pinger) (*Router, *bytes.Buffer) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	return NewRouter(Deps{Log: log, DB: db, Now: func() time.Time { return standTime }}), &buf
}

func do(h http.Handler, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(`{"password":"s3cret"}`))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type errResp struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func decodeErr(t *testing.T, rec *httptest.ResponseRecorder) errResp {
	t.Helper()
	var e errResp
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("body is not error JSON: %q: %v", rec.Body.String(), err)
	}
	if e.Error.Details == nil {
		t.Fatalf("details must be an object, got %q", rec.Body.String())
	}
	return e
}

func lastLog(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		t.Fatalf("log line is not JSON: %v", err)
	}
	return m
}

func TestHealthzOK(t *testing.T) {
	rt, _ := newTest(fakeDB{})
	rec := do(rt.Handler(), http.MethodGet, "/healthz", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("X-Stand-Time"); got != "2026-10-10T12:00:00Z" {
		t.Errorf("X-Stand-Time = %q", got)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id missing")
	}
}

func TestHealthzDBDown(t *testing.T) {
	rt, _ := newTest(fakeDB{err: errors.New("connection refused")})
	rec := do(rt.Handler(), http.MethodGet, "/healthz", nil)
	if rec.Code != 503 || decodeErr(t, rec).Error.Code != CodeOverloaded {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "refused") {
		t.Error("db error text leaked to client")
	}
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	rt, _ := newTest(fakeDB{})
	h := rt.Handler()

	rec := do(h, http.MethodGet, "/nope", nil)
	if rec.Code != 404 || decodeErr(t, rec).Error.Code != CodeNotFound {
		t.Fatalf("404: got %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("404 without X-Request-Id")
	}

	rec = do(h, http.MethodPost, "/healthz", nil)
	if rec.Code != 405 || decodeErr(t, rec).Error.Code != CodeMethodNotAllowed {
		t.Fatalf("405: got %d %s", rec.Code, rec.Body)
	}
	if a := rec.Header().Get("Allow"); a != "GET, HEAD" {
		t.Errorf("Allow = %q", a)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestRequestIDPropagation(t *testing.T) {
	rt, _ := newTest(fakeDB{})
	h := rt.Handler()

	rec := do(h, http.MethodGet, "/healthz", map[string]string{"X-Request-Id": "client-id_1.2"})
	if got := rec.Header().Get("X-Request-Id"); got != "client-id_1.2" {
		t.Errorf("valid client id not kept: %q", got)
	}
	rec = do(h, http.MethodGet, "/healthz", map[string]string{"X-Request-Id": "bad id\twith junk"})
	if got := rec.Header().Get("X-Request-Id"); got == "" || strings.ContainsAny(got, " \t") {
		t.Errorf("unsafe id not replaced: %q", got)
	}
}

func TestPanicRecovered(t *testing.T) {
	rt, buf := newTest(fakeDB{})
	rt.Handle(http.MethodGet, "/boom", func(http.ResponseWriter, *http.Request) { panic("secret internal detail") })
	rec := do(rt.Handler(), http.MethodGet, "/boom", nil)
	if rec.Code != 500 || decodeErr(t, rec).Error.Code != CodeInternal {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("panic detail leaked to client")
	}
	if !strings.Contains(buf.String(), "secret internal detail") {
		t.Error("panic not logged")
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("500 without X-Request-Id")
	}
}

func TestRequestLog(t *testing.T) {
	rt, buf := newTest(fakeDB{})
	rt.Handle(http.MethodGet, "/api/things/{id}", func(w http.ResponseWriter, r *http.Request) { WriteJSON(w, 200, map[string]string{}) })
	h := rt.Handler()

	do(h, http.MethodGet, "/api/things/42?token=abc", map[string]string{
		"X-Request-Id": "rid-1", "Authorization": "Bearer topsecret", "Cookie": "session=cookiesecret",
	})
	m := lastLog(t, buf)
	if m["request_id"] != "rid-1" || m["method"] != "GET" || m["path"] != "/api/things/{id}" || m["status"] != float64(200) {
		t.Errorf("log = %v", m)
	}
	if _, ok := m["duration"]; !ok {
		t.Error("no duration")
	}
	for _, leak := range []string{"topsecret", "cookiesecret", "s3cret", "abc", "things/42"} {
		if strings.Contains(buf.String(), leak) {
			t.Errorf("log leaks %q: %s", leak, buf.String())
		}
	}

	do(h, http.MethodGet, "/secret/path/xyz", nil)
	m = lastLog(t, buf)
	if m["path"] != "unmatched" || m["status"] != float64(404) {
		t.Errorf("404 log = %v", m)
	}
}
