package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAPI2 — настраиваемый фейк API для FS-162.
type fakeAPI2 struct {
	mu          sync.Mutex
	stop        string
	suggestions string
	hot         string
	deliveries  []string
	delCode     int
	seen        map[string]bool
}

func (f *fakeAPI2) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/api/stop-list":
		_, _ = w.Write([]byte(f.stop))
	case "/api/supply-suggestions":
		_, _ = w.Write([]byte(f.suggestions))
	case "/api/hot-offers":
		_, _ = w.Write([]byte(f.hot))
	case "/api/products":
		_, _ = w.Write([]byte(`[{"id":"salmon","shelf_life_hours":72}]`))
	case "/api/clock":
		_, _ = w.Write([]byte(`{"now":"2026-10-12T10:30:00+03:00","timezone":"Europe/Moscow"}`))
	case "/api/deliveries":
		b, _ := io.ReadAll(r.Body)
		f.deliveries = append(f.deliveries, string(b))
		if f.delCode != 0 {
			w.WriteHeader(f.delCode)
		} else if seen := f.seen[string(b)]; seen {
			w.WriteHeader(200)
		} else {
			if f.seen == nil {
				f.seen = map[string]bool{}
			}
			f.seen[string(b)] = true
			w.WriteHeader(201)
		}
		_, _ = w.Write([]byte(`{}`))
	default:
		http.NotFound(w, r)
	}
}

const stopBurger = `[{"id":"burger","name":"Бургер","missing":[{"product_name":"Булочка","required":{"qty":1,"unit":"pcs"},"available":{"qty":0,"unit":"pcs"},"missing":{"qty":1,"unit":"pcs"}}]}]`

func newN(t *testing.T) (*Notifier, *Bot, *fakeTG, *fakeAPI2) {
	t.Helper()
	tgf, apif := &fakeTG{}, &fakeAPI2{stop: "[]", suggestions: "[]", hot: `{"offers":[]}`}
	tgs, as := httptest.NewServer(tgf), httptest.NewServer(apif)
	t.Cleanup(tgs.Close)
	t.Cleanup(as.Close)
	tg := &Telegram{Base: tgs.URL, Token: "TOK", ChatID: "777", HC: tgs.Client()}
	n := &Notifier{API: &APIClient{Base: as.URL, Key: "k", HC: as.Client()}, Out: tg, Buttons: tg}
	return n, &Bot{N: n, TG: tg, ChatID: botChat}, tgf, apif
}

func (f *fakeAPI2) set(fn func()) { f.mu.Lock(); fn(); f.mu.Unlock() }

func TestStopListDiff(t *testing.T) {
	n, _, tgf, api := newN(t)
	ctx := context.Background()
	n.SeedStop(ctx)
	api.set(func() { api.stop = stopBurger })
	n.FlushStop(ctx)
	got := tgf.sent("sendMessage")
	if len(got) != 1 || !strings.Contains(got[0], "⛔ Бургер в стопе: нет булочки (нужно 1 шт, есть 0)") {
		t.Fatalf("сообщения: %v", got)
	}
	n.FlushStop(ctx) // без изменений — тишина
	if len(tgf.sent("sendMessage")) != 1 {
		t.Fatal("повтор без изменений")
	}
	api.set(func() { api.stop = "[]" })
	n.FlushStop(ctx)
	got = tgf.sent("sendMessage")
	if len(got) != 2 || !strings.Contains(got[1], "✅ Бургер снова в продаже") {
		t.Fatalf("сообщения: %v", got)
	}
}

func TestStopListFirstSnapshotSilent(t *testing.T) {
	n, _, tgf, api := newN(t)
	api.set(func() { api.stop = stopBurger })
	n.FlushStop(context.Background()) // снимка не было: он первый
	if len(tgf.calls) != 0 {
		t.Fatalf("первый снимок прислал сообщение: %v", tgf.calls)
	}
	n.FlushStop(context.Background())
	if len(tgf.calls) != 0 {
		t.Fatal("то же состояние — сообщения быть не должно")
	}
}

func TestStopListBurstMerged(t *testing.T) {
	n, _, tgf, api := newN(t)
	n.StopWindow = 50 * time.Millisecond
	ctx := context.Background()
	n.SeedStop(ctx)
	api.set(func() { api.stop = stopBurger })
	for i := 0; i < 5; i++ {
		n.Handle(ctx, Event{Type: "stoplist.changed"})
	}
	time.Sleep(300 * time.Millisecond)
	if got := tgf.sent("sendMessage"); len(got) != 1 {
		t.Fatalf("залп должен дать одно сообщение: %v", got)
	}
}

const sugNow = `[{"product_id":"salmon","product_name":"Лосось","quantity":{"qty":4000,"unit":"g"},"urgency":"now","explanation":"Кончится сегодня","shelf_life_hours":72},
{"product_id":"rice","product_name":"Рис","quantity":{"qty":1000,"unit":"g"},"urgency":"soon"}]`

func TestOrderFlow(t *testing.T) {
	n, b, tgf, api := newBot2(t)
	ctx := context.Background()
	api.set(func() { api.suggestions = sugNow })
	b.HandleUpdate(ctx, msgUpdate(botChat, "/order"))
	msgs := tgf.sent("sendMessage")
	if len(msgs) != 1 || !strings.Contains(msgs[0], "acc:salmon") || strings.Contains(msgs[0], "Рис") {
		t.Fatalf("ожидалась одна строка «сейчас» с кнопкой: %v", msgs)
	}
	b.HandleUpdate(ctx, cbUpdate(botChat, "acc:salmon"))
	msgs = tgf.sent("sendMessage")
	if last := msgs[len(msgs)-1]; !strings.Contains(last, "force_reply") || !strings.Contains(last, "Обычно — 72 ч") {
		t.Fatalf("нет запроса срока с подсказкой: %s", last)
	}
	if len(api.deliveries) != 0 {
		t.Fatal("поставка до ввода срока")
	}
	b.HandleUpdate(ctx, msgUpdate(botChat, "годен до 15.10"))
	if len(api.deliveries) != 1 || !strings.Contains(api.deliveries[0], `"expires_on":"2026-10-15"`) ||
		!strings.Contains(api.deliveries[0], `"id":"TG-salmon-cb1"`) {
		t.Fatalf("поставка: %v", api.deliveries)
	}
	if m := tgf.sent("sendMessage"); !strings.Contains(m[len(m)-1], "✅ Поставка принята: Лосось, 4 кг, годен до 15.10.2026") {
		t.Fatalf("ответ без итоговой даты: %v", m[len(m)-1])
	}
	// вторая настоящая поставка в тот же день и с тем же количеством — другое нажатие, другой id, новая партия
	b.HandleUpdate(ctx, cbUpdateID(botChat, "cb2", "acc:salmon"))
	b.HandleUpdate(ctx, msgUpdate(botChat, "72 ч"))
	if len(api.deliveries) != 2 || !strings.Contains(api.deliveries[1], `"id":"TG-salmon-cb2"`) {
		t.Fatalf("поставки: %v", api.deliveries)
	}
	if m := tgf.sent("sendMessage"); !strings.Contains(m[len(m)-1], "✅ Поставка принята") || !strings.Contains(m[len(m)-1], "годен до 15.10.2026") {
		t.Fatalf("201 должен быть «принята», 72 ч от 12.10 10:30 = 15.10: %v", m[len(m)-1])
	}
	// повторная доставка того же нажатия: 200 от API — отдельный ответ, не «принята»
	b.HandleUpdate(ctx, cbUpdateID(botChat, "cb2", "acc:salmon"))
	b.HandleUpdate(ctx, msgUpdate(botChat, "72 ч"))
	if m := tgf.sent("sendMessage"); !strings.Contains(m[len(m)-1], "уже была принята ранее") || strings.Contains(m[len(m)-1], "✅") {
		t.Fatalf("replay: %v", m[len(m)-1])
	}
	_ = n
}

func TestOrderPastDateRejected(t *testing.T) {
	_, b, tgf, api := newN(t)
	ctx := context.Background()
	api.set(func() { api.suggestions = sugNow })
	b.HandleUpdate(ctx, cbUpdate(botChat, "acc:salmon"))
	b.HandleUpdate(ctx, msgUpdate(botChat, "годен до 10.10"))
	if len(api.deliveries) != 0 {
		t.Fatalf("прошедшая дата принята: %v", api.deliveries)
	}
	if m := tgf.sent("sendMessage"); !strings.Contains(m[len(m)-1], "в прошлом") {
		t.Fatalf("нет отказа: %v", m[len(m)-1])
	}
}

func TestSendSuggestionsNoneFlag(t *testing.T) {
	n, _, _, _ := newN(t)
	if msg, none := n.SendSuggestions(context.Background()); !none || msg == "" {
		t.Fatalf("нечего слать: %q %v", msg, none)
	}
}

func TestOrderNoRightsAndGone(t *testing.T) {
	_, b, tgf, api := newBot2(t)
	ctx := context.Background()
	b.HandleUpdate(ctx, cbUpdate(botChat, "acc:salmon")) // предложения нет
	if len(api.deliveries) != 0 || !strings.Contains(strings.Join(tgf.sent("sendMessage"), ""), "неактуально") {
		t.Fatal("устаревшая кнопка")
	}
	api.set(func() { api.suggestions = sugNow; api.delCode = 403 })
	b.HandleUpdate(ctx, cbUpdate(botChat, "acc:salmon"))
	b.HandleUpdate(ctx, msgUpdate(botChat, "48"))
	if m := tgf.sent("sendMessage"); !strings.Contains(m[len(m)-1], "operator") {
		t.Fatalf("нет сообщения о правах: %v", m)
	}
}

func newBot2(t *testing.T) (*Notifier, *Bot, *fakeTG, *fakeAPI2) { return newN(t) }

func TestParseSupplyExpiry(t *testing.T) {
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	for in, want := range map[string]string{
		"годен до 15.10": "2026-10-15", "15.10": "2026-10-15", "до 01.02": "2026-02-01", "12.10": "2026-10-12", "годен до 01.02.2027": "2027-02-01",
	} {
		on, h, ok := ParseSupplyExpiry(in, now)
		if !ok || on != want || h != 0 {
			t.Errorf("%q: %q %d %v", in, on, h, ok)
		}
	}
	for in, want := range map[string]int{"72 ч": 72, "72ч": 72, "48": 48, "5 часов": 5} {
		on, h, ok := ParseSupplyExpiry(in, now)
		if !ok || on != "" || h != want {
			t.Errorf("%q: %q %d %v", in, on, h, ok)
		}
	}
	// прошедшая дата без года — не следующий год (годен до 10.10 при сегодня 12.10)
	if on, _, ok := ParseSupplyExpiry("годен до 10.10", now); !ok || on != "2026-10-10" {
		t.Errorf("прошедшая дата должна остаться прошедшей: %q %v", on, ok)
	}
	dec := time.Date(2026, 12, 30, 10, 0, 0, 0, time.UTC)
	if on, _, ok := ParseSupplyExpiry("05.01", dec); !ok || on != "2027-01-05" {
		t.Errorf("декабрь→январь переносится на следующий год: %q %v", on, ok)
	}
	if on, _, _ := ParseSupplyExpiry("10.12", dec); on != "2026-12-10" {
		t.Errorf("прошедшая дата в декабре: %q", on)
	}
	for _, in := range []string{"", "завтра", "31.02", "0 ч", "99999 ч", "годен"} {
		if _, _, ok := ParseSupplyExpiry(in, now); ok {
			t.Errorf("%q принят", in)
		}
	}
}

func TestHallTips(t *testing.T) {
	n, _, _, api := newN(t)
	hall := &fakeTG{}
	hs := httptest.NewServer(hall)
	defer hs.Close()
	n.Hall = &HallConfig{At: "10:00", Out: &Telegram{Base: hs.URL, Token: "TOK", ChatID: "-100", HC: hs.Client()}}
	api.set(func() {
		api.stop = `[{"id":"stopped","name":"Стоповое","missing":[]}]`
		api.hot = `{"offers":[
		 {"dish_id":"steak","dish_name":"Стейк","reasons":[{"product_name":"Лосось","expires_at":"2026-10-12T18:00:00+03:00"}]},
		 {"dish_id":"hid","dish_name":"Скрытое","reasons":[]},
		 {"dish_id":"stopped","dish_name":"Стоповое","reasons":[]}],
		 "manager_picks":[{"dish_id":"soup","dish_name":"Суп","note":"к обеду"}],
		 "hidden":[{"dish_id":"hid"}]}`
	})
	n.HallTick(context.Background()) // 10:30 ≥ 10:00
	n.HallTick(context.Background()) // второй раз за день — нет
	got := hall.sent("sendMessage")
	if len(got) != 1 {
		t.Fatalf("сообщения залу: %v", got)
	}
	m := got[0]
	if !strings.Contains(m, "Стейк — лосось нужно продать сегодня") || !strings.Contains(m, "Суп — к обеду") ||
		strings.Contains(m, "Скрытое") || strings.Contains(m, "Стоповое") || !strings.Contains(m, `"chat_id":"-100"`) {
		t.Fatalf("текст: %s", m)
	}
}

func TestHallDisabledAndStartup(t *testing.T) {
	n, _, tgf, _ := newN(t)
	n.HallTick(context.Background()) // Hall == nil
	if len(tgf.calls) != 0 {
		t.Fatal("рассылка без чата")
	}
}

func TestStartMiniApp(t *testing.T) {
	_, b, tgf, _ := newN(t)
	b.MiniAppURL = "https://app.example/mini"
	b.HandleUpdate(context.Background(), msgUpdate(botChat, "/start"))
	m := tgf.sent("sendMessage")
	if len(m) != 1 || !strings.Contains(m[0], `"web_app":{"url":"https://app.example/mini"}`) || !strings.Contains(m[0], "Открыть SwapOwl") {
		t.Fatalf("/start: %v", m)
	}
	b.MiniAppURL = ""
	b.HandleUpdate(context.Background(), msgUpdate(botChat, "/start"))
	if m := tgf.sent("sendMessage"); strings.Contains(m[1], "web_app") {
		t.Fatal("кнопка без MINIAPP_URL")
	}
}

func TestSetChatMenuButton(t *testing.T) {
	_, b, tgf, _ := newN(t)
	if err := b.TG.SetChatMenuButton(context.Background(), "https://app.example/mini"); err != nil {
		t.Fatal(err)
	}
	m := tgf.sent("setChatMenuButton")
	if len(m) != 1 || !strings.Contains(m[0], `"type":"web_app"`) || !strings.Contains(m[0], `"chat_id":777`) {
		t.Fatalf("menu button: %v", m)
	}
}
