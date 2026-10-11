package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"umirhack/internal/domain"
)

// defaultDelivery — поставка без срока: берётся срок продукта (salmon — 48 ч).
func defaultDelivery(t *testing.T, db *DB, id string, at time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `UPDATE products SET shelf_life_hours = 48 WHERE id = 'salmon'`); err != nil {
		t.Fatal(err)
	}
	in := DeliveryInput{ID: id, Number: "1", CreatedAt: t0, OccurredAt: &at, Items: []DeliveryItem{
		{ProductID: "salmon", Unit: domain.UnitG, Qty: 500}}}
	if _, err := db.ReceiveDelivery(ctx, in, func() time.Time { return at }, time.UTC); err != nil {
		t.Fatal(err)
	}
}

func expirySource(t *testing.T, db *DB, id int64) string {
	t.Helper()
	var s string
	if err := db.Pool.QueryRow(context.Background(), `SELECT expiry_source FROM batches WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDeliverySetsExpirySource(t *testing.T) {
	db, clk := stockDB(t)
	receive(t, db, clk, "D1", 100, 24) // срок в поставке
	defaultDelivery(t, db, "D2", t0)
	if a, b := expirySource(t, db, 1), expirySource(t, db, 2); a != ExpirySourceDelivery || b != ExpirySourceProductDefault {
		t.Fatalf("sources: %s %s", a, b)
	}
}

func TestConfirmExpiry(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	defaultDelivery(t, db, "D1", t0)
	movements := cnt(t, db, `SELECT count(*) FROM movements`)

	res, err := db.ConfirmExpiry(ctx, ExpiryInput{BatchID: 1, ShelfLifeHours: 96}, clk.Now, time.UTC)
	if err != nil || !res.ExpiresAt.Equal(t0.Add(96*time.Hour)) || !res.OldExpiresAt.Equal(t0.Add(48*time.Hour)) ||
		res.OldSource != ExpirySourceProductDefault || res.ExpirySource != ExpirySourceConfirmed {
		t.Fatalf("confirm: %v %+v", err, res)
	}
	if expirySource(t, db, 1) != ExpirySourceConfirmed || cnt(t, db, `SELECT count(*) FROM movements`) != movements {
		t.Fatal("source not confirmed or movement written")
	}
	// Повторное подтверждение (смена даты) допустимо; expires_on — конец дня по поясу.
	res, err = db.ConfirmExpiry(ctx, ExpiryInput{BatchID: 1, ExpiresOn: "2026-10-20"}, clk.Now, time.UTC)
	want := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	if err != nil || !res.ExpiresAt.Equal(want) {
		t.Fatalf("expires_on: %v %+v", err, res)
	}
	balanceHolds(t, db)
}

func TestConfirmExpiryRejects(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	defaultDelivery(t, db, "D1", t0)

	if _, err := db.ConfirmExpiry(ctx, ExpiryInput{BatchID: 99, ShelfLifeHours: 5}, clk.Now, time.UTC); !errors.Is(err, ErrUnknownBatch) {
		t.Fatalf("unknown: %v", err)
	}
	// Срок не позже now (вчерашняя дата, и часы от received_at, дающие прошлое).
	clk.Set(t0.Add(10 * time.Hour))
	if _, err := db.ConfirmExpiry(ctx, ExpiryInput{BatchID: 1, ExpiresOn: "2026-10-01"}, clk.Now, time.UTC); !errors.Is(err, ErrExpiryNotFuture) {
		t.Fatalf("past date: %v", err)
	}
	if _, err := db.ConfirmExpiry(ctx, ExpiryInput{BatchID: 1, ShelfLifeHours: 5}, clk.Now, time.UTC); !errors.Is(err, ErrExpiryNotFuture) {
		t.Fatalf("past hours: %v", err)
	}
	if expirySource(t, db, 1) != ExpirySourceProductDefault {
		t.Fatal("rejected call changed the source")
	}
	// Просроченная партия.
	clk.Set(t0.Add(49 * time.Hour))
	if _, err := db.ConfirmExpiry(ctx, ExpiryInput{BatchID: 1, ShelfLifeHours: 200}, clk.Now, time.UTC); !errors.Is(err, ErrBatchClosed) {
		t.Fatalf("expired: %v", err)
	}
	// Списанная в ноль.
	clk.Set(t0)
	if _, err := db.Pool.Exec(ctx, `UPDATE batches SET remaining = 0 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ConfirmExpiry(ctx, ExpiryInput{BatchID: 1, ShelfLifeHours: 200}, clk.Now, time.UTC); !errors.Is(err, ErrBatchClosed) {
		t.Fatalf("written off: %v", err)
	}
}

func TestListReceivingWindow(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	defaultDelivery(t, db, "D1", t0)
	receive(t, db, clk, "D2", 100, 24) // срок из поставки — не в списке
	clk.Set(t0.Add(47 * time.Hour))
	list, err := db.ListReceiving(ctx, clk.Now())
	if err != nil || len(list) != 1 || list[0].ID != 1 || list[0].ProductName != "Лосось" || list[0].TimeLeftSeconds != 3600 {
		t.Fatalf("list: %v %+v", err, list)
	}
	// Окно 48 ч от received_at: партия D1 принята ровно 48 ч назад — ещё в окне, но истекает в тот же миг.
	clk.Set(t0.Add(48*time.Hour + time.Second))
	if list, _ = db.ListReceiving(ctx, clk.Now()); len(list) != 0 {
		t.Fatalf("expired batch listed: %+v", list)
	}
	// Подтверждённая уходит из списка; принятая давно — тоже.
	clk.Set(t0.Add(time.Hour))
	if _, err := db.ConfirmExpiry(ctx, ExpiryInput{BatchID: 1, ShelfLifeHours: 500}, clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
	if list, _ = db.ListReceiving(ctx, clk.Now()); len(list) != 0 {
		t.Fatalf("confirmed listed: %+v", list)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE batches SET expiry_source = 'product_default' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	clk.Set(t0.Add(60 * time.Hour))
	if list, _ = db.ListReceiving(ctx, clk.Now()); len(list) != 0 {
		t.Fatalf("old batch listed: %+v", list)
	}
}
