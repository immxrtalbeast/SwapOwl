package domain

import (
	"reflect"
	"testing"
	"time"
)

func saleBatches() []Batch {
	t0 := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	return []Batch{
		{ID: 2, ProductID: "a", Remaining: 50, ExpiresAt: t0.Add(time.Hour)},
		{ID: 1, ProductID: "a", Remaining: 30, ExpiresAt: t0.Add(time.Hour)},
		{ID: 3, ProductID: "a", Remaining: 100, ExpiresAt: t0.Add(2 * time.Hour)},
		{ID: 4, ProductID: "b", Remaining: 10, ExpiresAt: t0.Add(time.Hour)},
	}
}

func TestPlanSaleEnoughStockNoShortfall(t *testing.T) {
	plan, short := PlanSale([]Need{{"a", 100}}, saleBatches())
	want := []Writeoff{{1, "a", 30}, {2, "a", 50}, {3, "a", 20}}
	if !reflect.DeepEqual(plan, want) || len(short) != 0 {
		t.Fatalf("plan=%v short=%v", plan, short)
	}
}

func TestPlanSaleTakesAvailableAndRecordsShortfall(t *testing.T) {
	plan, short := PlanSale([]Need{{"b", 25}, {"a", 200}, {"c", 5}}, saleBatches())
	want := []Writeoff{{1, "a", 30}, {2, "a", 50}, {3, "a", 100}, {4, "b", 10}}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("plan=%v", plan)
	}
	ws := []Shortfall{{"a", 20}, {"b", 15}, {"c", 5}}
	if !reflect.DeepEqual(short, ws) {
		t.Fatalf("short=%v", short)
	}
}

func TestPlanSaleSumsDuplicateNeedsAndIgnoresNonPositive(t *testing.T) {
	plan, short := PlanSale([]Need{{"b", 6}, {"b", 6}, {"a", 0}}, saleBatches())
	if len(plan) != 1 || plan[0].Qty != 10 || len(short) != 1 || short[0].Qty != 2 {
		t.Fatalf("plan=%v short=%v", plan, short)
	}
}

func TestPlanSaleConservesNeed(t *testing.T) {
	plan, short := PlanSale([]Need{{"a", 175}}, saleBatches())
	var sum Qty
	for _, w := range plan {
		sum += w.Qty
	}
	for _, s := range short {
		sum += s.Qty
	}
	if sum != 175 {
		t.Fatalf("sum=%d", sum)
	}
}
