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
	list, total, _ := db.ListOrders(context.Background(), OrderRejected, 10, "")
	if len(list) != 1 || total != 1 {
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

func TestPlaceOrderPOSWritesOffAvailableAndRecordsShortfall(t *testing.T) {
	db, clk := orderDB(t)
	receive(t, db, clk, "D1", 300, 72)
	in := order(o1, 5, 500)
	in.Source = SourcePOS
	resp, err := db.PlaceOrder(context.Background(), in, clk.Now)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != OrderAccepted || resp.Source != SourcePOS || len(resp.Writeoffs) != 1 ||
		len(resp.Shortfalls) != 1 || resp.Shortfalls[0].Quantity.Qty != 200 {
		t.Fatalf("%+v", resp)
	}
	rem, sold := remainingSum(t, db)
	var short int64
	_ = db.Pool.QueryRow(context.Background(), `SELECT COALESCE(SUM(quantity),0) FROM shortfalls WHERE order_id = $1`, o1).Scan(&short)
	if rem != 0 || sold != 300 || short != 200 {
		t.Fatalf("remaining=%d sold=%d shortfall=%d", rem, sold, short)
	}
	again, err := db.PlaceOrder(context.Background(), in, clk.Now)
	if err != nil || !again.Replayed || len(again.Shortfalls) != 1 {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, sold2 := remainingSum(t, db); sold2 != 300 {
		t.Fatalf("replay sold again: %d", sold2)
	}
	api := in
	api.Source = SourceAPI
	if _, err := db.PlaceOrder(context.Background(), api, clk.Now); !errors.Is(err, ErrOrderConflict) {
		t.Fatalf("same id other source: %v", err)
	}
}

func TestPlaceOrderPOSWithoutStockIsAcceptedWithShortfallOnly(t *testing.T) {
	db, clk := orderDB(t)
	in := order(o2, 1, 100)
	in.Source = SourcePOS
	resp, err := db.PlaceOrder(context.Background(), in, clk.Now)
	if err != nil || resp.Status != OrderAccepted || len(resp.Writeoffs) != 0 || len(resp.Shortfalls) != 1 {
		t.Fatalf("%+v %v", resp, err)
	}
}

func occ(t time.Time) *time.Time { return &t }

func TestLateSaleUsesExpiredButUnsweptBatch(t *testing.T) {
	db, clk := orderDB(t)
	db.LateEvents = 2 * time.Hour
	ctx := context.Background()
	receive(t, db, clk, "D1", 500, 24)
	expires := t0.Add(24 * time.Hour)
	clk.Set(expires.Add(time.Hour)) // партия истекла час назад, окно ещё открыто

	// occurred_at < expires_at — партию берёт; orders.at и movements.at = occurred_at.
	in := order(o1, 1, 100)
	in.OccurredAt = occ(expires.Add(-30 * time.Minute))
	resp, err := db.PlaceOrder(ctx, in, clk.Now)
	if err != nil || resp.Status != OrderAccepted || len(resp.Writeoffs) != 1 || !resp.At.Equal(*in.OccurredAt) {
		t.Fatalf("late sale: %v %+v", err, resp)
	}
	var at time.Time
	_ = db.Pool.QueryRow(ctx, `SELECT at FROM movements WHERE kind='sale' AND order_id=$1`, o1).Scan(&at)
	if !at.Equal(*in.OccurredAt) {
		t.Fatalf("movement at %v", at)
	}

	// occurred_at ≥ expires_at — не берёт; api-заказ отклонён.
	in2 := order(o2, 1, 100)
	in2.OccurredAt = occ(expires)
	resp, err = db.PlaceOrder(ctx, in2, clk.Now)
	if err != nil || resp.Status != OrderRejected {
		t.Fatalf("at expires_at: %v %+v", err, resp)
	}

	// Продажа без occurred_at по-прежнему не видит истёкшую партию.
	in3 := order("00000000-0000-4000-8000-000000000003", 1, 100)
	if resp, err = db.PlaceOrder(ctx, in3, clk.Now); err != nil || resp.Status != OrderRejected {
		t.Fatalf("now sale: %v %+v", err, resp)
	}
}

func TestLateSaleIgnoresBatchNotYetReceived(t *testing.T) {
	db, clk := orderDB(t)
	db.LateEvents = 2 * time.Hour
	ctx := context.Background()
	clk.Set(t0.Add(time.Hour))
	receive(t, db, clk, "D1", 500, 24) // received_at = t0+1h
	in := order(o1, 1, 100)
	in.OccurredAt = occ(t0.Add(30 * time.Minute)) // продажа до прихода партии
	resp, err := db.PlaceOrder(ctx, in, clk.Now)
	if err != nil || resp.Status != OrderRejected {
		t.Fatalf("sale before receipt: %v %+v", err, resp)
	}
	in = order(o2, 1, 100)
	in.Source, in.OccurredAt = SourcePOS, occ(t0.Add(30*time.Minute))
	resp, err = db.PlaceOrder(ctx, in, clk.Now)
	if err != nil || resp.Status != OrderAccepted || len(resp.Writeoffs) != 0 || len(resp.Shortfalls) != 1 {
		t.Fatalf("pos before receipt: %v %+v", err, resp)
	}
}

func TestOccurredAtOutsideWindow(t *testing.T) {
	db, clk := orderDB(t)
	db.LateEvents = 2 * time.Hour
	ctx := context.Background()
	receive(t, db, clk, "D1", 500, 24)
	clk.Set(t0.Add(2 * time.Hour)) // нижняя граница окна = момент прихода партии
	for name, at := range map[string]time.Time{
		"future":   clk.Now().Add(time.Second),
		"too late": clk.Now().Add(-2*time.Hour - time.Second),
	} {
		in := order(o1, 1, 100)
		in.OccurredAt = occ(at)
		if _, err := db.PlaceOrder(ctx, in, clk.Now); !errors.Is(err, ErrEventOutsideWindow) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// Границы окна включены; отказ ничего не записал, тот же id принимается.
	in0 := order(o1, 1, 100)
	in0.OccurredAt = occ(clk.Now())
	if resp, err := db.PlaceOrder(ctx, in0, clk.Now); err != nil || resp.Status != OrderAccepted {
		t.Fatalf("upper boundary: %v %+v", err, resp)
	}
	in := order(o2, 1, 100)
	in.OccurredAt = occ(clk.Now().Add(-2 * time.Hour))
	if resp, err := db.PlaceOrder(ctx, in, clk.Now); err != nil || resp.Status != OrderAccepted {
		t.Fatalf("lower boundary: %v %+v", err, resp)
	}
}

func TestOccurredAtIsPartOfOrderHash(t *testing.T) {
	a, b := order(o1, 1, 100), order(o1, 1, 100)
	b.OccurredAt = occ(t0)
	if string(a.BodyHash()) == string(b.BodyHash()) {
		t.Fatal("occurred_at must change the hash")
	}
	c := order(o1, 1, 100)
	c.OccurredAt = occ(t0.In(time.FixedZone("x", 3*3600)))
	if string(b.BodyHash()) != string(c.BodyHash()) {
		t.Fatal("same instant in another zone must hash equal")
	}
}

func TestListOrdersTotalPagingAndProductNames(t *testing.T) {
	db, clk := orderDB(t)
	ctx := context.Background()
	receive(t, db, clk, "D1", 500, 72)
	ids := []string{o1, o2, "00000000-0000-4000-8000-000000000003"}
	for _, id := range ids {
		if r, err := db.PlaceOrder(ctx, order(id, 1, 100), clk.Now); err != nil || r.Status != OrderAccepted {
			t.Fatalf("%v %+v", err, r)
		}
		clk.Advance(time.Minute)
	}
	if r, err := db.PlaceOrder(ctx, order("00000000-0000-4000-8000-000000000009", 9, 900), clk.Now); err != nil || r.Status != OrderRejected {
		t.Fatalf("%v %+v", err, r)
	}
	page1, total, err := db.ListOrders(ctx, OrderAccepted, 2, "")
	if err != nil || total != 3 || len(page1) != 2 || page1[0].ID != ids[2] {
		t.Fatalf("page1: %v total=%d %+v", err, total, page1)
	}
	if w := page1[0].Writeoffs; len(w) != 1 || w[0].ProductName != "Лосось" || w[0].BatchID == 0 {
		t.Fatalf("writeoffs: %+v", w)
	}
	page2, total, err := db.ListOrders(ctx, OrderAccepted, 2, page1[1].ID)
	if err != nil || total != 3 || len(page2) != 1 || page2[0].ID != ids[0] {
		t.Fatalf("page2: %v total=%d %+v", err, total, page2)
	}
	if _, _, err := db.ListOrders(ctx, "", 2, "00000000-0000-4000-8000-0000000000ff"); !errors.Is(err, ErrUnknownCursor) {
		t.Fatalf("unknown cursor: %v", err)
	}
	if _, total, _ := db.ListOrders(ctx, "", 1, ""); total != 4 {
		t.Fatalf("total without status: %d", total)
	}
}
