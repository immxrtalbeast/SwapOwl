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

type botEnv struct {
	bot   *Bot
	mu    sync.Mutex
	sent  []string // sendMessage: текст (+ "[кнопки]")
	ans   []string
	calls []string // изменяющие вызовы API
	role  string
	open  bool // партия 5 ещё не подтверждена
}

func newBotEnv(t *testing.T, role string, withAct bool) *botEnv {
	e := &botEnv{role: role, open: true}
	tgs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var v struct {
			Text string `json:"text"`
			Rm   *struct {
				K [][]struct {
					D string `json:"callback_data"`
				} `json:"inline_keyboard"`
			} `json:"reply_markup"`
		}
		_ = json.Unmarshal(b, &v)
		e.mu.Lock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			s := v.Text
			if v.Rm != nil {
				for _, b := range v.Rm.K[0] {
					s += "[" + b.D + "]"
				}
			}
			e.sent = append(e.sent, s)
		case strings.HasSuffix(r.URL.Path, "/answerCallbackQuery"):
			e.ans = append(e.ans, v.Text)
		}
		e.mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(tgs.Close)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		switch r.URL.Path {
		case "/api/auth/me":
			_, _ = w.Write([]byte(`{"role":"` + e.role + `"}`))
		case "/api/receiving":
			if e.open {
				_, _ = w.Write([]byte(`[{"id":5,"product_name":"Лосось","delivery_number":"D-1","remaining":{"qty":1500,"unit":"g"},"received_at":"2026-10-09T10:00:00+03:00","expires_at":"2026-10-11T10:00:00+03:00"}]`))
			} else {
				_, _ = w.Write([]byte(`[]`))
			}
		case "/api/batches/5/expiry":
			b, _ := io.ReadAll(r.Body)
			e.calls = append(e.calls, r.Method+" "+string(b))
			e.open = false
			_, _ = w.Write([]byte(`{}`))
		case "/api/hot-offers":
			_, _ = w.Write([]byte(`{"offers":[{"dish_name":"A","target_portions":7,"reasons":[{"product_name":"Лосось","batch_id":17}]},{"dish_name":"B","target_portions":2},{"dish_name":"C"},{"dish_name":"D"}]}`))
		case "/api/stop-list":
			_, _ = w.Write([]byte(`[{"name":"Суши","missing":[{"product_name":"Рис","missing":{"qty":500,"unit":"g"}}]}]`))
		case "/api/alerts":
			_, _ = w.Write([]byte(`[{"batch_id":17,"product_id":"salmon","product_name":"Лосось","remaining":{"qty":1400,"unit":"g"},"expires_at":"2026-10-09T18:00:00+03:00","status":"expiring"},
			{"batch_id":19,"product_id":"salmon","product_name":"Лосось","remaining":{"qty":100,"unit":"g"},"expires_at":"2026-10-10T18:00:00+03:00","status":"expiring"}]`))
		case "/api/analytics/writeoffs":
			_, _ = w.Write([]byte(`{"total_minor":123450,"by_product":[{"product_id":"salmon","quantity":{"qty":1000,"unit":"g"},"cost_minor":50000}]}`))
		case "/api/analytics/shortfalls":
			_, _ = w.Write([]byte(`{"total_minor":5000,"by_product":[]}`))
		case "/api/clock":
			_, _ = w.Write([]byte(`{"now":"2026-10-09T12:00:00+03:00","timezone":"Europe/Moscow"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)
	tgc := &Telegram{Base: tgs.URL, Token: "T", ChatID: "42", HC: http.DefaultClient}
	n := &Notifier{API: &APIClient{Base: api.URL, Key: "v", HC: http.DefaultClient}, Out: tgc, Logf: t.Logf}
	e.bot = &Bot{N: n, TG: tgc, ChatID: "42"}
	if withAct {
		e.bot.Act = &APIClient{Base: api.URL, Key: "op", HC: http.DefaultClient}
	}
	return e
}

func (e *botEnv) texts() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.sent...)
}

func msg(chat int64, text string) Update {
	u := Update{}
	u.Message = &struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	}{Text: text}
	u.Message.Chat.ID = chat
	return u
}

func cb(chat int64, id, data string) Update {
	u := Update{}
	u.Callback = &struct {
		ID      string `json:"id"`
		Data    string `json:"data"`
		Message *struct {
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	}{ID: id, Data: data}
	u.Callback.Message = &struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	}{}
	u.Callback.Message.Chat.ID = chat
	return u
}

func TestForeignChatIgnored(t *testing.T) {
	e := newBotEnv(t, "operator", true)
	e.bot.HandleUpdate(context.Background(), msg(7, "/hot"))
	e.bot.HandleUpdate(context.Background(), cb(7, "c1", "ok:5"))
	if len(e.texts()) != 0 || len(e.calls) != 0 {
		t.Fatalf("чужой чат обработан: %v %v", e.texts(), e.calls)
	}
}

func TestCommands(t *testing.T) {
	e := newBotEnv(t, "operator", true)
	ctx := context.Background()
	for _, c := range []string{"/hot", "/stop", "/expiring", "/summary"} {
		e.bot.HandleUpdate(ctx, msg(42, c))
	}
	got := e.texts()
	if len(got) != 4 {
		t.Fatalf("ответов %d: %v", len(got), got)
	}
	checks := [][]string{
		{"1. A — цель 7 порц.", "Лосось, партия №17", "3. C"},
		{"Суши", "не хватает Рис: 500 г"},
		{"Истекает сегодня: 1 парт.", "700,00 ₽", "партия №17"},
		{"Сводка за 2026-10-09", "1 234,50 ₽"},
	}
	for i, ws := range checks {
		for _, w := range ws {
			if !strings.Contains(got[i], w) {
				t.Errorf("нет %q в %q", w, got[i])
			}
		}
	}
	if strings.Contains(got[0], "D") && strings.Contains(got[0], "4.") {
		t.Error("больше трёх предложений")
	}
	if strings.Contains(got[2], "№19") {
		t.Error("завтрашняя партия в «истекает сегодня»")
	}
}

func TestConfirmIdempotent(t *testing.T) {
	e := newBotEnv(t, "operator", true)
	ctx := context.Background()
	e.bot.HandleUpdate(ctx, cb(42, "c1", "ok:5"))
	e.bot.HandleUpdate(ctx, cb(42, "c1", "ok:5")) // тот же дубль Telegram
	e.bot.HandleUpdate(ctx, cb(42, "c2", "ok:5")) // повторное нажатие
	if len(e.calls) != 1 || e.calls[0] != `PATCH {"shelf_life_hours":48}` {
		t.Fatalf("вызовы API: %v", e.calls)
	}
	got := e.texts()
	if len(got) != 2 || !strings.Contains(got[0], "подтверждён") || !strings.Contains(got[1], "уже подтверждено") {
		t.Fatalf("сообщения: %v", got)
	}
}

func TestNoRightsDoesNotTouchAPI(t *testing.T) {
	e := newBotEnv(t, "viewer", true)
	e.bot.HandleUpdate(context.Background(), cb(42, "c1", "ok:5"))
	got := e.texts()
	if len(e.calls) != 0 || len(got) != 1 || !strings.Contains(got[0], "тикет") {
		t.Fatalf("calls=%v msgs=%v", e.calls, got)
	}
	e2 := newBotEnv(t, "operator", false)
	e2.bot.HandleUpdate(context.Background(), cb(42, "c1", "ok:5"))
	if g := e2.texts(); len(e2.calls) != 0 || len(g) != 1 || !strings.Contains(g[0], "тикет") {
		t.Fatalf("без ключа: %v", g)
	}
}

func TestEditExpiry(t *testing.T) {
	e := newBotEnv(t, "operator", true)
	ctx := context.Background()
	e.bot.HandleUpdate(ctx, cb(42, "c1", "ed:5"))
	e.bot.HandleUpdate(ctx, msg(42, "абв"))
	e.bot.HandleUpdate(ctx, msg(42, "12.10.2026"))
	if len(e.calls) != 1 || e.calls[0] != `PATCH {"expires_on":"2026-10-12"}` {
		t.Fatalf("вызовы: %v", e.calls)
	}
	e.bot.HandleUpdate(ctx, msg(42, "24")) // ждать больше нечего
	if len(e.calls) != 1 {
		t.Fatal("число без ожидания применено")
	}
	e2 := newBotEnv(t, "admin", true)
	e2.bot.HandleUpdate(ctx, cb(42, "c1", "ed:5"))
	e2.bot.HandleUpdate(ctx, msg(42, "72"))
	if len(e2.calls) != 1 || e2.calls[0] != `PATCH {"shelf_life_hours":72}` {
		t.Fatalf("вызовы: %v", e2.calls)
	}
}

func TestNotifyReceivingOnce(t *testing.T) {
	e := newBotEnv(t, "operator", true)
	ctx := context.Background()
	e.bot.NotifyReceiving(ctx)
	e.bot.NotifyReceiving(ctx)
	got := e.texts()
	if len(got) != 1 || !strings.Contains(got[0], "[ok:5][ed:5]") || !strings.Contains(got[0], "Лосось") {
		t.Fatalf("сообщения: %v", got)
	}
	e2 := newBotEnv(t, "operator", true)
	e2.bot.SeedReceiving(ctx)
	e2.bot.NotifyReceiving(ctx)
	if len(e2.texts()) != 0 {
		t.Fatal("после засева не должно быть сообщений")
	}
}

func TestRunPollsUpdates(t *testing.T) {
	var mu sync.Mutex
	var offsets []float64
	tgs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			var v map[string]any
			_ = json.Unmarshal(b, &v)
			mu.Lock()
			offsets = append(offsets, v["offset"].(float64))
			mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":10,"message":{"text":"/help","chat":{"id":42}}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer tgs.Close()
	tgc := &Telegram{Base: tgs.URL, Token: "T", ChatID: "42", HC: http.DefaultClient}
	bot := &Bot{N: &Notifier{Out: tgc, Logf: t.Logf}, TG: tgc, ChatID: "42"}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	bot.Run(ctx)
	mu.Lock()
	defer mu.Unlock()
	if len(offsets) < 2 || offsets[0] != 0 || offsets[1] != 11 {
		t.Fatalf("offset: %v", offsets)
	}
}
