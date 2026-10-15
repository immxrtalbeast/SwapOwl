package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"umirhack/internal/auth"
	"umirhack/internal/auth/authtest"
	"umirhack/internal/domain"
	"umirhack/internal/hotoffers"
	"umirhack/internal/store"
)

// accessEnv — настоящие маршруты FS-159/FS-160/отмены/Telegram за включённой аутентификацией, с заглушками хранилища.
type accessEnv struct {
	h       http.Handler
	cookies map[string]string // роль → значение cookie; "" — без входа
}

func newAccessEnv(t *testing.T) *accessEnv {
	t.Helper()
	repo := authtest.New()
	svc := auth.New(repo, auth.Options{Required: true, SessionTTL: time.Hour, SecureCookie: true,
		Cost: bcrypt.MinCost, Now: func() time.Time { return standTime }})
	snap := hotSnap()
	snap.Dishes = append(snap.Dishes, domain.Dish{ID: "soup", Name: "Суп дня", Recipe: []domain.RecipeItem{{ProductID: "beef", Quantity: 10}}})
	rt := NewRouter(Deps{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), DB: fakeDB{}, Now: func() time.Time { return hotNow },
		Auth: svc, Telegram: &memLinks{byID: map[int64]string{}, users: map[string]bool{}}, TelegramBotToken: tgToken,
		HotOffers: fakeSnap{snap}, HotOverrides: &fakeOverrides{}, DishHolds: &fakeHolds{},
		HotConfig:   HotOffersConfig{DefaultAlgo: "b", Rank: hotoffers.Config{TopN: 3, WMin: 0.5, CoverHours: 12, MaxIter: 200}},
		Corrections: &fakeCorrections{}, Stock: &fakeStock{}, Supply: fakeSupply{},
	})
	e := &accessEnv{h: rt.Handler(), cookies: map[string]string{"": ""}}
	for _, role := range []auth.Role{auth.Viewer, auth.Operator, auth.Admin} {
		login := "u-" + string(role)
		if _, err := svc.CreateUser(context.Background(), login, "password1", role); err != nil {
			t.Fatal(err)
		}
		rec := e.do("", "POST", "/api/auth/login", `{"login":"`+login+`","password":"password1"}`)
		if rec.Code != 200 {
			t.Fatalf("login %s: %d %s", login, rec.Code, rec.Body)
		}
		for _, c := range rec.Result().Cookies() {
			if c.Name == sessionCookie {
				e.cookies[string(role)] = c.Value
			}
		}
	}
	return e
}

func (e *accessEnv) do(cookie, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	return rec
}

// Роли × маршруты: без входа — 401, viewer на изменяющих — 403 (код forbidden), operator и admin проходят.
// Тело заведомо корректное, поэтому «проход» — 2xx, а не 4xx валидации.
func TestRouteAccessByRole(t *testing.T) {
	e := newAccessEnv(t)
	delivery := `{"id":"D1","number":"1","created_at":"2026-10-09T08:00:00+03:00","items":[{"product_id":"salmon","quantity":"1","unit":"kg","shelf_life_hours":72}]}`
	routes := []struct {
		name, method, path, body string
		// ok — коды успеха для operator/admin; viewer получает okViewer (0 — 403).
		ok       int
		okViewer int
	}{
		{"PUT override", "PUT", "/api/hot-offers/overrides/soup", `{"mode":"pin"}`, 200, 0},
		{"DELETE override", "DELETE", "/api/hot-offers/overrides/soup", ``, 204, 0},
		{"GET overrides", "GET", "/api/hot-offers/overrides", ``, 200, 200},
		{"PUT hold", "PUT", "/api/dish-holds/soup", `{"note":"плита"}`, 200, 0},
		{"DELETE hold", "DELETE", "/api/dish-holds/soup", ``, 204, 0},
		{"GET holds", "GET", "/api/dish-holds", ``, 200, 200},
		{"POST cancel", "POST", "/api/orders/" + oid + "/cancel", `{"id":"c1"}`, 201, 0},
		{"POST delivery", "POST", "/api/deliveries", delivery, 201, 0},
		{"GET supply", "GET", "/api/supply-suggestions", ``, 200, 200},
	}
	for _, rc := range routes {
		for _, role := range []string{"", "viewer", "operator", "admin"} {
			want := rc.ok
			switch {
			case role == "":
				want = 401
			case role == "viewer":
				want = rc.okViewer
				if want == 0 {
					want = 403
				}
			}
			rec := e.do(e.cookies[role], rc.method, rc.path, rc.body)
			if rec.Code != want {
				t.Errorf("%s как %q: %d, ожидали %d (%s)", rc.name, role, rec.Code, want, rec.Body)
				continue
			}
			switch want {
			case 401:
				if c := decodeErr(t, rec).Error.Code; c != CodeUnauthorized {
					t.Errorf("%s без входа: код %q", rc.name, c)
				}
			case 403:
				if c := decodeErr(t, rec).Error.Code; c != CodeForbidden {
					t.Errorf("%s viewer: код %q", rc.name, c)
				}
			}
		}
	}
}

// Отказ по правам не доходит до хранилища: viewer не оставляет решения и не создаёт поставку.
func TestForbiddenRequestsDoNotReachStore(t *testing.T) {
	repo := authtest.New()
	svc := auth.New(repo, auth.Options{Required: true, SessionTTL: time.Hour, Cost: bcrypt.MinCost, Now: func() time.Time { return standTime }})
	if _, err := svc.CreateUser(context.Background(), "v", "password1", auth.Viewer); err != nil {
		t.Fatal(err)
	}
	ov, fs, fc := &fakeOverrides{}, &fakeStock{}, &fakeCorrections{}
	rt := NewRouter(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), DB: fakeDB{}, Now: func() time.Time { return hotNow }, Auth: svc,
		HotOffers: fakeSnap{hotSnap()}, HotOverrides: ov, Stock: fs, Corrections: fc})
	h := rt.Handler()
	r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"login":"v","password":"password1"}`))
	r.Header.Set("Content-Type", "application/json")
	lr := httptest.NewRecorder()
	h.ServeHTTP(lr, r)
	var cookie *http.Cookie
	for _, c := range lr.Result().Cookies() {
		cookie = c
	}
	for _, c := range []struct{ method, path, body string }{
		{"PUT", "/api/hot-offers/overrides/soup", `{"mode":"hide"}`},
		{"POST", "/api/orders/" + oid + "/cancel", `{"id":"c1"}`},
		{"POST", "/api/deliveries", `{"id":"D1"}`},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Errorf("%s %s: %d", c.method, c.path, rec.Code)
		}
	}
	if ov.set.DishID != "" || fs.in.ID != "" || fc.can.ID != "" {
		t.Fatalf("запрос viewer дошёл до хранилища: %+v %+v %+v", ov.set, fs.in, fc.can)
	}
	_ = store.HotPin
}
