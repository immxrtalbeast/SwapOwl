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

func TestRescueRateLimitPerClientBehindProxy(t *testing.T) {
	rt := rescueRouter(rescueSnap(), true, nil)
	get := func(remote, xff string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/public/rescue", nil)
		req.RemoteAddr = remote
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		rt.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	// Один гость за прокси исчерпал лимит — другой гость за тем же прокси не затронут.
	for i := 0; i < rescuePerMinute; i++ {
		get("172.18.0.2:4000", "203.0.113.7")
	}
	if c := get("172.18.0.2:4000", "203.0.113.7"); c != http.StatusTooManyRequests {
		t.Fatalf("первый гость: %d", c)
	}
	if c := get("172.18.0.2:4000", "203.0.113.8"); c != http.StatusOK {
		t.Fatalf("второй гость: %d", c)
	}
	// С публичного адреса подставленный заголовок игнорируется.
	for i := 0; i < rescuePerMinute; i++ {
		get("198.51.100.9:1", "10.0.0.1")
	}
	if c := get("198.51.100.9:1", "10.0.0.2"); c != http.StatusTooManyRequests {
		t.Fatalf("подмена XFF с публичного адреса: %d", c)
	}
}
