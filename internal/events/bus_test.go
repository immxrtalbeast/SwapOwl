package events

import (
	"encoding/json"
	"fmt"
	"sync"
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
	for i := 0; i < 3*SubscriberBuffer; i++ {
		b.Publish(OrderAccepted, nil) // не склеивается; не должно зависнуть
		<-fast.C                      // быстрый успевает за каждым событием, медленный не читает
	}
	n := 0
	for range slow.C { // канал закрыт шиной после переполнения
		n++
	}
	// буфер канала, очередь за ним и, возможно, одно событие в руках насоса
	if n < 2*SubscriberBuffer || n > 2*SubscriberBuffer+1 {
		t.Fatalf("slow got %d, want %d..%d", n, 2*SubscriberBuffer, 2*SubscriberBuffer+1)
	}
	if b.Subscribers() != 1 {
		t.Fatalf("subscribers = %d, want 1", b.Subscribers())
	}
	b.Close()
	if _, ok := <-fast.C; ok {
		t.Fatal("fast subscription must be closed by Close")
	}
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

func TestCoalesceSignalsInQueue(t *testing.T) {
	b := New(func() time.Time { return t0 })
	s := b.Subscribe()
	defer s.Unsubscribe()
	b.Publish(OrderAccepted, map[string]any{"order_id": "o1"})
	b.Publish(OrderAccepted, map[string]any{"order_id": "o2"})
	if e := <-s.C; e.Type != OrderAccepted {
		t.Fatal(e)
	}
	if e := <-s.C; e.Type != OrderAccepted {
		t.Fatal(e)
	}
	// прямая проверка склейки
	e1 := Event{ID: 1, Type: StockChanged, Data: []byte(`{"product_ids":["b"],"at":"1"}`)}
	e2 := Event{ID: 5, Type: StockChanged, Data: []byte(`{"product_ids":["a","b"],"at":"2"}`)}
	m, ok := coalesce(e1, e2)
	if !ok || m.ID != 5 || string(m.Data) != `{"at":"2","product_ids":["a","b"]}` {
		t.Fatalf("%v %+v %s", ok, m, m.Data)
	}
	if _, ok := coalesce(Event{Type: OrderAccepted, Data: []byte(`{}`)}, Event{Type: OrderAccepted, Data: []byte(`{}`)}); ok {
		t.Fatal("order.* must not merge")
	}
	h1 := Event{Type: HotOffersChanged, Data: []byte(`{"algo":"a","at":"1"}`)}
	h2 := Event{Type: HotOffersChanged, Data: []byte(`{"algo":"b","at":"2"}`)}
	if _, ok := coalesce(h1, h2); ok {
		t.Fatal("different algo must not merge")
	}
	s1 := Event{Type: StoplistChanged, Data: []byte(`{"added":["x","y"],"removed":[],"at":"1"}`)}
	s2 := Event{Type: StoplistChanged, Data: []byte(`{"added":[],"removed":["x"],"at":"2"}`)}
	m, ok = coalesce(s1, s2)
	if !ok || string(m.Data) != `{"added":["y"],"at":"2","removed":[]}` {
		t.Fatalf("%s", m.Data)
	}
}

func TestBurstCoalescedNoDisconnect(t *testing.T) {
	b := New(func() time.Time { return t0 })
	s := b.Subscribe()
	defer s.Unsubscribe()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b.Publish(OrderAccepted, map[string]any{"order_id": fmt.Sprint(i)})
			b.Publish(StockChanged, map[string]any{"product_ids": []string{fmt.Sprintf("p%02d", i)}})
		}(i)
	}
	wg.Wait()
	seen := map[string]bool{}
	orders := 0
	for len(seen) < 50 || orders < 50 {
		time.Sleep(time.Millisecond) // читающий подписчик с задержкой
		ev, ok := <-s.C
		if !ok {
			t.Fatal("subscriber disconnected")
		}
		switch ev.Type {
		case OrderAccepted:
			orders++
		case StockChanged:
			var m struct {
				IDs []string `json:"product_ids"`
			}
			_ = json.Unmarshal(ev.Data, &m)
			for _, id := range m.IDs {
				seen[id] = true
			}
		}
	}
	if b.Subscribers() != 1 {
		t.Fatal("subscriber dropped")
	}
}

func TestStockSignalsCoalesceBehindFullBuffer(t *testing.T) {
	b := New(func() time.Time { return t0 })
	s := b.Subscribe()
	defer s.Unsubscribe()
	for i := 0; i < SubscriberBuffer; i++ { // забиваем буфер канала, читатель стоит
		b.Publish(OrderAccepted, nil)
	}
	for i := 0; i < 500; i++ { // залп сигналов не переполняет очередь за буфером
		b.Publish(StockChanged, map[string]any{"product_ids": []string{fmt.Sprintf("p%03d", i)}})
	}
	if b.Subscribers() != 1 {
		t.Fatal("subscriber dropped")
	}
	ids, stocks := map[string]bool{}, 0
	var lastID int64
	for len(ids) < 500 {
		e, ok := <-s.C
		if !ok {
			t.Fatal("closed")
		}
		lastID = e.ID
		if e.Type != StockChanged {
			continue
		}
		stocks++
		var m struct {
			IDs []string `json:"product_ids"`
		}
		_ = json.Unmarshal(e.Data, &m)
		for _, id := range m.IDs {
			ids[id] = true
		}
	}
	// одно событие могло быть уже в руках насоса, остальные склеены; последнее несёт последний id
	if stocks > 3 || lastID != SubscriberBuffer+500 {
		t.Fatalf("stock events %d, last id %d", stocks, lastID)
	}
}
