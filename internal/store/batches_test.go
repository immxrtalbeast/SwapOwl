package store

import (
	"context"
	"testing"
	"time"

	"umirhack/internal/domain"
)

// Три партии одного продукта: ok, expiring и expired; статус и фильтр идут через domain.Status.
func TestListBatchesStatusExpiringAndFilter(t *testing.T) {
	db, clk := stockDB(t)
	db.Expiry = domain.ExpiryConfig{Share: 0.2, MinHours: 12, MaxShare: 0.5}
	ctx := context.Background()
	in := DeliveryInput{ID: "D-ST", Number: "1", CreatedAt: t0.Add(-time.Hour), Items: []DeliveryItem{
		{ProductID: "salmon", Unit: domain.UnitG, Qty: 100, ShelfLifeHours: 200}, // порог 40 ч
		{ProductID: "salmon", Unit: domain.UnitG, Qty: 200, ShelfLifeHours: 77},  // порог 15,4 ч
		{ProductID: "salmon", Unit: domain.UnitG, Qty: 300, ShelfLifeHours: 24},
	}}
	if _, err := db.ReceiveDelivery(ctx, in, clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
	clk.Advance(72 * time.Hour) // осталось 128 ч / 5 ч / просрочена

	all, err := db.ListBatches(ctx, clk.Now(), "salmon", "")
	if err != nil || len(all) != 3 {
		t.Fatalf("all: %v %+v", err, all)
	}
	got := map[int64]string{}
	for _, b := range all {
		got[b.Remaining.Qty] = b.Status
	}
	if got[100] != "ok" || got[200] != "expiring" || got[300] != "expired" {
		t.Fatalf("statuses: %v", got)
	}
	for status, qty := range map[string]int64{"ok": 100, "expiring": 200, "expired": 300} {
		list, err := db.ListBatches(ctx, clk.Now(), "", status)
		if err != nil || len(list) != 1 || list[0].Remaining.Qty != qty || list[0].Status != status {
			t.Fatalf("filter %s: %v %+v", status, err, list)
		}
	}
}
