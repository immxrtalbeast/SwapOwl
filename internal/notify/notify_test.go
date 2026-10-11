package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type tg struct {
	mu   sync.Mutex
	path []string
	text []string
}

func (t *tg) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var v struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}
	_ = json.Unmarshal(b, &v)
	t.mu.Lock()
	t.path = append(t.path, r.URL.Path)
	t.text = append(t.text, v.ChatID+"|"+v.Text)
	t.mu.Unlock()
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (t *tg) texts() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.text...)
}

func stubAPI(t *testing.T, now string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer viewer-key" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/alerts":
			_, _ = w.Write([]byte(`[{"batch_id":17,"product_id":"salmon","product_name":"Лосось","remaining":{"qty":1400,"unit":"g"},"expires_at":"2026-10-09T18:00:00+03:00","status":"expiring","doomed":false},
			{"batch_id":18,"product_id":"milk","product_name":"Молоко","remaining":{"qty":2000,"unit":"ml"},"expires_at":"2026-10-09T10:00:00+03:00","status":"expired","doomed":true}]`))
		case "/api/hot-offers":
			_, _ = w.Write([]byte(`{"offers":[{"dish_id":"steak","dish_name":"Стейк из лосося","target_portions":7,"reasons":[{"product_id":"salmon","batch_id":17}]},{"dish_id":"x","dish_name":"Другое","reasons":[{"product_id":"y","batch_id":99}]}]}`))
		case "/api/analytics/writeoffs":
			_, _ = w.Write([]byte(`{"total_minor":123450,"by_product":[{"product_id":"salmon","quantity":{"qty":1000,"unit":"g"},"cost_minor":50000}]}`))
		case "/api/analytics/shortfalls":
			_, _ = w.Write([]byte(`{"total_minor":5000,"by_product":[]}`))
		case "/api/clock":
			_, _ = w.Write([]byte(`{"now":"` + now + `","timezone":"Europe/Moscow"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newNotifier(t *testing.T, now string) (*Notifier, *tg, *Telegram) {
	api := stubAPI(t, now)
	bot := &tg{}
	tsrv := httptest.NewServer(bot)
	t.Cleanup(tsrv.Close)
	tgc := &Telegram{Base: tsrv.URL, Token: "SECRET-TOKEN", ChatID: "42", HC: http.DefaultClient}
	return &Notifier{
		API:      &APIClient{Base: api.URL, Key: "viewer-key", HC: http.DefaultClient},
		Out:      tgc,
		DigestAt: "22:00",
		Logf:     t.Logf,
	}, bot, tgc
}

func TestBatchExpiringMessage(t *testing.T) {
	n, bot, _ := newNotifier(t, "2026-10-09T12:00:00+03:00")
	ev := Event{Type: "batch.expiring", Data: `{"batch_id":17}`}
	n.Handle(context.Background(), ev)
	n.Handle(context.Background(), ev) // повтор — без второго сообщения
	got := bot.texts()
	if len(got) != 1 {
		t.Fatalf("сообщений: %d, нужно 1", len(got))
	}
	m := got[0]
	for _, want := range []string{"42|", "Лосось", "№17", "1,4 кг", "09.10 18:00", "700,00 ₽", "Стейк из лосося (7 порц.)"} {
		if !strings.Contains(m, want) {
			t.Errorf("нет %q в %q", want, m)
		}
	}
	if strings.Contains(m, "Другое") {
		t.Error("блюдо чужой партии попало в сообщение")
	}
	if bot.path[0] != "/botSECRET-TOKEN/sendMessage" {
		t.Errorf("путь: %s", bot.path[0])
	}
}

func TestOtherEventsIgnored(t *testing.T) {
	n, bot, _ := newNotifier(t, "2026-10-09T12:00:00+03:00")
	n.Handle(context.Background(), Event{Type: "stock.changed", Data: `{}`})
	n.Handle(context.Background(), Event{Type: "batch.expiring", Data: `{"batch_id":777}`}) // нет в ленте
	if len(bot.texts()) != 0 {
		t.Fatalf("лишние сообщения: %v", bot.texts())
	}
}

func TestDigestOncePerDayAtConfiguredTime(t *testing.T) {
	n, bot, _ := newNotifier(t, "2026-10-09T21:59:00+03:00")
	n.Tick(context.Background())
	if len(bot.texts()) != 0 {
		t.Fatal("сводка раньше времени")
	}
	n2, bot2, _ := newNotifier(t, "2026-10-09T22:01:00+03:00")
	n2.Tick(context.Background())
	n2.Tick(context.Background())
	got := bot2.texts()
	if len(got) != 1 {
		t.Fatalf("сводок: %d", len(got))
	}
	for _, want := range []string{"1 234,50 ₽", "50,00 ₽", "Под риском: 700,00 ₽ (партий: 1"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("нет %q в %q", want, got[0])
		}
	}
}

func TestDigestSkippedWhenStartedAfterTime(t *testing.T) {
	n, bot, _ := newNotifier(t, "2026-10-09T23:00:00+03:00")
	n.MarkDigestStartup(context.Background())
	n.Tick(context.Background())
	if len(bot.texts()) != 0 {
		t.Fatal("сводка при старте после времени")
	}
}

func TestTelegramErrorsDoNotLeakToken(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad "+r.URL.Path, http.StatusBadRequest)
	}))
	defer bad.Close()
	c := &Telegram{Base: bad.URL, Token: "SECRET-TOKEN", ChatID: "1", HC: http.DefaultClient}
	err := c.Send(context.Background(), "x")
	if err == nil || strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Fatalf("ошибка: %v", err)
	}
	bad.Close()
	err = c.Send(context.Background(), "x")
	if err == nil || strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Fatalf("ошибка транспорта: %v", err)
	}
}

func TestSubscribeReadsEventsAndReconnects(t *testing.T) {
	var mu sync.Mutex
	conns := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer viewer-key" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		conns++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: ping\ndata: {}\n\nevent: batch.expiring\nid: 1\ndata: {\"batch_id\":17}\n\n")
	}))
	defer srv.Close()
	c := &APIClient{Base: srv.URL, Key: "viewer-key", HC: http.DefaultClient}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var got []string
	done := make(chan struct{})
	go func() {
		c.Subscribe(ctx, 10*time.Millisecond, nil, func(e Event) {
			mu.Lock()
			got = append(got, e.Type+e.Data)
			if len(got) >= 4 {
				cancel()
			}
			mu.Unlock()
		}, t.Logf)
		close(done)
	}()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if conns < 2 || len(got) < 4 || got[1] != `batch.expiring{"batch_id":17}` {
		t.Fatalf("conns=%d got=%v", conns, got)
	}
}

func TestRub(t *testing.T) {
	for in, want := range map[int64]string{0: "0,00 ₽", 5: "0,05 ₽", 123450: "1 234,50 ₽", 100000000: "1 000 000,00 ₽", -250: "-2,50 ₽"} {
		if got := Rub(in); got != want {
			t.Errorf("Rub(%d)=%q want %q", in, got, want)
		}
	}
}
