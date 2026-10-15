package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/hotoffers"
	"umirhack/internal/store"
)

type rescueOverrides struct{ list []store.HotOverride }

func (f rescueOverrides) ListHotOverrides(context.Context) ([]store.HotOverride, error) {
	return f.list, nil
}
func (rescueOverrides) SetHotOverride(context.Context, store.HotOverride) (store.HotOverride, error) {
	return store.HotOverride{}, nil
}
func (rescueOverrides) DeleteHotOverride(context.Context, string) error { return nil }

func rescueRouter(snap store.Snapshot, on bool, ov []store.HotOverride) *Router {
	return NewRouter(Deps{HotOffers: fakeSnap{snap}, Now: func() time.Time { return hotNow }, PublicRescue: on,
		HotOverrides: rescueOverrides{ov},
		HotConfig:    HotOffersConfig{DefaultAlgo: "b", Rank: hotoffers.Config{TopN: 3, WMin: 0.5, CoverHours: 12, MaxIter: 200}}})
}

func rescueSnap() store.Snapshot {
	s := hotSnap()
	s.Stopped = map[string]bool{}
	s.Lambda["steak"] = 1 // обычный спрос мал → цель выше 2× обычного → скидка
	return s
}

func getRescue(rt *Router) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/public/rescue", nil)
	req.RemoteAddr = "1.2.3.4:5555"
	rt.Handler().ServeHTTP(rec, req)
	return rec
}

func TestRescueDisabled404(t *testing.T) {
	if rec := getRescue(rescueRouter(rescueSnap(), false, nil)); rec.Code != http.StatusNotFound {
		t.Fatalf("code %d", rec.Code)
	}
}

func TestRescueListsDiscountDish(t *testing.T) {
	rec := getRescue(rescueRouter(rescueSnap(), true, nil))
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=15" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	var out []RescueJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out) != 1 || out[0].DishID != "steak" {
		t.Fatalf("%v %s", err, rec.Body)
	}
	if d := out[0].DiscountPercent; d < 5 || d > 50 || out[0].ServingsLeft <= 0 {
		t.Fatalf("%+v", out[0])
	}
	body := rec.Body.String()
	for _, f := range []string{"batch", "cost", "price_minor", "reasons", "at_risk"} {
		if strings.Contains(body, f) {
			t.Fatalf("утечка %q: %s", f, body)
		}
	}
}

func TestRescueNoStoppedNoHidden(t *testing.T) {
	s := rescueSnap()
	s.Stopped = map[string]bool{"steak": true}
	if rec := getRescue(rescueRouter(s, true, nil)); strings.Contains(rec.Body.String(), "steak") {
		t.Fatalf("стоп-лист: %s", rec.Body)
	}
	hide := []store.HotOverride{{DishID: "steak", Mode: store.HotHide}}
	if rec := getRescue(rescueRouter(rescueSnap(), true, hide)); strings.Contains(rec.Body.String(), "steak") {
		t.Fatalf("скрытое: %s", rec.Body)
	}
}

func TestRescueRateLimitAndReadOnly(t *testing.T) {
	snap := rescueSnap()
	before := snap.Batches[0].Remaining
	rt := rescueRouter(snap, true, nil)
	var last int
	for i := 0; i < rescuePerMinute+1; i++ {
		last = getRescue(rt).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("last %d", last)
	}
	if snap.Batches[0].Remaining != before {
		t.Fatal("склад изменился")
	}
	_ = domain.Qty(0)
}

func TestRescueClientIPBehindProxy(t *testing.T) {
	get := func(remote, xff string) string {
		r := httptest.NewRequest(http.MethodGet, "/api/public/rescue", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return clientIP(r)
	}
	if got := get("172.18.0.2:1", "9.9.9.9"); got != "9.9.9.9" {
		t.Fatalf("за прокси: %s", got)
	}
	if got := get("127.0.0.1:1", "6.6.6.6, 9.9.9.9"); got != "9.9.9.9" {
		t.Fatalf("крайнее правое: %s", got)
	}
	if got := get("8.8.8.8:1", "9.9.9.9"); got != "8.8.8.8" {
		t.Fatalf("публичный адрес, XFF игнорируется: %s", got)
	}
	// Разные клиенты за одним прокси расходуют разные лимиты.
	rt := rescueRouter(rescueSnap(), true, nil)
	do := func(xff string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/public/rescue", nil)
		req.RemoteAddr = "172.18.0.2:1"
		req.Header.Set("X-Forwarded-For", xff)
		rt.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < rescuePerMinute; i++ {
		do("1.1.1.1")
	}
	if do("1.1.1.1") != http.StatusTooManyRequests || do("2.2.2.2") != 200 {
		t.Fatal("лимит должен быть по клиенту, не по прокси")
	}
}
