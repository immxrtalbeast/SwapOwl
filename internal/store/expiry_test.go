package store

import (
	"context"
	"testing"
	"time"
)

func TestSweepExpiredWritesOffOnceAtExpiresAt(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	if _, err := db.ReceiveDelivery(ctx, delivery("D1"), clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
	// Частично продаём вторую партию лосося (1000 г, срок 24 ч), чтобы остаток был неполным.
	if _, err := db.Pool.Exec(ctx, `UPDATE batches SET remaining = 400 WHERE line_no = 2`); err != nil {
		t.Fatal(err)
	}

	got, err := db.SweepExpired(ctx, clk.Now)
	if err != nil || len(got) != 0 {
		t.Fatalf("nothing expired yet: %v %+v", err, got)
	}

	clk.Advance(30 * time.Hour)
	got, err = db.SweepExpired(ctx, clk.Now)
	if err != nil || len(got) != 1 || got[0].ProductID != "salmon" || got[0].Qty != 400 {
		t.Fatalf("sweep: %v %+v", err, got)
	}
	var kind, reason string
	var qty int64
	var at time.Time
	if err := db.Pool.QueryRow(ctx,
		`SELECT kind, reason, quantity, at FROM movements WHERE kind='writeoff' AND batch_id=$1`, got[0].BatchID).
		Scan(&kind, &reason, &qty, &at); err != nil {
		t.Fatal(err)
	}
	if reason != "expired" || qty != 400 || !at.Equal(t0.Add(24*time.Hour)) {
		t.Fatalf("movement: %s %s %d %v", kind, reason, qty, at)
	}

	// Повторный проход ничего не меняет.
	again, err := db.SweepExpired(ctx, clk.Now)
	if err != nil || len(again) != 0 {
		t.Fatalf("repeat: %v %+v", err, again)
	}
	var n int
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM movements WHERE kind='writeoff'`).Scan(&n)
	if n != 1 {
		t.Fatalf("writeoff movements: %d", n)
	}
	// Инвариант 1: поступило − списано = остаток по партиям.
	var sum, moved int64
	_ = db.Pool.QueryRow(ctx, `SELECT COALESCE(sum(remaining),0) FROM batches WHERE product_id='salmon'`).Scan(&sum)
	_ = db.Pool.QueryRow(ctx, `SELECT COALESCE(sum(CASE WHEN kind='receipt' THEN quantity ELSE -quantity END),0) FROM movements WHERE product_id='salmon'`).Scan(&moved)
	// 600 г «проданы» прямым UPDATE выше без движения: сверяем только прирост списания.
	if sum != 4000 || moved-sum != 600 {
		t.Fatalf("sum=%d moved=%d", sum, moved)
	}
}

func TestNextExpiry(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	if next, err := db.NextExpiry(ctx, clk.Now()); err != nil || next != nil {
		t.Fatalf("empty: %v %v", err, next)
	}
	if _, err := db.ReceiveDelivery(ctx, delivery("D1"), clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
	next, err := db.NextExpiry(ctx, clk.Now())
	if err != nil || next == nil || !next.Equal(t0.Add(24*time.Hour)) {
		t.Fatalf("next: %v %v", err, next)
	}
}
