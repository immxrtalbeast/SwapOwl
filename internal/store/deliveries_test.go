package store

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/clock"
	"umirhack/internal/domain"
)

var t0 = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func stockDB(t *testing.T) (*DB, *clock.Fixed) {
	t.Helper()
	db := testDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO products (id, name, unit) VALUES ('salmon','Лосось','g'),('cream','Сливки','ml')`); err != nil {
		t.Fatal(err)
	}
	return db, clock.NewFixed(t0)
}

func delivery(id string) DeliveryInput {
	return DeliveryInput{ID: id, Number: "1", CreatedAt: t0.Add(-time.Hour), Items: []DeliveryItem{
		{ProductID: "salmon", Unit: domain.UnitG, Qty: 4000, ShelfLifeHours: 72},
		{ProductID: "salmon", Unit: domain.UnitG, Qty: 1000, ShelfLifeHours: 24},
		{ProductID: "cream", Unit: domain.UnitMl, Qty: 600, ExpiresOn: "2026-10-12"},
	}}
}

func TestReceiveDeliveryCreatesBatchPerLine(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	resp, err := db.ReceiveDelivery(ctx, delivery("D1"), clk.Now, time.UTC)
	if err != nil || resp.Replayed || len(resp.Batches) != 3 {
		t.Fatalf("receive: %v %+v", err, resp)
	}
	if !resp.Batches[0].ExpiresAt.Equal(t0.Add(72*time.Hour)) || !resp.Batches[1].ExpiresAt.Equal(t0.Add(24*time.Hour)) {
		t.Fatalf("expires: %+v", resp.Batches)
	}
	if want := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC); !resp.Batches[2].ExpiresAt.Equal(want) {
		t.Fatalf("expires_on: got %v want %v", resp.Batches[2].ExpiresAt, want)
	}
	var movements int
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM movements WHERE kind='receipt'`).Scan(&movements)
	if movements != 3 {
		t.Fatalf("receipt movements: %d", movements)
	}

	// Доступно = remaining > 0 AND now < expires_at; время из часов.
	stock, err := db.ListProductStock(ctx, clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ProductStock{}
	for _, s := range stock {
		byID[s.ID] = s
	}
	if s := byID["salmon"]; s.Available.Qty != 5000 || s.BatchesCount != 2 || !s.NearestExpiresAt.Equal(t0.Add(24*time.Hour)) {
		t.Fatalf("salmon: %+v", s)
	}
	clk.Advance(30 * time.Hour) // вторая партия лосося просрочена, но ещё не списана
	stock, _ = db.ListProductStock(ctx, clk.Now())
	for _, s := range stock {
		if s.ID == "salmon" && (s.Available.Qty != 4000 || s.BatchesCount != 1) {
			t.Fatalf("salmon after 30h: %+v", s)
		}
	}
	batches, err := db.ListBatches(ctx, clk.Now(), "salmon", "")
	if err != nil || len(batches) != 2 || batches[0].Status != "expired" || batches[1].Status != "ok" || batches[0].TimeLeftSeconds >= 0 {
		t.Fatalf("batches: %v %+v", err, batches)
	}
	if ok, _ := db.ListBatches(ctx, clk.Now(), "", "ok"); len(ok) != 2 {
		t.Fatalf("status=ok: %+v", ok)
	}
}

func TestReceiveDeliveryReplayAndConflict(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	first, err := db.ReceiveDelivery(ctx, delivery("D1"), clk.Now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Hour)
	again := delivery("D1")
	again.Items[0], again.Items[2] = again.Items[2], again.Items[0] // порядок и name не важны
	again.Items[1].Name = "другое имя"
	rep, err := db.ReceiveDelivery(ctx, again, clk.Now, time.UTC)
	if err != nil || !rep.Replayed || !rep.ReceivedAt.Equal(first.ReceivedAt) || len(rep.Batches) != 3 || rep.Batches[0].ID != first.Batches[0].ID {
		t.Fatalf("replay: %v %+v", err, rep)
	}
	changed := delivery("D1")
	changed.Items[0].Qty++
	if _, err := db.ReceiveDelivery(ctx, changed, clk.Now, time.UTC); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatalf("conflict: %v", err)
	}
	var n int
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM batches`).Scan(&n)
	if n != 3 {
		t.Fatalf("batches after replay/conflict: %d", n)
	}
}

func TestConcurrentIdenticalDeliveriesCreateOneSet(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var created, replayed atomic.Int32
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := db.ReceiveDelivery(ctx, delivery("D1"), clk.Now, time.UTC)
			if err != nil {
				errs <- err
				return
			}
			if r.Replayed {
				replayed.Add(1)
			} else {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var n int
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM batches`).Scan(&n)
	if created.Load() != 1 || replayed.Load() != 9 || n != 3 {
		t.Fatalf("created=%d replayed=%d batches=%d", created.Load(), replayed.Load(), n)
	}
}

func TestExpiredOnArrivalIsWrittenOffAtReceipt(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	in := DeliveryInput{ID: "D2", Number: "2", CreatedAt: t0, Items: []DeliveryItem{
		{ProductID: "cream", Unit: domain.UnitMl, Qty: 500, ExpiresOn: "2026-10-09"}}}
	resp, err := db.ReceiveDelivery(ctx, in, clk.Now, time.UTC)
	if err != nil || len(resp.Warnings) != 1 || resp.Warnings[0].Code != WarnExpiredOnArrival || resp.Batches[0].Status != "expired" {
		t.Fatalf("expired: %v %+v", err, resp)
	}
	var at time.Time
	var kind, reason string
	_ = db.Pool.QueryRow(ctx, `SELECT kind, reason, at FROM movements WHERE kind='writeoff'`).Scan(&kind, &reason, &at)
	if reason != "expired" || !at.Equal(t0) {
		t.Fatalf("writeoff: %s %s %v", kind, reason, at)
	}
	if b, _ := db.ListBatches(ctx, clk.Now(), "", ""); len(b) != 0 {
		t.Fatalf("written-off batch listed: %+v", b)
	}
}

func TestAutoCreatedProductAndUnitMismatch(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	in := DeliveryInput{ID: "D3", Number: "3", CreatedAt: t0, Items: []DeliveryItem{
		{ProductID: "tea", Name: "Чай", Unit: domain.UnitG, Qty: 100, ShelfLifeHours: 10}}}
	if _, err := db.ReceiveDelivery(ctx, in, clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
	var auto bool
	_ = db.Pool.QueryRow(ctx, `SELECT auto_created FROM products WHERE id='tea'`).Scan(&auto)
	if !auto {
		t.Fatal("tea must be auto_created")
	}
	bad := DeliveryInput{ID: "D4", Number: "4", CreatedAt: t0, Items: []DeliveryItem{
		{ProductID: "tea", Unit: domain.UnitMl, Qty: 100, ShelfLifeHours: 10}}}
	if _, err := db.ReceiveDelivery(ctx, bad, clk.Now, time.UTC); !errors.Is(err, ErrUnitMismatch) {
		t.Fatalf("mismatch: %v", err)
	}
	var n int
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE id='D4'`).Scan(&n)
	if n != 0 {
		t.Fatal("failed delivery must leave no trace")
	}
}

func TestLockProductsSortedAndUnknown(t *testing.T) {
	db, _ := stockDB(t)
	ctx := context.Background()
	err := db.InTx(ctx, func(tx pgx.Tx) error { return LockProducts(ctx, tx, []string{"salmon", "cream", "salmon"}) })
	if err != nil {
		t.Fatal(err)
	}
	err = db.InTx(ctx, func(tx pgx.Tx) error { return LockProducts(ctx, tx, []string{"nope"}) })
	if !errors.Is(err, ErrUnknownProduct) {
		t.Fatalf("unknown: %v", err)
	}
}

// Автосоздание новых продуктов идёт в отсортированном порядке id: поставки [a,b] и [b,a]
// с ещё не существующими продуктами не должны давать deadlock.
func TestConcurrentAutoCreateOppositeOrderNoDeadlock(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	mk := func(id string, products ...string) DeliveryInput {
		in := DeliveryInput{ID: id, Number: "n", CreatedAt: t0}
		for _, p := range products {
			in.Items = append(in.Items, DeliveryItem{ProductID: p, Unit: domain.UnitG, Qty: 10, ShelfLifeHours: 5})
		}
		return in
	}
	for round := 0; round < 40; round++ {
		a, b := "a"+strconv.Itoa(round), "b"+strconv.Itoa(round)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, in := range []DeliveryInput{mk("X"+a, a, b), mk("Y"+a, b, a)} {
			wg.Add(1)
			go func() { defer wg.Done(); _, errs[i] = db.ReceiveDelivery(ctx, in, clk.Now, time.UTC) }()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
	}
}
