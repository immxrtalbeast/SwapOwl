package domain

import (
	"strings"
	"testing"
	"time"
)

var supplyNow = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

func TestSupplyBlockingFirst(t *testing.T) {
	got := SupplySuggestions(SupplyInput{Now: supplyNow, CoverDays: 3, MinPortions: 10, Products: []SupplyProduct{
		{ID: "a", LeadDays: 1, PackSize: 1, UsableStock: 100, RatePerDay: 100, MaxRecipeNorm: 10},
		{ID: "z", LeadDays: 1, PackSize: 1, UsableStock: 300, RatePerDay: 100, MaxRecipeNorm: 10, BlocksDishes: 1},
	}})
	if len(got) != 2 || got[0].ProductID != "z" || got[0].Urgency != UrgencyNow {
		t.Fatalf("блокирующий продукт должен быть первым и now: %+v", got)
	}
	if got[1].ProductID != "a" || got[1].Quantity != 300 || got[1].Urgency != UrgencySoon { // 1 сут. не < 1
		t.Fatalf("a: %+v", got[1])
	}
}

func TestSupplyZeroRateMinStock(t *testing.T) {
	got := SupplySuggestions(SupplyInput{Now: supplyNow, CoverDays: 3, MinPortions: 10, Products: []SupplyProduct{
		{ID: "p", PackSize: 250, UsableStock: 100, MaxRecipeNorm: 80},
		{ID: "full", PackSize: 1, UsableStock: 800, MaxRecipeNorm: 80},
	}})
	if len(got) != 1 {
		t.Fatalf("строка с нулём не возвращается: %+v", got)
	}
	s := got[0]
	// 10·80 − 100 = 700 → до кратного 250 = 750
	if s.Quantity != 750 || s.StockoutAt != nil || s.Urgency != UrgencyPlanned {
		t.Fatalf("%+v", s)
	}
	if !strings.Contains(s.Explanation, "750") {
		t.Fatalf("в объяснении нет чисел: %q", s.Explanation)
	}
}

func TestSupplyQuantityStockoutUrgency(t *testing.T) {
	got := SupplySuggestions(SupplyInput{Now: supplyNow, CoverDays: 3, MinPortions: 10, Products: []SupplyProduct{
		{ID: "soon", LeadDays: 1, PackSize: 100, UsableStock: 250, RatePerDay: 100}, // 2.5 сут. → soon
		{ID: "now", LeadDays: 2, PackSize: 1, UsableStock: 100, RatePerDay: 100},    // 1 сут. < 2 → now
		{ID: "plan", LeadDays: 1, PackSize: 1, UsableStock: 1000, RatePerDay: 100},  // 10 сут. → planned, 400-1000<0 → нет строки
		{ID: "plan2", LeadDays: 1, PackSize: 1, UsableStock: 399, RatePerDay: 100},  // 3.99 < 4 → soon
	}})
	byID := map[string]SupplySuggestion{}
	for _, s := range got {
		byID[s.ProductID] = s
	}
	if _, ok := byID["plan"]; ok {
		t.Fatal("plan не должен возвращаться")
	}
	if s := byID["soon"]; s.Quantity != 200 || s.Urgency != UrgencySoon { // 400−250=150 → 200
		t.Fatalf("%+v", s)
	}
	if s := byID["now"]; s.Quantity != 400 || s.Urgency != UrgencyNow { // 100·(2+3)−100
		t.Fatalf("%+v", s)
	}
	if want := supplyNow.Add(24 * time.Hour); !byID["now"].StockoutAt.Equal(want) {
		t.Fatalf("дата обнуления %v", byID["now"].StockoutAt)
	}
	// порядок: now (1 сут.), soon (2.5), plan2 (3.99)
	if got[0].ProductID != "now" || got[1].ProductID != "soon" || got[2].ProductID != "plan2" {
		t.Fatalf("порядок: %v %v %v", got[0].ProductID, got[1].ProductID, got[2].ProductID)
	}
}

func TestSupplyTieBreakByID(t *testing.T) {
	p := func(id string) SupplyProduct {
		return SupplyProduct{ID: id, LeadDays: 1, PackSize: 1, UsableStock: 100, RatePerDay: 100}
	}
	got := SupplySuggestions(SupplyInput{Now: supplyNow, CoverDays: 3, Products: []SupplyProduct{p("b"), p("a")}})
	if got[0].ProductID != "a" {
		t.Fatalf("%+v", got)
	}
}
