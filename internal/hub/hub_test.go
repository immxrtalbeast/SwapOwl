package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const goodKey = "key-secret"

func fakePoint(t *testing.T, stop string, expiring int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	j := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+goodKey {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(body))
		}
	}
	mux.HandleFunc("/api/clock", j(`{"now":"2026-10-09T12:00:00+03:00","timezone":"Europe/Moscow"}`))
	al := `[`
	for i := 0; i < expiring; i++ {
		if i > 0 {
			al += ","
		}
		al += `{"batch_id":1,"product_id":"salmon","status":"expiring","remaining":{"qty":100,"unit":"g"}}`
	}
	mux.HandleFunc("/api/alerts", j(al+`]`))
	mux.HandleFunc("/api/stop-list", j(stop))
	mux.HandleFunc("/api/hot-offers", j(`{"offers":[{"dish_id":"d1","dish_name":"Стейк"}]}`))
	mux.HandleFunc("/api/analytics/writeoffs", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+goodKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("from") == r.URL.Query().Get("to") {
			_, _ = w.Write([]byte(`{"total_minor":1500}`))
			return
		}
		_, _ = w.Write([]byte(`{"total_minor":9900}`))
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func setup(t *testing.T) (*Collector, http.Handler) {
	t.Helper()
	a := fakePoint(t, `[{"id":"x","name":"Паста"},{"id":"y","name":"Рис"}]`, 2)
	b := fakePoint(t, `[]`, 0)
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close() // порт закрыт: соединение отказано
	cfg := Config{Points: []Point{
		{ID: "a", Name: "A", URL: a.URL, APIKey: goodKey},
		{ID: "b", Name: "B", URL: b.URL, APIKey: "wrong"},
		{ID: "c", Name: "C", URL: deadURL, APIKey: goodKey},
	}}
	col := NewCollector(cfg, 2*time.Second, nil)
	col.PollAll(context.Background())
	return col, NewServer(col, "hub-token")
}

func do(h http.Handler, path, tok string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestSummary(t *testing.T) {
	_, h := setup(t)
	rr := do(h, "/api/network/summary", "hub-token")
	if rr.Code != 200 {
		t.Fatalf("code %d", rr.Code)
	}
	var got struct {
		Points []PointState `json:"points"`
		Totals Totals       `json:"totals"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	a, b, c := got.Points[0], got.Points[1], got.Points[2]
	if a.Status != StatusOK || a.ExpiringBatches != 2 || a.StopListCount != 2 ||
		a.StopListNames[0] != "Паста" || a.WriteoffsTodayMinor != 1500 || a.Writeoffs7dMinor != 9900 ||
		a.TopHotOffer == nil || a.TopHotOffer.DishID != "d1" || a.LastSuccessAt == nil || a.StandTime == nil {
		t.Errorf("point a: %+v", a)
	}
	if a.Alerts != nil {
		t.Error("summary must not carry feeds")
	}
	if b.Status != StatusUnauthorized || b.LastSuccessAt != nil {
		t.Errorf("point b: %+v", b)
	}
	if c.Status != StatusUnreachable {
		t.Errorf("point c: %+v", c)
	}
	if got.Totals.Points != 3 || got.Totals.OK != 1 || got.Totals.Unavailable != 2 ||
		got.Totals.ExpiringBatches != 2 || got.Totals.Writeoffs7dMinor != 9900 {
		t.Errorf("totals: %+v", got.Totals)
	}
	if strings.Contains(rr.Body.String(), goodKey) || strings.Contains(rr.Body.String(), "127.0.0.1") {
		t.Error("response leaks key or address")
	}
}

func TestPointDetailAndLastSuccess(t *testing.T) {
	col, h := setup(t)
	var a PointState
	_ = json.Unmarshal(do(h, "/api/network/points/a", "hub-token").Body.Bytes(), &a)
	if len(a.Alerts) != 2 || len(a.StopList) != 2 {
		t.Errorf("feeds: %+v", a)
	}
	if do(h, "/api/network/points/zzz", "hub-token").Code != 404 {
		t.Error("want 404")
	}
	// Точка падает после успешного опроса: статус unreachable, время успеха сохраняется.
	col.mu.Lock()
	col.points[0].URL = "http://127.0.0.1:1"
	col.mu.Unlock()
	col.PollAll(context.Background())
	st, _ := col.Point("a")
	if st.Status != StatusUnreachable || st.LastSuccessAt == nil || st.StopListCount != 0 {
		t.Errorf("after failure: %+v", st)
	}
}

func TestAuthAndHealth(t *testing.T) {
	_, h := setup(t)
	if do(h, "/healthz", "").Code != 200 {
		t.Error("healthz must be open")
	}
	for _, tok := range []string{"", "bad"} {
		if c := do(h, "/api/network/summary", tok).Code; c != 401 {
			t.Errorf("token %q: %d", tok, c)
		}
	}
}

func TestTimeoutDoesNotBlock(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer slow.Close()
	col := NewCollector(Config{Points: []Point{{ID: "s", Name: "S", URL: slow.URL, APIKey: "k"}}}, 100*time.Millisecond, nil)
	start := time.Now()
	col.PollAll(context.Background())
	if time.Since(start) > 2*time.Second {
		t.Error("poll not bounded by timeout")
	}
	if st, _ := col.Point("s"); st.Status != StatusUnreachable || st.Error != "timeout" {
		t.Errorf("%+v", st)
	}
}

func TestParseConfig(t *testing.T) {
	env := func(k string) string { return map[string]string{"K2": "v2"}[k] }
	y := "points:\n  - id: a\n    name: \"Точка A\"\n    url: http://a:8080/\n    api_key: k1\n  - id: b\n    url: https://b\n    api_key_env: K2\n"
	c, err := ParseConfig([]byte(y), env)
	if err != nil || len(c.Points) != 2 || c.Points[0].URL != "http://a:8080" || c.Points[1].APIKey != "v2" || c.Points[0].Name != "Точка A" {
		t.Fatalf("yaml: %+v %v", c, err)
	}
	j := `{"points":[{"id":"a","url":"http://a","api_key":"k"}]}`
	if _, err := ParseConfig([]byte(j), env); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`[{"id":"a","url":"http://a"}]`,
		`[{"id":"a","url":"ftp://a","api_key":"k"}]`,
		`[{"id":"a","url":"http://a","api_key":"k"},{"id":"a","url":"http://a","api_key":"k"}]`,
		`[]`,
	} {
		if _, err := ParseConfig([]byte(bad), env); err == nil {
			t.Errorf("want error: %s", bad)
		}
	}
	if d, _ := PollInterval(""); d != time.Minute {
		t.Error("default poll")
	}
	if d, err := PollInterval("30"); err != nil || d != 30*time.Second {
		t.Error("seconds poll")
	}
}
