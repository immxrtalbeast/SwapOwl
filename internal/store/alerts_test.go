package store

import (
	"context"
	"testing"
	"time"

	"umirhack/internal/domain"
)

func TestListAlertsExpiringThenExpiredWithLoss(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	if _, err := db.ReceiveDelivery(ctx, delivery("D1"), clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
	p := AlertParams{Expiry: testExpiry, Schedule: domain.Schedule{Location: time.UTC, OpenAt: 0, CloseAt: 24 * time.Hour}, WMin: 0.5}

	got, err := db.ListAlerts(ctx, clk.Now(), p)
	if err != nil || len(got) != 0 {
		t.Fatalf("fresh: %v %+v", err, got)
	}

	clk.Advance(13 * time.Hour) // вторая лосось (1000 г) — «истекает»
	got, err = db.ListAlerts(ctx, clk.Now(), p)
	if err != nil || len(got) != 1 || got[0].Status != "expiring" || got[0].Remaining.Qty != 1000 ||
		got[0].ProductName != "Лосось" || got[0].TimeLeftSeconds != 11*3600 || got[0].Doomed {
		t.Fatalf("expiring: %v %+v", err, got)
	}

	clk.Advance(12 * time.Hour) // 25 ч: просрочена; до прохода «дворника» остаток ещё на партии
	got, err = db.ListAlerts(ctx, clk.Now(), p)
	if err != nil || len(got) != 1 || got[0].Status != "expired" || got[0].Remaining.Qty != 1000 || !got[0].Doomed {
		t.Fatalf("expired before sweep: %v %+v", err, got)
	}
	// После прохода остаток ноль, но потерянное количество остаётся в ленте.
	if _, err := db.Sweep(ctx, clk.Now, &testExpiry); err != nil {
		t.Fatal(err)
	}
	after, err := db.ListAlerts(ctx, clk.Now(), p)
	if err != nil || len(after) != 1 || after[0].Remaining.Qty != 1000 || after[0].Status != "expired" {
		t.Fatalf("expired after sweep: %v %+v", err, after)
	}

	clk.Advance(25 * time.Hour) // 50 ч: окно 24 ч для просроченной прошло, сливки (срок 60 ч) истекают
	got, err = db.ListAlerts(ctx, clk.Now(), p)
	if err != nil || len(got) != 1 || got[0].ProductID != "cream" || got[0].Status != "expiring" {
		t.Fatalf("after window: %v %+v", err, got)
	}
}

func TestListMovementsFiltersAndLimit(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	if _, err := db.ReceiveDelivery(ctx, delivery("D1"), clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
	clk.Advance(30 * time.Hour)
	if _, err := db.SweepExpired(ctx, clk.Now); err != nil {
		t.Fatal(err)
	}
	all, err := db.ListMovements(ctx, "", "", 0)
	if err != nil || len(all) != 4 {
		t.Fatalf("all: %v %d", err, len(all))
	}
	if all[0].Kind != "writeoff" || all[0].Reason == nil || *all[0].Reason != "expired" || all[0].Quantity.Unit != domain.UnitG {
		t.Fatalf("newest first: %+v", all[0])
	}
	wo, _ := db.ListMovements(ctx, "", "writeoff", 0)
	if len(wo) != 1 {
		t.Fatalf("kind filter: %d", len(wo))
	}
	pr, _ := db.ListMovements(ctx, "cream", "", 0)
	if len(pr) != 1 || pr[0].Kind != "receipt" {
		t.Fatalf("product filter: %+v", pr)
	}
	one, _ := db.ListMovements(ctx, "", "", 1)
	if len(one) != 1 {
		t.Fatalf("limit: %d", len(one))
	}
}
