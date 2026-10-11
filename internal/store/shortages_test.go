package store

import (
	"context"
	"testing"
	"time"

	"umirhack/internal/domain"
)

func TestShortagesReport(t *testing.T) {
	db, clk := orderDB(t)
	ctx := context.Background()
	receive(t, db, clk, "D1", 150, 72)
	// Суп (200 г лосося) и соус (200 г лосося + 50 мл сливок): не хватает лосося, у соуса ещё и сливок.
	reject := func(id string, in OrderInput) {
		t.Helper()
		in.ID = id
		if resp, err := db.PlaceOrder(ctx, in, clk.Now); err != nil || resp.Status != OrderRejected {
			t.Fatalf("%v %+v", err, resp)
		}
	}
	reject(o1, order(o1, 2, 200))
	reject(o2, OrderInput{Items: []OrderItem{{"sauce", 1}},
		Needs: []domain.Need{{ProductID: "cream", Qty: 50}, {ProductID: "salmon", Qty: 300}}})
	// Принятый заказ в отчёт не попадает.
	if resp, err := db.PlaceOrder(ctx, order("00000000-0000-4000-8000-000000000003", 1, 100), clk.Now); err != nil || resp.Status != OrderAccepted {
		t.Fatalf("%v %+v", err, resp)
	}

	now := clk.Now()
	rep, err := db.ShortagesReport(ctx, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rep.RejectedOrders != 2 || len(rep.ByProduct) != 2 {
		t.Fatalf("report: %+v", rep)
	}
	// Лосось завалил оба заказа: (200−150) + (300−150); сливки — только второй: 50 − 0.
	salmon, cream := rep.ByProduct[0], rep.ByProduct[1]
	if salmon.ProductID != "salmon" || salmon.Orders != 2 || salmon.Missing.Qty != 50+150 || salmon.ProductName != "Лосось" {
		t.Fatalf("salmon: %+v", salmon)
	}
	if cream.ProductID != "cream" || cream.Orders != 1 || cream.Missing.Qty != 50 {
		t.Fatalf("cream: %+v", cream)
	}

	// Вне периода — пусто, но не null.
	empty, err := db.ShortagesReport(ctx, now.Add(time.Hour), now.Add(2*time.Hour))
	if err != nil || empty.RejectedOrders != 0 || empty.ByProduct == nil || len(empty.ByProduct) != 0 {
		t.Fatalf("empty: %v %+v", err, empty)
	}
}
