package store

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func countSupplyOrders(t *testing.T, db *DB) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox WHERE kind = $1`, OutboxSupplyOrder).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSupplyOrderEnqueueIdempotentAndStatus(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO products (id, name, unit, price_minor_per_1000, lead_days, pack_size, auto_created)
		VALUES ('salmon','Лосось','g',100,1,1,false), ('cream','Сливки','ml',100,1,1,false)`); err != nil {
		t.Fatal(err)
	}
	items := []SupplyOrderItem{{ProductID: "salmon", Qty: 2000}, {ProductID: "cream", Qty: 500}, {ProductID: "salmon", Qty: 1000}}
	o, err := db.EnqueueSupplyOrder(ctx, "o-1", items, t0)
	if err != nil || o.Replayed || o.Status != SupplyOrderSent || len(o.Items) != 2 || o.Items[1].ProductID != "salmon" || o.Items[1].Qty != 3000 || o.Items[1].Unit != "g" {
		t.Fatalf("первая отправка: %v %+v", err, o)
	}
	// Повтор (в другом порядке) — тот же заказ, второй строки outbox нет.
	again, err := db.EnqueueSupplyOrder(ctx, "o-1", []SupplyOrderItem{{ProductID: "salmon", Qty: 3000}, {ProductID: "cream", Qty: 500}}, t0.Add(1))
	if err != nil || !again.Replayed || countSupplyOrders(t, db) != 1 {
		t.Fatalf("повтор: %v %+v n=%d", err, again, countSupplyOrders(t, db))
	}
	if _, err := db.EnqueueSupplyOrder(ctx, "o-1", []SupplyOrderItem{{ProductID: "salmon", Qty: 1}}, t0); !errors.Is(err, ErrSupplyOrderConflict) {
		t.Fatalf("другой состав: %v", err)
	}
	if _, err := db.EnqueueSupplyOrder(ctx, "o-2", []SupplyOrderItem{{ProductID: "nope", Qty: 1}}, t0); !errors.Is(err, ErrSupplyOrderProduct) || countSupplyOrders(t, db) != 1 {
		t.Fatalf("неизвестный продукт: %v", err)
	}

	// Статус следует за подтверждениями outbox: sent → error → delivered.
	entries, _ := db.ListOutbox(ctx, 0, 0)
	id := entries[0].ID
	if err := db.AckOutbox(ctx, id, false, "iiko down", t0); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetSupplyOrder(ctx, "o-1"); got.Status != SupplyOrderError || got.LastError == nil || *got.LastError != "iiko down" {
		t.Fatalf("после ошибки: %+v", got)
	}
	if err := db.AckOutbox(ctx, id, true, "", t0); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetSupplyOrder(ctx, "o-1"); got.Status != SupplyOrderDelivered {
		t.Fatalf("после доставки: %+v", got)
	}
	if _, err := db.GetSupplyOrder(ctx, "missing"); !errors.Is(err, ErrSupplyOrderNotFound) {
		t.Fatalf("нет заказа: %v", err)
	}
}

// Одновременные одинаковые отправки дают одну строку outbox.
func TestSupplyOrderConcurrentSameID(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO products (id, name, unit, price_minor_per_1000, lead_days, pack_size, auto_created)
		VALUES ('salmon','Лосось','g',100,1,1,false)`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := db.EnqueueSupplyOrder(ctx, "o-x", []SupplyOrderItem{{ProductID: "salmon", Qty: 10}}, t0); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := countSupplyOrders(t, db); n != 1 {
		t.Fatalf("строк outbox: %d, ждали 1", n)
	}
}
