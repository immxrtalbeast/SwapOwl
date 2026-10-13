package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/clock"
)

const (
	c1 = "corr-1"
	c2 = "corr-2"
)

func batchRemaining(t *testing.T, db *DB, id int64) int64 {
	t.Helper()
	var r int64
	if err := db.Pool.QueryRow(context.Background(), `SELECT remaining FROM batches WHERE id = $1`, id).Scan(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

func cnt(t *testing.T, db *DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// balanceHolds — инвариант 1 с возвратами и корректировками: для каждой партии
// receipt + return − sale − writeoff − adjustment = remaining.
func balanceHolds(t *testing.T, db *DB) {
	t.Helper()
	bad := cnt(t, db, `
		SELECT count(*) FROM batches b WHERE b.remaining <> COALESCE((
			SELECT SUM(CASE WHEN kind IN ('receipt','return') THEN quantity ELSE -quantity END)
			FROM movements WHERE batch_id = b.id), 0)`)
	if bad != 0 {
		t.Fatalf("%d batches break quantity conservation", bad)
	}
}

func TestWriteOffBatch(t *testing.T) {
	db, clk := orderDB(t)
	ctx := context.Background()
	receive(t, db, clk, "D1", 500, 72)
	in := WriteoffInput{ID: c1, BatchID: 1, Qty: 120, Reason: ReasonSpoilage}

	res, err := db.WriteOffBatch(ctx, in, clk.Now)
	if err != nil || res.Replayed || res.Remaining.Qty != 380 || res.ProductID != "salmon" {
		t.Fatalf("writeoff: %v %+v", err, res)
	}
	if n := cnt(t, db, `SELECT count(*) FROM movements WHERE kind=$1 AND reason=$2 AND quantity=120`, KindWriteoff, ReasonSpoilage); n != 1 {
		t.Fatalf("movements: %d", n)
	}
	again, err := db.WriteOffBatch(ctx, in, clk.Now)
	if err != nil || !again.Replayed || again.Remaining.Qty != 380 || batchRemaining(t, db, 1) != 380 {
		t.Fatalf("replay: %v %+v remaining=%d", err, again, batchRemaining(t, db, 1))
	}
	in.Qty = 10
	if _, err := db.WriteOffBatch(ctx, in, clk.Now); !errors.Is(err, ErrCorrectionConflict) {
		t.Fatalf("other body: %v", err)
	}
	if _, err := db.WriteOffBatch(ctx, WriteoffInput{ID: c2, BatchID: 1, Qty: 381, Reason: ReasonDamage}, clk.Now); !errors.Is(err, ErrInsufficientBatch) {
		t.Fatalf("too much: %v", err)
	}
	if _, err := db.WriteOffBatch(ctx, WriteoffInput{ID: c2, BatchID: 99, Qty: 1, Reason: ReasonDamage}, clk.Now); !errors.Is(err, ErrUnknownBatch) {
		t.Fatalf("unknown batch: %v", err)
	}
	// Отказ откатывает и ключ идемпотентности: тот же id можно использовать после исправления.
	if _, err := db.WriteOffBatch(ctx, WriteoffInput{ID: c2, BatchID: 1, Qty: 380, Reason: ReasonDamage}, clk.Now); err != nil || batchRemaining(t, db, 1) != 0 {
		t.Fatalf("retry after reject: %v", err)
	}
	balanceHolds(t, db)
}

func TestWriteOffConcurrentSameID(t *testing.T) {
	db, clk := orderDB(t)
	receive(t, db, clk, "D1", 500, 72)
	var wg sync.WaitGroup
	var mu sync.Mutex
	replays := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := db.WriteOffBatch(context.Background(), WriteoffInput{ID: c1, BatchID: 1, Qty: 100, Reason: ReasonSpoilage}, clk.Now)
			if err != nil {
				t.Error(err)
				return
			}
			if res.Replayed {
				mu.Lock()
				replays++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if replays != 9 || batchRemaining(t, db, 1) != 400 {
		t.Fatalf("replays=%d remaining=%d", replays, batchRemaining(t, db, 1))
	}
	balanceHolds(t, db)
}

func TestInventoryShortageFEFO(t *testing.T) {
	db, clk := orderDB(t)
	ctx := context.Background()
	receive(t, db, clk, "D1", 300, 72) // batch 1
	receive(t, db, clk, "D2", 200, 24) // batch 2 — истекает раньше
	res, err := db.Inventory(ctx, InventoryInput{ID: c1, ProductID: "salmon", Counted: 250}, clk.Now)
	if err != nil || res.Surplus != nil || len(res.Shortage) != 2 {
		t.Fatalf("inventory: %v %+v", err, res)
	}
	if batchRemaining(t, db, 2) != 0 || batchRemaining(t, db, 1) != 250 {
		t.Fatalf("FEFO: b1=%d b2=%d", batchRemaining(t, db, 1), batchRemaining(t, db, 2))
	}
	if n := cnt(t, db, `SELECT count(*) FROM movements WHERE kind=$1 AND reason=$2`, KindAdjustment, ReasonInventoryShortage); n != 2 {
		t.Fatalf("adjustments: %d", n)
	}
	if again, err := db.Inventory(ctx, InventoryInput{ID: c1, ProductID: "salmon", Counted: 250}, clk.Now); err != nil || !again.Replayed || batchRemaining(t, db, 1) != 250 {
		t.Fatalf("replay: %v %+v", err, again)
	}
	if _, err := db.Inventory(ctx, InventoryInput{ID: c1, ProductID: "salmon", Counted: 10}, clk.Now); !errors.Is(err, ErrCorrectionConflict) {
		t.Fatalf("other body: %v", err)
	}
	balanceHolds(t, db)
}

func TestInventorySurplusMakesInvBatch(t *testing.T) {
	db, clk := orderDB(t)
	ctx := context.Background()
	receive(t, db, clk, "D1", 100, 72)
	if _, err := db.Inventory(ctx, InventoryInput{ID: c1, ProductID: "salmon", Counted: 130}, clk.Now); !errors.Is(err, ErrInventoryNoShelf) {
		t.Fatalf("no shelf life: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE products SET shelf_life_hours = 48 WHERE id = 'salmon'`); err != nil {
		t.Fatal(err)
	}
	res, err := db.Inventory(ctx, InventoryInput{ID: c1, ProductID: "salmon", Counted: 130}, clk.Now)
	if err != nil || res.Surplus == nil || res.Surplus.Quantity.Qty != 30 || len(res.Shortage) != 0 {
		t.Fatalf("surplus: %v %+v", err, res)
	}
	if !res.Surplus.ExpiresAt.Equal(clk.Now().Add(48 * time.Hour)) {
		t.Fatalf("expires: %v", res.Surplus.ExpiresAt)
	}
	if n := cnt(t, db, `SELECT count(*) FROM batches WHERE delivery_id = 'INV-corr-1' AND remaining = 30 AND quantity = 30`); n != 1 {
		t.Fatalf("INV batch: %d", n)
	}
	if n := cnt(t, db, `SELECT count(*) FROM movements WHERE kind=$1 AND reason=$2 AND quantity=30`, KindReceipt, ReasonInventorySurplus); n != 1 {
		t.Fatalf("receipt: %d", n)
	}
	// Повтор не заводит вторую партию; совпадение с учётом ничего не пишет.
	if again, err := db.Inventory(ctx, InventoryInput{ID: c1, ProductID: "salmon", Counted: 130}, clk.Now); err != nil || !again.Replayed {
		t.Fatalf("replay: %v", err)
	}
	if res, err := db.Inventory(ctx, InventoryInput{ID: c2, ProductID: "salmon", Counted: 130}, clk.Now); err != nil || res.Surplus != nil || len(res.Shortage) != 0 {
		t.Fatalf("equal: %v %+v", err, res)
	}
	if n := cnt(t, db, `SELECT count(*) FROM batches`); n != 2 {
		t.Fatalf("batches: %d", n)
	}
	if _, err := db.Inventory(ctx, InventoryInput{ID: "x", ProductID: "nope", Counted: 1}, clk.Now); !errors.Is(err, ErrUnknownProduct) {
		t.Fatalf("unknown product: %v", err)
	}
	balanceHolds(t, db)
}

func TestCancelOrderReturnsOnce(t *testing.T) {
	db, clk := orderDB(t)
	ctx := context.Background()
	receive(t, db, clk, "D1", 300, 72)
	if resp, err := db.PlaceOrder(ctx, order(o1, 2, 200), clk.Now); err != nil || resp.Status != OrderAccepted {
		t.Fatalf("order: %v %+v", err, resp)
	}
	res, err := db.CancelOrder(ctx, CancelInput{ID: c1, OrderID: o1}, clk.Now)
	if err != nil || res.Replayed || len(res.Returned) != 1 || res.Returned[0].Qty.Qty != 200 || len(res.Expired) != 0 {
		t.Fatalf("cancel: %v %+v", err, res)
	}
	if batchRemaining(t, db, 1) != 300 {
		t.Fatalf("remaining %d", batchRemaining(t, db, 1))
	}
	if n := cnt(t, db, `SELECT count(*) FROM movements WHERE kind=$1 AND order_id=$2`, KindReturn, o1); n != 1 {
		t.Fatalf("return movements: %d", n)
	}
	if list, _, err := db.ListOrders(ctx, OrderAccepted, 10, ""); err != nil || len(list) != 1 || list[0].CancelledAt == nil {
		t.Fatalf("list after cancel: %v %+v", err, list)
	}
	if again, err := db.CancelOrder(ctx, CancelInput{ID: c1, OrderID: o1}, clk.Now); err != nil || !again.Replayed || batchRemaining(t, db, 1) != 300 {
		t.Fatalf("replay: %v %+v", err, again)
	}
	if _, err := db.CancelOrder(ctx, CancelInput{ID: c2, OrderID: o1}, clk.Now); !errors.Is(err, ErrAlreadyCancelled) {
		t.Fatalf("second cancel: %v", err)
	}
	if _, err := db.CancelOrder(ctx, CancelInput{ID: c1, OrderID: o2}, clk.Now); !errors.Is(err, ErrCorrectionConflict) {
		t.Fatalf("same id other order: %v", err)
	}
	if _, err := db.CancelOrder(ctx, CancelInput{ID: "c3", OrderID: o2}, clk.Now); !errors.Is(err, ErrUnknownOrder) {
		t.Fatalf("unknown order: %v", err)
	}
	balanceHolds(t, db)
}

func TestCancelRejectedOrderNotAllowed(t *testing.T) {
	db, clk := orderDB(t)
	receive(t, db, clk, "D1", 100, 72)
	if resp, err := db.PlaceOrder(context.Background(), order(o1, 5, 500), clk.Now); err != nil || resp.Status != OrderRejected {
		t.Fatalf("order: %v %+v", err, resp)
	}
	if _, err := db.CancelOrder(context.Background(), CancelInput{ID: c1, OrderID: o1}, clk.Now); !errors.Is(err, ErrOrderNotCancellable) {
		t.Fatalf("cancel rejected: %v", err)
	}
}

func TestCancelOrderExpiredBatchStaysEmpty(t *testing.T) {
	db, clk := orderDB(t)
	ctx := context.Background()
	receive(t, db, clk, "D1", 100, 24) // batch 1, истекает через сутки
	receive(t, db, clk, "D2", 100, 72) // batch 2
	if resp, err := db.PlaceOrder(ctx, order(o1, 3, 150), clk.Now); err != nil || len(resp.Writeoffs) != 2 {
		t.Fatalf("order: %v %+v", err, resp)
	}
	later := clock.NewFixed(clk.Now().Add(25 * time.Hour)) // партия 1 просрочена, остаток 0
	res, err := db.CancelOrder(ctx, CancelInput{ID: c1, OrderID: o1}, later.Now)
	if err != nil || len(res.Returned) != 1 || res.Returned[0].BatchID != 2 || res.Returned[0].Qty.Qty != 50 ||
		len(res.Expired) != 1 || res.Expired[0].BatchID != 1 || res.Expired[0].Qty.Qty != 100 {
		t.Fatalf("cancel: %v %+v", err, res)
	}
	if batchRemaining(t, db, 1) != 0 || batchRemaining(t, db, 2) != 100 {
		t.Fatalf("remaining: b1=%d b2=%d", batchRemaining(t, db, 1), batchRemaining(t, db, 2))
	}
	if n := cnt(t, db, `SELECT count(*) FROM movements WHERE kind=$1 AND reason=$2 AND batch_id=1 AND quantity=100`, KindAdjustment, ReasonReturnExpired); n != 1 {
		t.Fatalf("return_expired: %d", n)
	}
	balanceHolds(t, db)
}

func TestResetClearsCorrections(t *testing.T) {
	db, clk := orderDB(t)
	ctx := context.Background()
	receive(t, db, clk, "D1", 100, 72)
	if _, err := db.WriteOffBatch(ctx, WriteoffInput{ID: c1, BatchID: 1, Qty: 1, Reason: ReasonDamage}, clk.Now); err != nil {
		t.Fatal(err)
	}
	if err := db.InTx(ctx, func(tx pgx.Tx) error { return DeleteStandData(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
	if n := cnt(t, db, `SELECT count(*) FROM corrections`); n != 0 {
		t.Fatalf("corrections left: %d", n)
	}
}
