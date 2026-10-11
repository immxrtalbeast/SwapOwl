package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"umirhack/internal/api"
	"umirhack/internal/domain"
	"umirhack/internal/events"
	"umirhack/internal/store"
)

var t0 = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

type fakeStock struct {
	api.Stock
	resp store.DeliveryResponse
}

func (f fakeStock) ReceiveDelivery(context.Context, store.DeliveryInput, func() time.Time, *time.Location) (store.DeliveryResponse, error) {
	return f.resp, nil
}

type fakeOrders struct {
	api.Orders
	resp store.OrderResponse
}

func (f fakeOrders) PlaceOrder(context.Context, store.OrderInput, func() time.Time) (store.OrderResponse, error) {
	return f.resp, nil
}

// fakeMenu: блюдо d1 нужен продукт p1; остаток меняет тест.
type fakeMenu struct{ have domain.Qty }

func (m *fakeMenu) MenuSnapshot(context.Context, time.Time) (store.MenuSnapshot, error) {
	return store.MenuSnapshot{
		Dishes:    []domain.Dish{{ID: "d1", Recipe: []domain.RecipeItem{{ProductID: "p1", Quantity: 10}}}},
		Available: map[string]domain.Qty{"p1": m.have},
	}, nil
}

func drain(s *events.Subscription) []string {
	var out []string
	for {
		select {
		case e := <-s.C:
			out = append(out, e.Type+" "+string(e.Data))
		default:
			return out
		}
	}
}

func newPub(menu *fakeMenu) (*publisher, *events.Subscription) {
	bus := events.New(func() time.Time { return t0 })
	p := newPublisher(bus, menu, func() time.Time { return t0 }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.prime(context.Background())
	return p, bus.Subscribe()
}

func TestDeliveryPublishesOnceNotOnReplay(t *testing.T) {
	menu := &fakeMenu{have: 0}
	p, sub := newPub(menu)
	resp := store.DeliveryResponse{ID: "D1", Batches: []store.BatchJSON{{ProductID: "p1"}, {ProductID: "p1"}}}
	s := pubStock{fakeStock{resp: resp}, p}

	menu.have = 100 // поставка вернула блюдо из стоп-листа
	if _, err := s.ReceiveDelivery(context.Background(), store.DeliveryInput{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(drain(sub), "\n")
	for _, want := range []string{`delivery.accepted {"at":"2026-10-10T12:00:00Z","delivery_id":"D1","product_ids":["p1"]}`,
		"stock.changed", `stoplist.changed {"added":[],"at":"2026-10-10T12:00:00Z","removed":["d1"]}`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}

	resp.Replayed = true
	s = pubStock{fakeStock{resp: resp}, p}
	_, _ = s.ReceiveDelivery(context.Background(), store.DeliveryInput{}, nil, nil)
	if got := drain(sub); len(got) != 0 {
		t.Fatalf("replay published %v", got)
	}
}

func TestOrderAcceptedAndRejected(t *testing.T) {
	menu := &fakeMenu{have: 10}
	p, sub := newPub(menu)

	menu.have = 0 // заказ забрал последнее: блюдо уходит в стоп-лист
	acc := store.OrderResponse{ID: "o1", Status: store.OrderAccepted, Writeoffs: []store.WriteoffJSON{{ProductID: "p1"}}}
	_, _ = pubOrders{fakeOrders{resp: acc}, p}.PlaceOrder(context.Background(), store.OrderInput{}, nil)
	got := strings.Join(drain(sub), "\n")
	for _, want := range []string{"order.accepted", `"order_id":"o1"`, "stock.changed", `"added":["d1"]`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}

	rej := store.OrderResponse{ID: "o2", Status: store.OrderRejected, Missing: []store.MissingJSON{{ProductID: "p1"}}}
	_, _ = pubOrders{fakeOrders{resp: rej}, p}.PlaceOrder(context.Background(), store.OrderInput{}, nil)
	got = strings.Join(drain(sub), "\n")
	if !strings.Contains(got, "order.rejected") || strings.Contains(got, "stoplist.changed") {
		t.Fatalf("rejected: %s", got)
	}
}

func TestSweepPublishesBatchExpiredCapped(t *testing.T) {
	menu := &fakeMenu{have: 100}
	p, sub := newPub(menu)
	menu.have = 0
	var exp []store.ExpiredBatch
	for i := 0; i < 60; i++ {
		exp = append(exp, store.ExpiredBatch{BatchID: int64(i), ProductID: "p1"})
	}
	p.onSweep(exp)
	got := strings.Join(drain(sub), "\n")
	if !strings.Contains(got, `"count":60`) || !strings.Contains(got, `"product_ids":["p1"]`) ||
		strings.Contains(got, `,50,`) || !strings.Contains(got, "stoplist.changed") {
		t.Fatalf("%s", got)
	}
}
