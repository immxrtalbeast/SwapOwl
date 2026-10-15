package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"umirhack/internal/auth"
	"umirhack/internal/auth/authtest"
	"umirhack/internal/config"
)

// Чистый экземпляр для ресторана (спецификация, 8.1): конфиг принимается,
// вход обязателен, поставка с явным unit от operator проходит, /api/demo/* — 404.
func TestCleanInstance(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL": "postgres://x", "DEMO_MODE": "false", "CLOCK_MODE": "real",
		"SEED_ON_EMPTY": "false", "AUTH_REQUIRED": "true", "ADMIN_LOGIN": "root", "ADMIN_PASSWORD": "pw",
	}
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("clean config rejected: %v", err)
	}
	if cfg.DemoMode || cfg.SeedOnEmpty || !cfg.AuthRequired || cfg.ClockMode != "real" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	// Пустая таблица пользователей и заданный администратор — проверка старта проходит.
	if err := cfg.CheckAdminBootstrap(true); err != nil {
		t.Fatal(err)
	}

	svc := auth.New(authtest.New(), auth.Options{Required: cfg.AuthRequired, SessionTTL: time.Hour,
		Cost: bcrypt.MinCost, Now: func() time.Time { return standTime }})
	if _, err := svc.CreateUser(context.Background(), "op", "pw", auth.Role("operator")); err != nil {
		t.Fatal(err)
	}
	fs := &fakeStock{}
	log := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	h := NewRouter(Deps{Log: log, DB: fakeDB{}, Now: func() time.Time { return standTime }, Auth: svc,
		Stock: fs, Demo: nil, DemoToken: "tok"}).Handler()

	do := func(method, path, body, cookie, bearer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "session", Value: cookie})
		}
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	delivery := item(`{"product_id":"salmon","quantity":"4","unit":"kg","shelf_life_hours":72}`)
	if rec := do("POST", "/api/deliveries", delivery, "", ""); rec.Code != 401 {
		t.Fatalf("anonymous delivery: %d", rec.Code)
	}

	login := do("POST", "/api/auth/login", `{"login":"op","password":"pw"}`, "", "")
	if login.Code != 200 {
		t.Fatalf("login: %d %s", login.Code, login.Body)
	}
	var cookie string
	for _, c := range login.Result().Cookies() {
		if c.Name == "session" {
			cookie = c.Value
		}
	}
	if rec := do("POST", "/api/deliveries", delivery, cookie, ""); rec.Code != 201 {
		t.Fatalf("operator delivery: %d %s", rec.Code, rec.Body)
	}

	for _, p := range []string{"/api/demo/clock/speed", "/api/demo/clock/jump", "/api/demo/reset", "/api/demo/simulator"} {
		if rec := do("POST", p, `{}`, cookie, "tok"); rec.Code != 404 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}
