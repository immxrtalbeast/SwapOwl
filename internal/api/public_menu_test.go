package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func menuRouter(on bool) *Router {
	return NewRouter(Deps{Menu: fakeMenu{testSnap()}, PublicRescue: on, Now: func() time.Time { return time.Unix(0, 0) }})
}

func getMenu(rt *Router, remote string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/public/menu", nil)
	req.RemoteAddr = remote
	rt.Handler().ServeHTTP(rec, req)
	return rec
}

func TestPublicMenuDisabled404(t *testing.T) {
	if rec := getMenu(menuRouter(false), "1.2.3.4:1"); rec.Code != http.StatusNotFound {
		t.Fatalf("code %d", rec.Code)
	}
}

// Гость видит только название и статус: ни рецептов, ни причин стопа, ни остатков.
func TestPublicMenuShowsOnlyStatus(t *testing.T) {
	rec := getMenu(menuRouter(true), "1.2.3.4:1")
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=15" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	var rows []PublicMenuJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil || len(rows) != 2 {
		t.Fatalf("%v %s", err, rec.Body)
	}
	status := map[string]string{}
	for _, r := range rows {
		status[r.DishID] = r.Status
	}
	// testSnap: овощей 350 → суп на 3 порции («мало»), для стейка нет говядины («стоп»).
	if status["soup"] != menuFew || status["steak"] != menuStopped {
		t.Fatalf("статусы: %v", status)
	}
	for _, leak := range []string{"Говядина", "missing", "recipe", "servings", "required", "available\":"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Fatalf("утечка %q: %s", leak, rec.Body)
		}
	}
}

func TestPublicMenuStatusThresholds(t *testing.T) {
	for _, c := range []struct {
		avail bool
		n     int64
		want  string
	}{{true, 10, menuAvailable}, {true, 4, menuAvailable}, {true, 3, menuFew}, {true, 1, menuFew}, {true, 0, menuStopped}, {false, 5, menuStopped}} {
		if got := publicMenuStatus(c.avail, c.n); got != c.want {
			t.Errorf("%v/%d: %s, ждали %s", c.avail, c.n, got, c.want)
		}
	}
}

func TestPublicMenuRateLimitPerClient(t *testing.T) {
	rt := menuRouter(true)
	for i := 0; i < rescuePerMinute; i++ {
		getMenu(rt, "172.18.0.2:1")
	}
	if rec := getMenu(rt, "172.18.0.2:1"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("code %d", rec.Code)
	}
}
