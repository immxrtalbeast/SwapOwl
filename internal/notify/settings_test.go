package notify

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const evExpiring = `{"batch_id":17}`

func settingsN(t *testing.T, now, file string) (*Notifier, *tg, *Bot) {
	t.Helper()
	n, bot, tgc := newNotifier(t, now)
	n.Settings = LoadSettings(file, t.Logf)
	return n, bot, &Bot{N: n, TG: tgc, ChatID: botChat, Logf: t.Logf}
}

func press(b *Bot, data string) {
	b.HandleUpdate(context.Background(), Update{Callback: &Callback{ID: "c", Data: data, Message: &Message{ID: 9, Chat: Chat{ID: botChat}}}})
}

func TestSettingsToggleDisablesType(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state.json")
	n, out, b := settingsN(t, "2026-10-09T12:00:00+03:00", file)
	b.HandleUpdate(context.Background(), msgUpdate(botChat, "/settings"))
	if got := out.texts(); len(got) != 1 || !strings.Contains(got[0], "Настройки уведомлений") {
		t.Fatalf("/settings: %v", got)
	}
	press(b, "st:t:expiring")
	if n.Settings.Enabled(KindExpiring) {
		t.Fatal("тип не выключился")
	}
	n.Handle(context.Background(), Event{Type: "batch.expiring", Data: evExpiring})
	for _, m := range out.texts() {
		if strings.Contains(m, "Истекает срок") {
			t.Fatalf("отключённый тип отправлен: %s", m)
		}
	}
	press(b, "st:t:expiring") // включили обратно: старое событие не всплывает
	n.CatchUp(context.Background())
	for _, m := range out.texts() {
		if strings.Contains(m, "Истекает срок") {
			t.Fatalf("отключённое событие всплыло после включения: %s", m)
		}
	}
}

func TestSettingsSurviveRestartAndDedupe(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state.json")
	n, out, b := settingsN(t, "2026-10-09T12:00:00+03:00", file)
	press(b, "st:t:hall")
	_ = n.Settings.SetQuiet(true, "23:00", "06:00")
	n.Handle(context.Background(), Event{Type: "batch.expiring", Data: evExpiring})
	if len(out.texts()) < 1 {
		t.Fatal("сообщение не ушло")
	}
	sent := 0
	for _, m := range out.texts() {
		if strings.Contains(m, "Истекает срок") {
			sent++
		}
	}
	// «перезапуск»: новый процесс, новая память, тот же файл
	n2, out2, _ := settingsN(t, "2026-10-09T12:00:00+03:00", file)
	if n2.Settings.Enabled(KindHall) {
		t.Fatal("выключенный тип забыт после перезапуска")
	}
	if on, f, to := n2.Settings.QuietInfo(); !on || f != "23:00" || to != "06:00" {
		t.Fatalf("тихие часы: %v %s %s", on, f, to)
	}
	n2.Handle(context.Background(), Event{Type: "batch.expiring", Data: evExpiring})
	n2.CatchUp(context.Background())
	if sent != 1 || len(out2.texts()) != 0 {
		t.Fatalf("дубликат после перезапуска: %d, %v", sent, out2.texts())
	}
}

func TestQuietHoursQueueAndMerge(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state.json")
	n, out, _ := settingsN(t, "2026-10-09T23:30:00+03:00", file)
	_ = n.Settings.SetQuiet(true, "22:00", "07:00")
	n.DigestAt = "" // сводку здесь не проверяем
	ctx := context.Background()
	n.Handle(ctx, Event{Type: "batch.expiring", Data: evExpiring})
	if err := n.deliver(ctx, KindStop, "", "⛔ Паста в стопе", n.Out); err != nil {
		t.Fatal(err)
	}
	if err := n.deliver(ctx, KindHall, "hall:2026-10-09", "Подсказка залу", n.Out); err != nil {
		t.Fatal(err)
	}
	n.Tick(ctx)
	if len(out.texts()) != 0 {
		t.Fatalf("в тихие часы ушло: %v", out.texts())
	}
	if n.Settings.QueueLen() != 1 {
		t.Fatalf("в очереди %d, нужно 1 (стоп-лист и подсказки залу не копятся)", n.Settings.QueueLen())
	}
	// утро, новый процесс
	n2, out2, _ := settingsN(t, "2026-10-10T07:05:00+03:00", file)
	n2.Tick(ctx)
	n2.Tick(ctx)
	got := out2.texts()
	if len(got) != 1 {
		t.Fatalf("утром нужно одно сообщение: %v", got)
	}
	for _, want := range []string{"Лосось"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("нет %q: %s", want, got[0])
		}
	}
	if strings.Contains(got[0], "Паста") || strings.Contains(got[0], "Подсказка залу") {
		t.Error("стоп-лист попал в накопленное")
	}
	if n2.Settings.QueueLen() != 0 {
		t.Fatal("очередь не очищена")
	}
}

func TestPauseAndResumeNow(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state.json")
	n, out, b := settingsN(t, "2026-10-09T12:00:00+03:00", file)
	press(b, "st:p:4")
	if n.Settings.PausedUntil(n.now(context.Background())).IsZero() {
		t.Fatal("пауза не стоит")
	}
	n.Handle(context.Background(), Event{Type: "batch.expiring", Data: evExpiring})
	for _, m := range out.texts() {
		if strings.Contains(m, "Истекает срок") {
			t.Fatal("во время паузы ушло сообщение")
		}
	}
	press(b, "st:p:0") // «Включить сейчас» — копилка уходит
	var merged bool
	for _, m := range out.texts() {
		merged = merged || (strings.Contains(m, "Накопилось") && strings.Contains(m, "Лосось"))
	}
	if !merged {
		t.Fatalf("после «Включить сейчас» нет накопленного: %v", out.texts())
	}
}

func TestQuietCommand(t *testing.T) {
	n, _, b := settingsN(t, "2026-10-09T12:00:00+03:00", "")
	b.HandleUpdate(context.Background(), msgUpdate(botChat, "/quiet 23:00 08:30"))
	if on, f, to := n.Settings.QuietInfo(); !on || f != "23:00" || to != "08:30" {
		t.Fatalf("%v %s %s", on, f, to)
	}
	b.HandleUpdate(context.Background(), msgUpdate(botChat, "/quiet 25:00 08:30"))
	if _, f, _ := n.Settings.QuietInfo(); f != "23:00" {
		t.Fatal("мусорное время принято")
	}
}

func TestCorruptStateFileFallsBackToDefaults(t *testing.T) {
	for name, content := range map[string]string{
		"мусор":  "{не json",
		"версия": `{"version":99,"disabled":{"stop":true}}`,
	} {
		file := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		var logs []string
		s := LoadSettings(file, func(f string, a ...any) { logs = append(logs, f) })
		if !s.Enabled(KindStop) || len(logs) == 0 {
			t.Fatalf("%s: значения по умолчанию и запись в журнал: %v", name, logs)
		}
		s.Toggle(KindStop) // и записывать после этого можно
		if LoadSettings(file, nil).Enabled(KindStop) {
			t.Fatalf("%s: состояние не записалось поверх повреждённого", name)
		}
	}
}

// flakyTG отвечает заданными кодами по очереди, дальше 200.
type flakyTG struct {
	mu    sync.Mutex
	codes []int
	ok    int
}

func (f *flakyTG) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.codes) > 0 {
		c := f.codes[0]
		f.codes = f.codes[1:]
		w.WriteHeader(c)
		if c == 429 {
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":3}}`))
		}
		return
	}
	f.ok++
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func TestRetryAfter429(t *testing.T) {
	ft := &flakyTG{codes: []int{429, 502}}
	srv := httptest.NewServer(ft)
	defer srv.Close()
	n := &Notifier{Out: &Telegram{Base: srv.URL, Token: "T", ChatID: "1", HC: srv.Client()}, Logf: t.Logf}
	var sleeps []time.Duration
	n.Sleep = func(d time.Duration) { sleeps = append(sleeps, d) }
	if err := n.sendRetry(context.Background(), n.Out, "x"); err != nil {
		t.Fatal(err)
	}
	if len(sleeps) != 2 || sleeps[0] != 3*time.Second || sleeps[1] != 4*time.Second || ft.ok != 1 {
		t.Fatalf("паузы %v, отправок %d (ждём 3s по retry_after, затем нарастающая 4s)", sleeps, ft.ok)
	}
}

func TestRetryGivesUpAndDoesNotBlockOthers(t *testing.T) {
	ft := &flakyTG{codes: []int{500, 500, 500}}
	srv := httptest.NewServer(ft)
	defer srv.Close()
	n := &Notifier{Out: &Telegram{Base: srv.URL, Token: "T", ChatID: "1", HC: srv.Client()}, Logf: t.Logf, Attempts: 3}
	n.Sleep = func(time.Duration) {}
	if err := n.sendRetry(context.Background(), n.Out, "a"); err == nil {
		t.Fatal("после N попыток нужна ошибка")
	}
	if err := n.sendRetry(context.Background(), n.Out, "b"); err != nil || ft.ok != 1 {
		t.Fatalf("следующее сообщение должно уйти: %v %d", err, ft.ok)
	}
	ft.codes = []int{400}
	n.Sleep = func(time.Duration) { t.Fatal("4xx (кроме 429) не повторяется") }
	if err := n.sendRetry(context.Background(), n.Out, "c"); err == nil {
		t.Fatal("ждали ошибку 400")
	}
}

// Подсказки залу в тихие часы не копятся в общей очереди (она ушла бы менеджеру): HallTick повторит их, когда тишина кончится.
func TestHallTipsWaitForQuietEndAndGoToHall(t *testing.T) {
	n, _, tgf, api := newN(t)
	n.Settings = LoadSettings(filepath.Join(t.TempDir(), "state.json"), t.Logf)
	hall := &fakeTG{}
	hs := httptest.NewServer(hall)
	defer hs.Close()
	n.Hall = &HallConfig{At: "10:00", Out: &Telegram{Base: hs.URL, Token: "TOK", ChatID: "-100", HC: hs.Client()}}
	api.set(func() {
		api.hot = `{"offers":[{"dish_id":"steak","dish_name":"Стейк","reasons":[{"product_name":"Лосось","expires_at":"2026-10-12T18:00:00+03:00"}]}]}`
	})
	_ = n.Settings.SetQuiet(true, "10:00", "11:00") // часы стенда — 10:30
	ctx := context.Background()
	n.HallTick(ctx)
	if len(hall.sent("sendMessage")) != 0 || len(tgf.sent("sendMessage")) != 0 || n.Settings.QueueLen() != 0 {
		t.Fatalf("в тишину ушло: зал %v, менеджер %v, очередь %d", hall.sent("sendMessage"), tgf.sent("sendMessage"), n.Settings.QueueLen())
	}
	_ = n.Settings.SetQuiet(false, "", "")
	n.HallTick(ctx)
	if got := hall.sent("sendMessage"); len(got) != 1 || !strings.Contains(got[0], "Стейк") {
		t.Fatalf("залу после тишины: %v", got)
	}
	if len(tgf.sent("sendMessage")) != 0 {
		t.Fatalf("подсказки залу ушли менеджеру: %v", tgf.sent("sendMessage"))
	}
}

// Изменение стоп-листа в тишину не теряется: снимок не продвигается, разницу отправляет Tick после окончания тишины.
func TestStopChangeDuringQuietIsSentAfter(t *testing.T) {
	n, _, tgf, api := newN(t)
	n.Settings = LoadSettings(filepath.Join(t.TempDir(), "state.json"), t.Logf)
	ctx := context.Background()
	n.SeedStop(ctx)
	_ = n.Settings.SetQuiet(true, "10:00", "11:00")
	api.set(func() { api.stop = stopBurger })
	n.FlushStop(ctx)
	if len(tgf.sent("sendMessage")) != 0 {
		t.Fatalf("в тишину ушло: %v", tgf.sent("sendMessage"))
	}
	_ = n.Settings.SetQuiet(false, "", "")
	n.FlushStop(ctx)
	got := tgf.sent("sendMessage")
	if len(got) != 1 || !strings.Contains(got[0], "Бургер") {
		t.Fatalf("после тишины: %v", got)
	}
	n.FlushStop(ctx)
	if len(tgf.sent("sendMessage")) != 1 {
		t.Fatal("повтор той же разницы")
	}
}

// Подсказки поставок идут следом за сводкой: если сводка отложена в тишину, их не шлют.
func TestSuggestionsFollowDigestNotQuiet(t *testing.T) {
	n, _, _ := settingsN(t, "2026-10-09T22:30:00+03:00", filepath.Join(t.TempDir(), "state.json"))
	var logs []string
	n.Logf = func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }
	ctx := context.Background()
	from, to := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	_ = n.Settings.SetQuiet(true, "22:00", "07:00")
	if err := n.sendDigest(ctx, from, to, "2026-10-09"); err != nil {
		t.Fatal(err)
	}
	if n.Settings.QueueLen() != 1 || strings.Contains(strings.Join(logs, "\n"), "предложения поставок") {
		t.Fatalf("в тишину: очередь %d, логи %v", n.Settings.QueueLen(), logs)
	}
	_ = n.Settings.SetQuiet(false, "", "")
	if err := n.sendDigest(ctx, from, to, "2026-10-10"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "предложения поставок") {
		t.Fatalf("после тишины подсказок нет: %v", logs)
	}
}
