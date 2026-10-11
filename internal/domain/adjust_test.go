package domain

import (
	"testing"
	"time"
)

func TestPlanInventory(t *testing.T) {
	t0 := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	bs := []Batch{
		{ID: 2, ProductID: "p", Remaining: 300, ExpiresAt: t0.Add(48 * time.Hour)},
		{ID: 1, ProductID: "p", Remaining: 100, ExpiresAt: t0.Add(24 * time.Hour)},
		{ID: 9, ProductID: "other", Remaining: 999, ExpiresAt: t0.Add(time.Hour)},
	}
	if p := PlanInventory("p", bs, 400); len(p.Shortage) != 0 || p.Surplus != 0 {
		t.Fatalf("equal: %+v", p)
	}
	if p := PlanInventory("p", bs, 450); len(p.Shortage) != 0 || p.Surplus != 50 {
		t.Fatalf("surplus: %+v", p)
	}
	p := PlanInventory("p", bs, 250) // недостача 150: FEFO — партия 1 целиком, потом 50 из партии 2
	if p.Surplus != 0 || len(p.Shortage) != 2 || p.Shortage[0] != (Writeoff{BatchID: 1, ProductID: "p", Qty: 100}) ||
		p.Shortage[1] != (Writeoff{BatchID: 2, ProductID: "p", Qty: 50}) {
		t.Fatalf("shortage: %+v", p)
	}
	if p := PlanInventory("p", bs, 0); len(p.Shortage) != 2 || p.Shortage[1].Qty != 300 {
		t.Fatalf("zero count: %+v", p)
	}
	if p := PlanInventory("p", nil, 70); p.Surplus != 70 {
		t.Fatalf("empty stock: %+v", p)
	}
}

func TestPlanCancelSplitsByExpiry(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	ret, exp := PlanCancel([]Sold{
		{BatchID: 1, ProductID: "p", Qty: 10, ExpiresAt: now.Add(time.Nanosecond)},
		{BatchID: 2, ProductID: "p", Qty: 20, ExpiresAt: now}, // граница строгая: now ≥ expires_at — просрочена
		{BatchID: 3, ProductID: "p", Qty: 0, ExpiresAt: now.Add(time.Hour)},
	}, now)
	if len(ret) != 1 || ret[0].BatchID != 1 || ret[0].Qty != 10 || len(exp) != 1 || exp[0].BatchID != 2 || exp[0].Qty != 20 {
		t.Fatalf("returns=%+v expired=%+v", ret, exp)
	}
}
