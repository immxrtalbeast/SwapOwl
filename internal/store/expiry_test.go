package store

import (
	"context"
	"testing"
	"time"

	"umirhack/internal/domain"
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

var testExpiry = domain.ExpiryConfig{Share: 0.2, MinHours: 12, MaxShare: 0.5}

func notifiedCount(t *testing.T, db *DB) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM batches WHERE expiring_notified_at IS NOT NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Сигнал «истекает» — один раз на партию: повторные проходы и рестарт (новый проход над теми же
// данными) его не повторяют. Пороги партий из delivery(): вторая лосось — через 12 ч,
// сливки — 48 ч, первая лосось — 57,6 ч.
func TestSweepNotifiesExpiringOncePerBatch(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	if _, err := db.ReceiveDelivery(ctx, delivery("D1"), clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
	res, err := db.Sweep(ctx, clk.Now, &testExpiry)
	if err != nil || len(res.Expiring) != 0 || len(res.Expired) != 0 {
		t.Fatalf("nothing yet: %v %+v", err, res)
	}

	clk.Advance(13 * time.Hour)
	res, err = db.Sweep(ctx, clk.Now, &testExpiry)
	if err != nil || len(res.Expiring) != 1 || res.Expiring[0].ProductID != "salmon" ||
		!res.Expiring[0].ExpiresAt.Equal(t0.Add(24*time.Hour)) {
		t.Fatalf("first expiring: %v %+v", err, res)
	}
	for i := 0; i < 3; i++ { // повторные проходы и «рестарт»
		res, err = db.Sweep(ctx, clk.Now, &testExpiry)
		if err != nil || len(res.Expiring) != 0 {
			t.Fatalf("repeat %d: %v %+v", i, err, res)
		}
	}

	clk.Advance(12 * time.Hour) // 25 ч: вторая лосось просрочена (о ней уже сообщали), новых «истекает» нет
	res, err = db.Sweep(ctx, clk.Now, &testExpiry)
	if err != nil || len(res.Expiring) != 0 || len(res.Expired) != 1 {
		t.Fatalf("second pass: %v %+v", err, res)
	}
	clk.Advance(24 * time.Hour) // 49 ч: сливки (срок 60 ч, порог 12 ч) перешли в «истекает»
	res, err = db.Sweep(ctx, clk.Now, &testExpiry)
	if err != nil || len(res.Expiring) != 1 || res.Expiring[0].ProductID != "cream" {
		t.Fatalf("third pass: %v %+v", err, res)
	}
	if n := notifiedCount(t, db); n != 2 {
		t.Fatalf("notified batches: %d", n)
	}
}

// Партия, ставшая просроченной минуя «истекает» (прыжок), получает только списание.
func TestSweepJumpOverExpiringSkipsSignal(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	if _, err := db.ReceiveDelivery(ctx, delivery("D1"), clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
	clk.Advance(200 * time.Hour)
	res, err := db.Sweep(ctx, clk.Now, &testExpiry)
	if err != nil || len(res.Expiring) != 0 || len(res.Expired) != 3 {
		t.Fatalf("jump: %v %+v", err, res)
	}
	if n := notifiedCount(t, db); n != 0 {
		t.Fatalf("expired-only batches must stay unnotified: %d", n)
	}
}

// Без cfg (SweepExpired) метка не ставится.
func TestSweepExpiredDoesNotMarkExpiring(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	if _, err := db.ReceiveDelivery(ctx, delivery("D1"), clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
	clk.Advance(13 * time.Hour)
	if _, err := db.SweepExpired(ctx, clk.Now); err != nil {
		t.Fatal(err)
	}
	if n := notifiedCount(t, db); n != 0 {
		t.Fatalf("notified: %d", n)
	}
}

func TestNextExpiringAt(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	if next, err := db.NextExpiringAt(ctx, clk.Now(), testExpiry); err != nil || next != nil {
		t.Fatalf("empty: %v %v", err, next)
	}
	if _, err := db.ReceiveDelivery(ctx, delivery("D1"), clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
	next, err := db.NextExpiringAt(ctx, clk.Now(), testExpiry)
	want := t0.Add(12 * time.Hour)
	if err != nil || next == nil || next.Sub(want) > time.Second || want.Sub(*next) > time.Second {
		t.Fatalf("next: %v %v want %v", err, next, want)
	}
}

func TestSweepWaitsForLateEventsWindow(t *testing.T) {
	db, clk := stockDB(t)
	db.LateEvents = 2 * time.Hour
	ctx := context.Background()
	if _, err := db.ReceiveDelivery(ctx, delivery("D1"), clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
	expires := t0.Add(24 * time.Hour) // партия лосося на 1000 г

	clk.Set(expires.Add(time.Hour)) // истекла, но окно ещё открыто
	if got, err := db.SweepExpired(ctx, clk.Now); err != nil || len(got) != 0 {
		t.Fatalf("inside window: %v %+v", err, got)
	}
	next, err := db.NextExpiry(ctx, clk.Now())
	if err != nil || next == nil || !next.Equal(expires.Add(2*time.Hour)) {
		t.Fatalf("next expiry: %v %v", err, next)
	}

	clk.Set(expires.Add(2 * time.Hour))
	got, err := db.SweepExpired(ctx, clk.Now)
	if err != nil || len(got) != 1 || got[0].Qty != 1000 {
		t.Fatalf("after window: %v %+v", err, got)
	}
	var at time.Time
	_ = db.Pool.QueryRow(ctx, `SELECT at FROM movements WHERE kind='writeoff' AND batch_id=$1`, got[0].BatchID).Scan(&at)
	if !at.Equal(expires) {
		t.Fatalf("writeoff at %v, want expires_at %v", at, expires)
	}
}
