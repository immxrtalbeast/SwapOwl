package events

import (
	"encoding/json"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func TestPublishMonotonicIDAndAt(t *testing.T) {
	b := New(func() time.Time { return t0 })
	s := b.Subscribe()
	defer s.Unsubscribe()
	b.Publish(OrderAccepted, map[string]any{"order_id": "o1"})
	b.Publish(StockChanged, map[string]any{"product_ids": []string{"x"}})
	e1, e2 := <-s.C, <-s.C
	if e1.ID != 1 || e2.ID != 2 || e1.Type != OrderAccepted {
		t.Fatalf("got %+v %+v", e1, e2)
	}
	var m map[string]any
	if err := json.Unmarshal(e1.Data, &m); err != nil || m["at"] != "2026-10-10T12:00:00Z" || m["order_id"] != "o1" {
		t.Fatalf("data %s", e1.Data)
	}
}

func TestSlowSubscriberDroppedWithoutBlocking(t *testing.T) {
	b := New(func() time.Time { return t0 })
	slow, fast := b.Subscribe(), b.Subscribe()
	done := make(chan struct{})
	go func() {
		for range fast.C {
		}
		close(done)
	}()
	for i := 0; i < SubscriberBuffer+10; i++ {
		b.Publish(StockChanged, nil) // не должно зависнуть
	}
	n := 0
	for range slow.C { // канал закрыт шиной после переполнения
		n++
	}
	if n != SubscriberBuffer {
		t.Fatalf("slow got %d, want %d", n, SubscriberBuffer)
	}
	if b.Subscribers() != 1 {
		t.Fatalf("subscribers = %d, want 1", b.Subscribers())
	}
	b.Close()
	<-done
}

func TestCloseAndUnsubscribeIdempotent(t *testing.T) {
	b := New(func() time.Time { return t0 })
	s := b.Subscribe()
	s.Unsubscribe()
	s.Unsubscribe()
	b.Close()
	if _, ok := <-b.Subscribe().C; ok {
		t.Fatal("subscription after Close must be closed")
	}
}

func TestStopTracker(t *testing.T) {
	b := New(func() time.Time { return t0 })
	s := b.Subscribe()
	tr := NewStopTracker(b)
	tr.Update([]string{"a"})      // первое состояние — без события
	tr.Update([]string{"a"})      // без изменений
	tr.Update([]string{"b", "c"}) // a ушло, b и c добавились
	ev := <-s.C
	var m struct{ Added, Removed []string }
	if err := json.Unmarshal(ev.Data, &m); err != nil || ev.Type != StoplistChanged ||
		len(m.Added) != 2 || m.Added[0] != "b" || len(m.Removed) != 1 || m.Removed[0] != "a" {
		t.Fatalf("%s %s", ev.Type, ev.Data)
	}
	select {
	case e := <-s.C:
		t.Fatalf("unexpected %+v", e)
	default:
	}
}
