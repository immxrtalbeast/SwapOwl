package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

type fakeSupply struct{ data store.SupplyData }

func (f fakeSupply) SupplyData(context.Context, time.Time) (store.SupplyData, error) {
	return f.data, nil
}

func TestSupplySuggestionsEndpoint(t *testing.T) {
	now := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	data := store.SupplyData{
		Products: map[string]store.ProductRef{"salmon": {Name: "Лосось", Unit: domain.UnitG}, "salt": {Name: "Соль", Unit: domain.UnitG}},
		Input: domain.SupplyInput{Now: now, CoverDays: 3, MinPortions: 10, Products: []domain.SupplyProduct{
			{ID: "salmon", Name: "Лосось", LeadDays: 1, PackSize: 500, UsableStock: 400, RatePerDay: 1000, MaxRecipeNorm: 200, BlocksDishes: 1},
			{ID: "salt", Name: "Соль", LeadDays: 1, PackSize: 1, UsableStock: 5000, RatePerDay: 10, MaxRecipeNorm: 5},
		}},
	}
	rt := NewRouter(Deps{Supply: fakeSupply{data}, Now: func() time.Time { return now }})
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/supply-suggestions", nil))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var out []SupplyJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 { // соли хватает: строка с нулём не показывается
		t.Fatalf("rows: %s", rec.Body)
	}
	r := out[0]
	if r.ProductID != "salmon" || r.Urgency != "now" || r.Quantity.Unit != domain.UnitG || r.Quantity.Qty != 4000 ||
		r.StockoutAt == nil || r.BlocksDishes != 1 || !strings.Contains(r.Explanation, "Блокирует") {
		t.Fatalf("row: %+v", r)
	}
}

func TestSupplySuggestionsEmptyIsArray(t *testing.T) {
	rt := NewRouter(Deps{Supply: fakeSupply{}, Now: time.Now})
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/supply-suggestions", nil))
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("body %q", rec.Body)
	}
}
