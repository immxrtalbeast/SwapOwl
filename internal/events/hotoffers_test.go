package events

import (
	"strings"
	"testing"
	"time"
)

func newBus() *Bus { return New(func() time.Time { return time.Unix(0, 0).UTC() }) }

func TestHotTrackerPublishesOnlyOnChange(t *testing.T) {
	bus := newBus()
	sub := bus.Subscribe()
	defer sub.Unsubscribe()
	tr := NewHotTracker(bus)

	if tr.Update("a", "x") {
		t.Fatal("первый отпечаток только запоминается")
	}
	if tr.Update("a", "x") {
		t.Fatal("тот же отпечаток не даёт события")
	}
	if !tr.Update("a", "y") {
		t.Fatal("новый отпечаток должен дать событие")
	}
	if tr.Update("b", "y") {
		t.Fatal("алгоритм b впервые — только запоминание")
	}
	if !tr.Update("b", "z") {
		t.Fatal("b изменился — событие")
	}
	got := []Event{<-sub.C, <-sub.C}
	for i, want := range []string{`"algo":"a"`, `"algo":"b"`} {
		if got[i].Type != HotOffersChanged || !strings.Contains(string(got[i].Data), want) {
			t.Fatalf("событие %d: %s %s", i, got[i].Type, got[i].Data)
		}
	}
	select {
	case e := <-sub.C:
		t.Fatalf("лишнее событие %s", e.Type)
	default:
	}
}

func TestFingerprintStable(t *testing.T) {
	a, _ := Fingerprint(map[string]int{"x": 1, "y": 2})
	b, _ := Fingerprint(map[string]int{"y": 2, "x": 1})
	c, _ := Fingerprint(map[string]int{"x": 1, "y": 3})
	if a != b || a == c {
		t.Fatalf("a=%s b=%s c=%s", a, b, c)
	}
}

func TestStandTick(t *testing.T) {
	t0 := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	tk := StandTick{Step: time.Minute}
	if tk.Due(t0) {
		t.Fatal("первый вызов не срабатывает")
	}
	if tk.Due(t0.Add(59 * time.Second)) {
		t.Fatal("раньше шага")
	}
	if !tk.Due(t0.Add(60 * time.Second)) {
		t.Fatal("шаг прошёл")
	}
	if tk.Due(t0.Add(90 * time.Second)) {
		t.Fatal("отсчёт от последнего срабатывания")
	}
	if !tk.Due(t0) {
		t.Fatal("время пошло назад (сброс) — пересчёт")
	}
}
