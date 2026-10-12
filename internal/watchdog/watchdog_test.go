package watchdog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"umirhack/internal/clock"
)

type rec struct {
	mu   sync.Mutex
	msgs []string
}

func (r *rec) Send(_ context.Context, s string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, s)
	return nil
}
func (r *rec) n() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.msgs) }

type fakeAPI struct {
	mu      sync.Mutex
	healthy bool
	metrics string
}

func (f *fakeAPI) set(h bool, m string) { f.mu.Lock(); f.healthy, f.metrics = h, m; f.mu.Unlock() }
func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/healthz":
		if !f.healthy {
			http.Error(w, "down", 503)
			return
		}
		fmt.Fprint(w, `{"status":"ok"}`)
	case "/metrics":
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, f.metrics)
	}
}

func setup(t *testing.T, cfg Config) (*Watchdog, *fakeAPI, *rec, *clock.Fixed) {
	t.Helper()
	api := &fakeAPI{healthy: true}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	cfg.APIURL = srv.URL
	clk := clock.NewFixed(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := &rec{}
	return New(cfg, srv.Client(), clk, r), api, r, clk
}

func TestAPIDownTwiceThenRecover(t *testing.T) {
	w, api, r, _ := setup(t, Config{Repeat: time.Hour})
	ctx := context.Background()
	api.set(false, "")
	w.Check(ctx)
	if r.n() != 0 {
		t.Fatal("одна неудача — ещё не тревога")
	}
	w.Check(ctx)
	if r.n() != 1 || !strings.Contains(r.msgs[0], "API") {
		t.Fatalf("msgs=%v", r.msgs)
	}
	w.Check(ctx)
	if r.n() != 1 {
		t.Fatal("спам до истечения Repeat")
	}
	api.set(true, "")
	w.Check(ctx)
	if r.n() != 2 || !strings.Contains(r.msgs[1], "восстановлено") {
		t.Fatalf("msgs=%v", r.msgs)
	}
	w.Check(ctx)
	if r.n() != 2 {
		t.Fatal("лишнее сообщение после выхода")
	}
}

func TestRepeatAfterInterval(t *testing.T) {
	w, api, r, clk := setup(t, Config{Repeat: time.Hour})
	api.set(false, "")
	ctx := context.Background()
	w.Check(ctx)
	w.Check(ctx)
	clk.Advance(59 * time.Minute)
	w.Check(ctx)
	if r.n() != 1 {
		t.Fatalf("рано: %v", r.msgs)
	}
	clk.Advance(2 * time.Minute)
	w.Check(ctx)
	if r.n() != 2 {
		t.Fatalf("повтор не пришёл: %v", r.msgs)
	}
}

func TestSyncAndOutbox(t *testing.T) {
	w, api, r, _ := setup(t, Config{MetricsToken: "tok", SyncMax: 15 * time.Minute, OutboxMax: 10 * time.Minute})
	ctx := context.Background()
	api.set(true, "outbox_undelivered 3\noutbox_oldest_unacked_age_seconds 700\nadapter_last_sync_age_seconds 100\n")
	w.Check(ctx)
	if r.n() != 1 || !strings.Contains(r.msgs[0], "outbox") {
		t.Fatalf("msgs=%v", r.msgs)
	}
	api.set(true, "outbox_oldest_unacked_age_seconds 0\nadapter_last_sync_age_seconds 1000\n")
	w.Check(ctx)
	if r.n() != 3 {
		t.Fatalf("ждали выход outbox и вход sync: %v", r.msgs)
	}
}

func TestMissingMetricsSkipped(t *testing.T) {
	w, api, r, _ := setup(t, Config{MetricsToken: "tok", SyncMax: time.Minute, OutboxMax: time.Minute})
	api.set(true, "outbox_undelivered 5\n")
	w.Check(context.Background())
	if r.n() != 0 {
		t.Fatalf("msgs=%v", r.msgs)
	}
}

func TestDisk(t *testing.T) {
	w, api, r, _ := setup(t, Config{DataDir: "/data"})
	api.set(true, "")
	free := 0.05
	w.SetDisk(func(string) (float64, error) { return free, nil })
	w.Check(context.Background())
	free = 0.5
	w.Check(context.Background())
	if r.n() != 2 {
		t.Fatalf("msgs=%v", r.msgs)
	}
}

func TestAdapterHealth(t *testing.T) {
	ad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "x", 500) }))
	defer ad.Close()
	w, api, r, _ := setup(t, Config{AdapterURL: ad.URL})
	api.set(true, "")
	w.Check(context.Background())
	if r.n() != 1 || !strings.Contains(r.msgs[0], "адаптер") {
		t.Fatalf("msgs=%v", r.msgs)
	}
}

func TestTelegramFailureRetries(t *testing.T) {
	fail := true
	var got []string
	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if fail {
			http.Error(w, "x", 500)
			return
		}
		got = append(got, r.URL.Path+"|"+r.PostForm.Get("chat_id"))
	}))
	defer tg.Close()
	w, api, _, _ := setup(t, Config{APIFailures: 1})
	w.notify = Telegram{Base: tg.URL, Token: "T", Chat: "42", Client: tg.Client()}
	api.set(false, "")
	w.Check(context.Background())
	fail = false
	w.Check(context.Background())
	if len(got) != 1 || got[0] != "/botT/sendMessage|42" {
		t.Fatalf("got=%v", got)
	}
}
