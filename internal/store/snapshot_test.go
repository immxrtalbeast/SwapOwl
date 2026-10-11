package store

import (
	"context"
	"math"
	"testing"
	"time"

	"umirhack/internal/domain"
)

func snapParams() SnapshotParams {
	return SnapshotParams{WindowDays: 14, PriorDays: 3,
		Schedule: domain.Schedule{Location: time.UTC, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour}}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestSnapshotAvailableBatchesMenuAndStopList(t *testing.T) {
	db, clk := orderDB(t)
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `UPDATE products SET price_minor_per_1000 = 90000 WHERE id='salmon';
		UPDATE dishes SET prior_daily = 6 WHERE id='soup'`); err != nil {
		t.Fatal(err)
	}
	receive(t, db, clk, "D1", 500, 72)
	receive(t, db, clk, "D2", 300, 1)

	snap, err := db.Snapshot(ctx, clk.Now().Add(2*time.Hour), snapParams())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Batches) != 1 || snap.Batches[0].Remaining != 500 {
		t.Fatalf("expired batch must not be available: %+v", snap.Batches)
	}
	if snap.Price["salmon"] != 90000 || snap.Products["salmon"].Unit != domain.UnitG {
		t.Fatalf("price/products: %+v %+v", snap.Price, snap.Products)
	}
	if len(snap.Dishes) != 2 || snap.Dishes[0].Name != "Соус" {
		t.Fatalf("dishes: %+v", snap.Dishes)
	}
	// У соуса нет сливок — он в стоп-листе, суп доступен.
	if !snap.Stopped["sauce"] || snap.Stopped["soup"] {
		t.Fatalf("stopped: %+v", snap.Stopped)
	}
}

func TestSnapshotDemandPriorOnlyWithoutHistory(t *testing.T) {
	db, clk := orderDB(t)
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `UPDATE dishes SET prior_daily = 6 WHERE id='soup'`); err != nil {
		t.Fatal(err)
	}
	snap, err := db.Snapshot(ctx, clk.Now(), snapParams())
	if err != nil {
		t.Fatal(err)
	}
	// Нет ни заказов, ни партий: окно пустое, λ = априор.
	if !near(snap.Lambda["soup"], 6) {
		t.Fatalf("lambda: %v", snap.Lambda)
	}
	// rate_salmon_день = 6·100 (суп) + 0·100 (соус); в час — деля на 13 рабочих часов.
	if !near(snap.RateDay["salmon"], 600) || !near(snap.RateHour["salmon"], 600.0/13) {
		t.Fatalf("rates: %v %v", snap.RateDay, snap.RateHour)
	}
}

func TestSnapshotDemandCountsAcceptedRejectedAndSkipsJumps(t *testing.T) {
	db, clk := orderDB(t)
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `UPDATE dishes SET prior_daily = 6 WHERE id='soup'`); err != nil {
		t.Fatal(err)
	}
	start := clk.Now().Add(-48 * time.Hour)
	for _, q := range []struct {
		id, status string
		at         time.Time
		portions   int
	}{
		{o1, "accepted", start.Add(time.Hour), 10},
		{o2, "rejected", start.Add(2 * time.Hour), 5},
	} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO orders (id, body_hash, status, at, response) VALUES ($1,'\x00',$2,$3,'{}')`,
			q.id, q.status, q.at); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO order_items (order_id, dish_id, portions) VALUES ($1,'soup',$2)`,
			q.id, q.portions); err != nil {
			t.Fatal(err)
		}
	}
	// Без прыжков: активные сутки = 2 (с первого заказа минус час), запрошено 15.
	snap, err := db.Snapshot(ctx, clk.Now(), snapParams())
	if err != nil {
		t.Fatal(err)
	}
	active := clk.Now().Sub(start.Add(time.Hour)).Hours() / 24
	if want := (6*3 + 15) / (3 + active); !near(snap.Lambda["soup"], want) {
		t.Fatalf("lambda %v, want %v", snap.Lambda["soup"], want)
	}

	// Прыжок на сутки внутри истории вычитается из активных суток.
	if _, err := db.Pool.Exec(ctx, `INSERT INTO clock_jumps VALUES ($1,$2)`,
		start.Add(24*time.Hour), start.Add(36*time.Hour)); err != nil {
		t.Fatal(err)
	}
	snap, err = db.Snapshot(ctx, clk.Now(), snapParams())
	if err != nil {
		t.Fatal(err)
	}
	if want := (6*3 + 15) / (3 + active - 0.5); !near(snap.Lambda["soup"], want) {
		t.Fatalf("lambda with jump %v, want %v", snap.Lambda["soup"], want)
	}
}
