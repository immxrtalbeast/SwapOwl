package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	cm := "points:\n  - id: a\n    url: http://a\n    api_key: sk_abc   # comment: x\n"
	if c, err := ParseConfig([]byte(cm), env); err != nil || c.Points[0].APIKey != "sk_abc" {
		t.Fatalf("inline comment: %+v %v", c, err)
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

// abPoint — точка с управляемыми часами стенда и списаниями «1 ₽ за каждую минуту стенда после 12:00».
type abPoint struct {
	srv     *httptest.Server
	mu      sync.Mutex
	now     time.Time
	running bool
}

func (a *abPoint) set(now time.Time, running bool) {
	a.mu.Lock()
	a.now, a.running = now, running
	a.mu.Unlock()
}

func newABPoint(t *testing.T, perMinute int64) *abPoint {
	t.Helper()
	a := &abPoint{now: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/clock", func(w http.ResponseWriter, _ *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"now": a.now, "timezone": "UTC", "simulator_running": a.running})
	})
	mux.HandleFunc("/api/analytics/writeoffs", func(w http.ResponseWriter, r *http.Request) {
		base := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
		from, e1 := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
		to, e2 := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
		if e1 != nil || e2 != nil { // сутки: не интересуют
			_, _ = w.Write([]byte(`{"total_minor":0}`))
			return
		}
		if from.Before(base) {
			from = base
		}
		min := int64(0)
		if to.After(from) {
			min = int64(to.Sub(from) / time.Minute)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"total_minor": min * perMinute})
	})
	empty := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }
	}
	mux.HandleFunc("/api/alerts", empty(`[]`))
	mux.HandleFunc("/api/stop-list", empty(`[]`))
	mux.HandleFunc("/api/hot-offers", empty(`{"offers":[]}`))
	a.srv = httptest.NewServer(mux)
	t.Cleanup(a.srv.Close)
	return a
}

func TestParseConfigGroupAndLabel(t *testing.T) {
	cfg, err := ParseConfig([]byte(`points:
  - id: with
    url: http://a
    api_key: k
    label: "Со SwapOwl"
    group: AB
  - id: without
    url: http://b
    api_key: k
    label: Без подсказок
    group: ab
  - id: other
    url: http://c
    api_key: k
`), func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Points
	if p[0].Label != "Со SwapOwl" || p[0].Group != GroupAB || p[1].Label != "Без подсказок" || p[2].Group != "" {
		t.Errorf("points: %+v", p)
	}
	_, err = ParseConfig([]byte(`[{"id":"a","url":"http://a","api_key":"k","group":"ab"},{"id":"b","url":"http://b","api_key":"k","group":"ab"},{"id":"c","url":"http://c","api_key":"k","group":"ab"}]`), nil)
	if err == nil {
		t.Error("три точки в группе ab должны быть отвергнуты")
	}
}

func TestABSeriesGroupingAndReset(t *testing.T) {
	with, without := newABPoint(t, 100), newABPoint(t, 300)
	other := fakePoint(t, `[]`, 0)
	cfg := Config{Points: []Point{
		{ID: "with", Name: "A", Label: "Со SwapOwl", Group: GroupAB, URL: with.srv.URL, APIKey: goodKey},
		{ID: "without", Name: "B", Label: "Без подсказок", Group: GroupAB, URL: without.srv.URL, APIKey: goodKey},
		{ID: "other", Name: "C", URL: other.URL, APIKey: goodKey},
	}}
	col := NewCollector(cfg, 2*time.Second, nil)
	ctx := context.Background()
	col.PollAll(ctx)
	col.PollAB(ctx)
	for _, s := range col.Snapshot() {
		if s.Run != nil {
			t.Fatalf("%s: прогон без запущенного генератора: %+v", s.ID, s.Run)
		}
	}

	// Генератор запущен, прошло 2 ч 30 мин стенда.
	t0 := time.Date(2026, 10, 9, 12, 20, 0, 0, time.UTC) // прогон начнётся с 12:00 (час стенда)
	with.set(t0, true)
	without.set(t0, true)
	col.PollAB(ctx)
	with.set(t0.Add(2*time.Hour+10*time.Minute), true)
	without.set(t0.Add(2*time.Hour+10*time.Minute), true)
	col.PollAB(ctx)

	snap := col.Snapshot()
	if snap[0].Label != "Со SwapOwl" || snap[0].Group != GroupAB || snap[1].Label != "Без подсказок" || snap[2].Group != "" {
		t.Errorf("label/group: %+v %+v %+v", snap[0].Label, snap[1].Label, snap[2].Group)
	}
	if snap[2].Run != nil {
		t.Error("точка вне группы ab не получает ряд")
	}
	a, b := snap[0].Run, snap[1].Run
	if a == nil || b == nil || !a.Running {
		t.Fatalf("run: %+v %+v", a, b)
	}
	// 12:00 → 14:30 = 150 мин стенда.
	if a.TotalMinor != 150*100 || b.TotalMinor != 150*300 {
		t.Errorf("totals: %d %d", a.TotalMinor, b.TotalMinor)
	}
	want := []SeriesPoint{{0, 0}, {1, 6000}, {2, 12000}, {2.5, 15000}}
	if len(a.Series) != len(want) {
		t.Fatalf("series: %+v", a.Series)
	}
	for i, sp := range want {
		if a.Series[i] != sp {
			t.Errorf("series[%d] = %+v, want %+v", i, a.Series[i], sp)
		}
	}
	if b.Series[len(b.Series)-1].CostMinor != 45000 || a.Series[1].Hour != b.Series[1].Hour {
		t.Errorf("оси двух точек должны совпадать: %+v %+v", a.Series, b.Series)
	}

	// В сводке и в JSON ряд есть.
	rr := do(NewServer(col, "t"), "/api/network/summary", "t")
	if !strings.Contains(rr.Body.String(), `"series"`) || !strings.Contains(rr.Body.String(), `"label":"Со SwapOwl"`) {
		t.Errorf("summary: %s", rr.Body.String())
	}

	// Пауза: генератор выключен, ряд остаётся.
	with.set(with.now, false)
	col.PollAB(ctx)
	if r := col.Snapshot()[0].Run; r == nil || r.Running || r.TotalMinor != 15000 {
		t.Errorf("после паузы: %+v", r)
	}

	// Сброс: часы назад, генератор остановлен — прогона нет.
	with.set(time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC), false)
	col.PollAB(ctx)
	if r := col.Snapshot()[0].Run; r != nil {
		t.Errorf("после сброса прогон должен исчезнуть: %+v", r)
	}
}

func TestABNextDayRestartStartsNewRun(t *testing.T) {
	p := newABPoint(t, 100)
	col := NewCollector(Config{Points: []Point{{ID: "a", Name: "A", Group: GroupAB, URL: p.srv.URL, APIKey: goodKey}}}, 2*time.Second, nil)
	ctx := context.Background()
	col.PollAll(ctx)
	day1 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	p.set(day1, true)
	col.PollAB(ctx)
	p.set(day1.Add(3*time.Hour), true)
	col.PollAB(ctx)
	p.set(day1.Add(3*time.Hour), false) // ab-stop
	col.PollAB(ctx)
	if r := col.Snapshot()[0].Run; r == nil || r.Running {
		t.Fatalf("после паузы ряд должен остаться: %+v", r)
	}
	// ab-start на следующий день: reset ставит часы на T0 нового дня — вперёд, а не назад.
	day2 := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	p.set(day2.Add(10*time.Minute), true)
	col.PollAB(ctx)
	r := col.Snapshot()[0].Run
	if r == nil || !r.StartedAt.Equal(day2) || len(r.Series) != 2 {
		t.Fatalf("новый прогон должен начаться с %v: %+v", day2, r)
	}
}

// «Дворник» списывает просрочку после окна опоздания и датирует движение моментом expires_at — уже закрытым
// часом. Hub перечитывает последние закрытые часы, иначе такой час остался бы нулевым навсегда.
func TestABLateWriteoffInClosedHour(t *testing.T) {
	base := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	now, late := base, false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/clock", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"now": now, "timezone": "UTC", "simulator_running": true})
	})
	mux.HandleFunc("/api/analytics/writeoffs", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		from, _ := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
		to, _ := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
		at := base.Add(90 * time.Minute) // списание датировано 13:30
		total := 0
		if late && !at.Before(from) && at.Before(to) {
			total = 5000
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"total_minor": total})
	})
	for path, body := range map[string]string{"/api/alerts": `[]`, "/api/stop-list": `[]`, "/api/hot-offers": `{"offers":[]}`} {
		body := body
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	col := NewCollector(Config{Points: []Point{{ID: "a", Name: "A", Group: GroupAB, URL: srv.URL, APIKey: goodKey}}}, 2*time.Second, nil)
	ctx := context.Background()
	col.PollAll(ctx)
	set := func(t time.Time, l bool) { mu.Lock(); now, late = t, l; mu.Unlock() }

	col.PollAB(ctx) // 12:00 — прогон начат
	set(base.Add(2*time.Hour+10*time.Minute), false)
	col.PollAB(ctx) // 14:10 — часы 12–13 и 13–14 закрыты и прочитаны: 0
	set(base.Add(2*time.Hour+20*time.Minute), true)
	col.PollAB(ctx) // 14:20 — «дворник» записал списание задним числом в 13:30
	r := col.Snapshot()[0].Run
	if r == nil || r.TotalMinor != 5000 || r.Series[2].CostMinor != 5000 {
		t.Fatalf("позднее списание в закрытом часе потеряно: %+v", r)
	}
}
