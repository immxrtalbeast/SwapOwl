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

const botChat = 777

// fakeTG записывает вызовы Bot API и отдаёт заданные обновления.
type fakeTG struct {
	mu      sync.Mutex
	calls   []string // method|json
	updates []string // JSON-массивы для getUpdates
}

func (f *fakeTG) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	f.mu.Lock()
	f.calls = append(f.calls, method+"|"+string(b))
	var res string = "true"
	if method == "getUpdates" {
		res = "[]"
		if len(f.updates) > 0 {
			res, f.updates = f.updates[0], f.updates[1:]
		}
	}
	f.mu.Unlock()
	_, _ = w.Write([]byte(`{"ok":true,"result":` + res + `}`))
}

func (f *fakeTG) sent(method string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, method+"|") {
			out = append(out, c)
		}
	}
	return out
}

type fakeAPI struct {
	mu        sync.Mutex
	receiving []string // JSON-элементы /api/receiving
	patches   []string
	patchCode int
	patchBody string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/receiving":
		f.mu.Lock()
		_, _ = w.Write([]byte("[" + strings.Join(f.receiving, ",") + "]"))
		f.mu.Unlock()
	case "/api/stop-list":
		_, _ = w.Write([]byte(`[{"name":"Паста","missing":[{"product_name":"Сливки","missing":{"qty":300,"unit":"ml"}}]}]`))
	case "/api/hot-offers":
		_, _ = w.Write([]byte(`{"offers":[{"dish_name":"A","target_portions":5,"reasons":[{"batch_id":1}]},{"dish_name":"B","reasons":[{"batch_id":2}]},{"dish_name":"C","reasons":[{"batch_id":3}]},{"dish_name":"D","reasons":[{"batch_id":4}]}]}`))
	case "/api/alerts":
		_, _ = w.Write([]byte(`[{"batch_id":17,"product_id":"salmon","product_name":"Лосось","remaining":{"qty":1000,"unit":"g"},"expires_at":"2026-10-12T20:00:00+03:00","status":"expiring"},
		{"batch_id":18,"product_id":"salmon","product_name":"Лосось","remaining":{"qty":500,"unit":"g"},"expires_at":"2026-10-13T20:00:00+03:00","status":"expiring"}]`))
	case "/api/analytics/writeoffs":
		_, _ = w.Write([]byte(`{"total_minor":100000,"by_product":[{"product_id":"salmon","quantity":{"qty":1000,"unit":"g"},"cost_minor":50000}]}`))
	case "/api/analytics/shortfalls":
		_, _ = w.Write([]byte(`{"total_minor":0,"by_product":[]}`))
	case "/api/clock":
		_, _ = w.Write([]byte(`{"now":"2026-10-12T12:00:00+03:00","timezone":"Europe/Moscow"}`))
	default:
		if r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/expiry") {
			b, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			f.patches = append(f.patches, r.URL.Path+"|"+string(b))
			code, body := f.patchCode, f.patchBody
			if code == 0 || code/100 == 2 {
				f.receiving = nil // подтверждено: из приёмки пропадает
			}
			f.mu.Unlock()
			if code == 0 {
				code, body = 200, `{}`
			}
			w.WriteHeader(code)
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}
}

const recvItem = `{"id":5,"product_name":"Сливки","delivery_number":"42","quantity":{"qty":2000,"unit":"ml"},"remaining":{"qty":2000,"unit":"ml"},"received_at":"2026-10-12T10:00:00+03:00","expires_at":"2026-10-15T10:00:00+03:00","expiry_source":"product_default"}`

func newBot(t *testing.T) (*Bot, *fakeTG, *fakeAPI) {
	t.Helper()
	tgf, apif := &fakeTG{}, &fakeAPI{receiving: []string{recvItem}}
	tgs, as := httptest.NewServer(tgf), httptest.NewServer(apif)
	t.Cleanup(tgs.Close)
	t.Cleanup(as.Close)
	tg := &Telegram{Base: tgs.URL, Token: "TOK", ChatID: "777", HC: tgs.Client()}
	n := &Notifier{API: &APIClient{Base: as.URL, Key: "k", HC: as.Client()}, Out: tg, Buttons: tg}
	return &Bot{N: n, TG: tg, ChatID: botChat}, tgf, apif
}

func msgUpdate(chat int64, text string) Update {
	return Update{Message: &Message{Chat: Chat{ID: chat}, Text: text}}
}

func cbUpdate(chat int64, data string) Update {
	return Update{Callback: &Callback{ID: "cb", Data: data, Message: &Message{Chat: Chat{ID: chat}}}}
}

func TestForeignChatIgnored(t *testing.T) {
	b, tgf, apif := newBot(t)
	var logs []string
	b.Logf = func(f string, a ...any) { logs = append(logs, f) }
	b.HandleUpdate(context.Background(), msgUpdate(1, "/hot"))
	b.HandleUpdate(context.Background(), cbUpdate(1, "ok:5"))
	if len(tgf.calls) != 0 || len(apif.patches) != 0 {
		t.Fatalf("чужой чат вызвал действия: %v %v", tgf.calls, apif.patches)
	}
	if len(logs) != 2 {
		t.Fatalf("ожидалось 2 записи в логе, есть %d", len(logs))
	}
}

func TestCommands(t *testing.T) {
	b, tgf, _ := newBot(t)
	ctx := context.Background()
	for _, c := range []string{"/hot", "/stop", "/expiring", "/summary"} {
		b.HandleUpdate(ctx, msgUpdate(botChat, c))
	}
	sent := tgf.sent("sendMessage")
	if len(sent) != 4 {
		t.Fatalf("ожидалось 4 ответа, есть %d", len(sent))
	}
	hot, stop, exp, sum := sent[0], sent[1], sent[2], sent[3]
	if !strings.Contains(hot, "цель 5 порц.") || !strings.Contains(hot, "партия №1") || strings.Contains(hot, "D") && strings.Contains(hot, "4. ") {
		t.Errorf("/hot: %s", hot)
	}
	if strings.Contains(hot, "4. ") {
		t.Errorf("/hot должен быть топ-3: %s", hot)
	}
	if !strings.Contains(stop, "Паста") || !strings.Contains(stop, "Сливки") {
		t.Errorf("/stop: %s", stop)
	}
	// сегодня истекает только партия 17: 1000 г по 50 коп. = 500,00 ₽
	if !strings.Contains(exp, "№17") || strings.Contains(exp, "№18") || !strings.Contains(exp, "500,00") {
		t.Errorf("/expiring: %s", exp)
	}
	if !strings.Contains(sum, "Сводка") || !strings.Contains(sum, "1 000,00") {
		t.Errorf("/summary: %s", sum)
	}
}

func TestConfirmButtonAndDuplicate(t *testing.T) {
	b, tgf, apif := newBot(t)
	ctx := context.Background()
	b.HandleUpdate(ctx, cbUpdate(botChat, "ok:5"))
	b.HandleUpdate(ctx, cbUpdate(botChat, "ok:5")) // дубль от Telegram
	if len(apif.patches) != 1 {
		t.Fatalf("PATCH должен уйти один раз: %v", apif.patches)
	}
	if !strings.Contains(apif.patches[0], "/api/batches/5/expiry") || !strings.Contains(apif.patches[0], `"shelf_life_hours":72`) {
		t.Errorf("PATCH: %s", apif.patches[0])
	}
	sent := tgf.sent("sendMessage")
	if len(sent) != 2 || !strings.Contains(sent[0], "подтверждён") || !strings.Contains(sent[1], "уже подтверждён") {
		t.Fatalf("ответы: %v", sent)
	}
}

func TestEditExpiryFlow(t *testing.T) {
	b, tgf, apif := newBot(t)
	ctx := context.Background()
	b.HandleUpdate(ctx, cbUpdate(botChat, "ed:5"))
	if len(tgf.sent("sendMessage")) != 1 || !strings.Contains(tgf.sent("sendMessage")[0], "force_reply") {
		t.Fatal("ожидался запрос срока с force_reply")
	}
	b.HandleUpdate(ctx, msgUpdate(botChat, "завтра"))
	if len(apif.patches) != 0 {
		t.Fatal("непонятный ответ не должен менять API")
	}
	b.HandleUpdate(ctx, msgUpdate(botChat, "14.10.2026"))
	if len(apif.patches) != 1 || !strings.Contains(apif.patches[0], `"expires_on":"2026-10-14"`) {
		t.Fatalf("PATCH: %v", apif.patches)
	}
	b.HandleUpdate(ctx, msgUpdate(botChat, "48")) // ожидание сброшено
	if len(apif.patches) != 1 {
		t.Fatal("после применения ожидание должно сбрасываться")
	}
}

func TestEditHours(t *testing.T) {
	b, _, apif := newBot(t)
	ctx := context.Background()
	b.HandleUpdate(ctx, cbUpdate(botChat, "ed:5"))
	b.HandleUpdate(ctx, msgUpdate(botChat, "48"))
	if len(apif.patches) != 1 || !strings.Contains(apif.patches[0], `"shelf_life_hours":48`) {
		t.Fatalf("PATCH: %v", apif.patches)
	}
}

func TestNoRightsDoesNotRetry(t *testing.T) {
	b, tgf, apif := newBot(t)
	apif.patchCode = 403
	apif.patchBody = `{"error":{"code":"forbidden"}}`
	b.HandleUpdate(context.Background(), cbUpdate(botChat, "ok:5"))
	sent := tgf.sent("sendMessage")
	if len(apif.patches) != 1 || len(sent) != 1 || !strings.Contains(sent[0], "нет прав") {
		t.Fatalf("patches=%v sent=%v", apif.patches, sent)
	}
}

func TestReceivingNotificationOnce(t *testing.T) {
	b, tgf, _ := newBot(t)
	ctx := context.Background()
	b.N.Handle(ctx, Event{Type: "delivery.accepted", Data: "{}"})
	b.N.Handle(ctx, Event{Type: "delivery.accepted", Data: "{}"})
	b.N.CatchUp(ctx)
	sent := tgf.sent("sendMessage")
	if len(sent) < 1 {
		t.Fatal("нет сообщения о партии без срока")
	}
	n := 0
	for _, s := range sent {
		if strings.Contains(s, "Подтвердить срок") {
			n++
			var v struct{ Text string }
			_ = json.Unmarshal([]byte(strings.SplitN(s, "|", 2)[1]), &v)
			if !strings.Contains(s, `"callback_data":"ok:5"`) || !strings.Contains(s, `"callback_data":"ed:5"`) {
				t.Errorf("кнопки: %s", s)
			}
		}
	}
	if n != 1 {
		t.Fatalf("сообщение с кнопками должно уйти один раз, ушло %d", n)
	}
}

func TestRunLongPolling(t *testing.T) {
	b, tgf, _ := newBot(t)
	tgf.updates = []string{`[{"update_id":10,"message":{"chat":{"id":777},"text":"/stop"}}]`}
	ctx, cancel := context.WithCancel(context.Background())
	b.Logf = func(string, ...any) {}
	done := make(chan struct{})
	go func() { b.Run(ctx, 1); close(done) }()
	offsetSeen := func() bool {
		for _, c := range tgf.sent("getUpdates") {
			if strings.Contains(c, `"offset":11`) {
				return true
			}
		}
		return false
	}
	for i := 0; i < 2000 && (len(tgf.sent("sendMessage")) == 0 || !offsetSeen()); i++ {
		waitTick()
	}
	cancel()
	<-done
	if len(tgf.sent("sendMessage")) == 0 {
		t.Fatal("команда из getUpdates не обработана")
	}
	found := false
	for _, c := range tgf.sent("getUpdates") {
		if strings.Contains(c, `"offset":11`) {
			found = true
		}
	}
	if !found {
		t.Error("offset не сдвинут после обработки обновления")
	}
}

func waitTick() { time.Sleep(2 * time.Millisecond) }
