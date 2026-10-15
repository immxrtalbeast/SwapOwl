package notify

import (
	"context"
	"strings"
	"testing"
	"time"
)

func countWith(texts []string, sub string) int {
	n := 0
	for _, t := range texts {
		if strings.Contains(t, sub) {
			n++
		}
	}
	return n
}

// Меньше часа до срока: к первому сообщению о партии добавляется ровно одно напоминание, повторные проходы тихие.
func TestReminderOncePerBatchWithinHour(t *testing.T) {
	n, bot, _ := newNotifier(t, "2026-10-09T17:20:00+03:00")
	ctx := context.Background()
	n.CatchUp(ctx)
	n.CatchUp(ctx)
	n.CatchUp(ctx)
	texts := bot.texts()
	if got := countWith(texts, "Через 40 мин истекает срок: Лосось"); got != 1 {
		t.Fatalf("напоминаний %d, ждали 1: %q", got, texts)
	}
	if got := countWith(texts, "Истекает срок: Лосось"); got != 1 {
		t.Fatalf("первых сообщений %d: %q", got, texts)
	}
	if !strings.Contains(strings.Join(texts, "\n"), "Продвигайте блюда") {
		t.Fatalf("нет совета: %q", texts)
	}
}

// Рано (больше часа) и уже истёкшая партия напоминаний не получают.
func TestReminderNotEarlyAndNotExpired(t *testing.T) {
	n, bot, _ := newNotifier(t, "2026-10-09T16:00:00+03:00")
	n.CatchUp(context.Background())
	if got := countWith(bot.texts(), "Через"); got != 0 {
		t.Fatalf("напоминание раньше времени: %q", bot.texts())
	}
	n2, bot2, _ := newNotifier(t, "2026-10-09T18:30:00+03:00")
	n2.CatchUp(context.Background())
	if got := countWith(bot2.texts(), "Через"); got != 0 {
		t.Fatalf("напоминание после срока: %q", bot2.texts())
	}
}

func TestReminderDoomedText(t *testing.T) {
	loc := time.FixedZone("MSK", 3*3600)
	a := Alert{BatchID: 7, ProductName: "Лосось", Remaining: Qty{Qty: 600, Unit: "g"}, ExpiresAt: time.Date(2026, 10, 9, 18, 0, 0, 0, loc), Doomed: true}
	got := Reminder(a, 25*time.Minute, loc)
	for _, want := range []string{"Через 25 мин", "Партия №7", "до 18:00", "Продать уже не успеют"} {
		if !strings.Contains(got, want) {
			t.Errorf("нет %q в %q", want, got)
		}
	}
}

// Ручная пауза блюда — отдельное сообщение типа «Стоп-лист»; обычное изменение стоп-листа так не разбирается.
func TestHoldEventMessages(t *testing.T) {
	n, bot, _ := newNotifier(t, "2026-10-09T12:00:00+03:00")
	ctx := context.Background()
	n.Handle(ctx, Event{Type: "stoplist.changed", Data: `{"added":[],"removed":[],"reason":"manual_hold","dish_id":"soup","held":true,"note":"сломана плита"}`})
	n.Handle(ctx, Event{Type: "stoplist.changed", Data: `{"added":[],"removed":[],"reason":"manual_hold","dish_id":"soup","held":false}`})
	texts := bot.texts()
	if len(texts) != 2 || !strings.Contains(texts[0], "поставлено менеджером на паузу") || !strings.Contains(texts[0], "Причина: сломана плита") ||
		!strings.Contains(texts[1], "Пауза снята") {
		t.Fatalf("%q", texts)
	}
	if _, ok := parseHoldEvent(`{"added":["burger"],"removed":[]}`); ok {
		t.Fatal("обычное изменение стоп-листа принято за паузу")
	}
	if _, ok := parseHoldEvent(`не json`); ok {
		t.Fatal("мусор принят за паузу")
	}
}

// Выключенный тип «Стоп-лист» молчит и про паузу.
func TestHoldEventRespectsDisabledKind(t *testing.T) {
	n, bot, _ := newNotifier(t, "2026-10-09T12:00:00+03:00")
	n.Settings = LoadSettings("", t.Logf)
	n.Settings.Toggle(KindStop) // включён по умолчанию — выключаем
	n.Handle(context.Background(), Event{Type: "stoplist.changed", Data: `{"reason":"manual_hold","dish_id":"soup","held":true}`})
	if len(bot.texts()) != 0 {
		t.Fatalf("%q", bot.texts())
	}
}
