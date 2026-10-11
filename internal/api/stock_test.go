package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"umirhack/internal/store"
)

type productsStock struct {
	*fakeStock
	items []store.ProductStock
}

func (p productsStock) ListProductStock(context.Context, time.Time) ([]store.ProductStock, error) {
	return p.items, nil
}

// GET /api/products отдаёт все поля контракта (.ai/solution.md, 7.4), nearest_expires_at — null без партий.
func TestListProductsContractFields(t *testing.T) {
	near := standTime.Add(5 * time.Hour)
	st := productsStock{&fakeStock{}, []store.ProductStock{
		{ID: "salmon", Name: "Лосось", Unit: "g", Available: store.QtyJSON{Qty: 5500, Unit: "g"},
			Expiring: store.QtyJSON{Qty: 1500, Unit: "g"}, BatchesCount: 2, NearestExpiresAt: &near},
		{ID: "cream", Name: "Сливки", Unit: "ml", Available: store.QtyJSON{Unit: "ml"}, Expiring: store.QtyJSON{Unit: "ml"}},
	}}
	rt := NewRouter(Deps{Now: func() time.Time { return standTime }, Stock: st})
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/products", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, row := range got {
		for _, k := range []string{"id", "name", "unit", "available", "expiring", "batches_count", "nearest_expires_at"} {
			if _, ok := row[k]; !ok {
				t.Fatalf("row %v: нет поля %q", row["id"], k)
			}
		}
	}
	if e := got[0]["expiring"].(map[string]any); e["qty"] != float64(1500) || e["unit"] != "g" {
		t.Fatalf("expiring: %v", e)
	}
	if got[1]["nearest_expires_at"] != nil {
		t.Fatalf("nearest_expires_at: %v", got[1]["nearest_expires_at"])
	}
}
