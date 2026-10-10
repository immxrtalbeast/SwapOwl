package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"umirhack/internal/clock"
	"umirhack/internal/domain"
)

const (
	o1 = "00000000-0000-4000-8000-000000000001"
	o2 = "00000000-0000-4000-8000-000000000002"
)

func orderDB(t *testing.T) (*DB, *clock.Fixed) {
	t.Helper()
	db, clk := stockDB(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO dishes (id, name) VALUES ('soup','Суп'),('sauce','Соус')`,
		`INSERT INTO recipe_items VALUES ('soup','salmon',100),('sauce','salmon',100),('sauce','cream',50)`,
	} {
		if _, err := db.Pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	return db, clk
}

func receive(t *testing.T, db *DB, clk *clock.Fixed, id string, qty int64, hours int) {
	t.Helper()
	_, err := db.ReceiveDelivery(context.Background(), DeliveryInput{ID: id, Number: "1", CreatedAt: t0,
		Items: []DeliveryItem{{ProductID: "salmon", Unit: domain.UnitG, Qty: qty, ShelfLifeHours: hours}}}, clk.Now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
}

func order(id string, portions int, needG int64) OrderInput {
	return OrderInput{ID: id, Items: []OrderItem{{DishID: "soup", Portions: portions}},
		Needs: []domain.Need{{ProductID: "salmon", Qty: domain.Qty(needG)}}}
}

func remainingSum(t *testing.T, db *DB) (sum, sold int64) {
	t.Helper()
	ctx := context.Background()
	_ = db.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(remaining),0) FROM batches`).Scan(&sum)
	_ = db.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(quantity),0) FROM movements WHERE kind='sale'`).Scan(&sold)
	return
}

func TestPlaceOrderFEFOAcrossBatches(t *testing.T) {
	db, clk := orderDB(t)
	receive(t, db, clk, "D1", 500, 72)
	receive(t, db, clk, "D2", 300, 24)
	resp, err := db.PlaceOrder(context.Background(), order(o1, 4, 400), clk.Now)
	if err != nil || resp.Status != OrderAccepted || len(resp.Writeoffs) != 2 {
		t.Fatalf("%v %+v", err, resp)
	}
	// Партия D2 (24 ч) исчерпана первой.
	if resp.Writeoffs[0].Qty.Qty != 300 || resp.Writeoffs[1].Qty.Qty != 100 || !resp.Writeoffs[0].ExpiresAt.Before(resp.Writeoffs[1].ExpiresAt) {
		t.Fatalf("writeoffs: %+v", resp.Writeoffs)
	}
	if sum, sold := remainingSum(t, db); sum != 400 || sold != 400 {
		t.Fatalf("sum=%d sold=%d", sum, sold)
	}
}

func TestPlaceOrderRejectedLeavesBatchesUntouched(t *testing.T) {
	db, clk := orderDB(t)
	receive(t, db, clk, "D1", 150, 72)
	// Суп и соус по отдельности помещаются (100 + 100 > 150 — вместе нет).
	in := OrderInput{ID: o1, Items: []OrderItem{{"soup", 1}, {"sauce", 1}},
		Needs: []domain.Need{{ProductID: "cream", Qty: 50}, {ProductID: "salmon", Qty: 200}}}
	resp, err := db.PlaceOrder(context.Background(), in, clk.Now)
	if err != nil || resp.Status != OrderRejected || len(resp.Missing) != 2 {
		t.Fatalf("%v %+v", err, resp)
	}
	if resp.Missing[1].Missing.Qty != 50 || resp.Missing[1].ProductName != "Лосось" {
		t.Fatalf("missing: %+v", resp.Missing)
	}
	if sum, sold := remainingSum(t, db); sum != 150 || sold != 0 {
		t.Fatalf("sum=%d sold=%d", sum, sold)
	}
	list, _ := db.ListOrders(context.Background(), OrderRejected, 10)
	if len(list) != 1 {
		t.Fatalf("history: %+v", list)
	}
}

func TestPlaceOrderIdempotency(t *testing.T) {
	db, clk := orderDB(t)
	receive(t, db, clk, "D1", 500, 72)
	ctx := context.Background()
	first, err := db.PlaceOrder(ctx, order(o1, 2, 200), clk.Now)
	if err != nil || first.Replayed {
		t.Fatal(err, first)
	}
	again, err := db.PlaceOrder(ctx, order(o1, 2, 200), clk.Now)
	if err != nil || !again.Replayed || again.Status != OrderAccepted || len(again.Writeoffs) != len(first.Writeoffs) {
		t.Fatalf("%v %+v", err, again)
	}
	if _, err := db.PlaceOrder(ctx, order(o1, 3, 300), clk.Now); !errors.Is(err, ErrOrderConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if sum, sold := remainingSum(t, db); sum != 300 || sold != 200 {
		t.Fatalf("sum=%d sold=%d", sum, sold)
	}
	// Отклонённый заказ при повторе остаётся отказом, даже если товар появился.
	rej, _ := db.PlaceOrder(ctx, order(o2, 10, 1000), clk.Now)
	receive(t, db, clk, "D2", 5000, 72)
	rej2, err := db.PlaceOrder(ctx, order(o2, 10, 1000), clk.Now)
	if err != nil || rej.Status != OrderRejected || rej2.Status != OrderRejected || !rej2.Replayed {
		t.Fatalf("%v %+v", err, rej2)
	}
}

func TestPlaceOrderConcurrentIdenticalPosts(t *testing.T) {
	db, clk := orderDB(t)
	receive(t, db, clk, "D1", 500, 72)
	var wg sync.WaitGroup
	var replayed int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := db.PlaceOrder(context.Background(), order(o1, 1, 100), clk.Now)
			if err != nil {
				t.Error(err)
				return
			}
			if r.Replayed {
				atomic.AddInt32(&replayed, 1)
			}
		}()
	}
	wg.Wait()
	var n int
	_ = db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM movements WHERE kind='sale'`).Scan(&n)
	if n != 1 || replayed != 9 {
		t.Fatalf("sale movements=%d replayed=%d", n, replayed)
	}
}

func TestPlaceOrderConcurrentNoOversell(t *testing.T) {
	for round := 0; round < 5; round++ {
		db, clk := orderDB(t)
		receive(t, db, clk, "D1", 400, 72)
		receive(t, db, clk, "D2", 300, 24) // 700 г = 7 порций
		var wg sync.WaitGroup
		var accepted, rejected int32
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				r, err := db.PlaceOrder(context.Background(), order(fmt.Sprintf("00000000-0000-4000-8000-%012d", i+10), 1, 100), clk.Now)
				switch {
				case err != nil:
					t.Error(err)
				case r.Status == OrderAccepted:
					atomic.AddInt32(&accepted, 1)
				default:
					atomic.AddInt32(&rejected, 1)
				}
			}(i)
		}
		wg.Wait()
		if sum, sold := remainingSum(t, db); accepted != 7 || rejected != 43 || sum != 0 || sold != 700 {
			t.Fatalf("round %d: accepted=%d rejected=%d sum=%d sold=%d", round, accepted, rejected, sum, sold)
		}
	}
}

func TestPlaceOrderStrictExpiryBoundary(t *testing.T) {
	db, clk := orderDB(t)
	receive(t, db, clk, "D1", 500, 24)
	expires := t0.Add(24 * time.Hour)
	ctx := context.Background()
	clk.Set(expires.Add(-time.Nanosecond))
	if r, err := db.PlaceOrder(ctx, order(o1, 1, 100), clk.Now); err != nil || r.Status != OrderAccepted {
		t.Fatalf("1ns before: %v %+v", err, r)
	}
	// «Дворник» не запускался: партия просрочена только по предикату времени.
	clk.Set(expires)
	r, err := db.PlaceOrder(ctx, order(o2, 1, 100), clk.Now)
	if err != nil || r.Status != OrderRejected || r.Missing[0].Available.Qty != 0 {
		t.Fatalf("at expires_at: %v %+v", err, r)
	}
}
