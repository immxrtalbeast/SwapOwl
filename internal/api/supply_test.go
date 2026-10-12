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

type fakeSupplyOrders struct {
	byID map[string]store.SupplyOrder
	err  error
}

func (f *fakeSupplyOrders) EnqueueSupplyOrder(_ context.Context, id string, items []store.SupplyOrderItem, now time.Time) (store.SupplyOrder, error) {
	if f.err != nil {
		return store.SupplyOrder{}, f.err
	}
	if o, ok := f.byID[id]; ok {
		o.Replayed = true
		return o, nil
	}
	o := store.SupplyOrder{ID: id, Status: store.SupplyOrderSent, CreatedAt: now, Items: items}
	f.byID[id] = o
	return o, nil
}

func (f *fakeSupplyOrders) GetSupplyOrder(_ context.Context, id string) (store.SupplyOrder, error) {
	if o, ok := f.byID[id]; ok {
		return o, nil
	}
	return store.SupplyOrder{}, store.ErrSupplyOrderNotFound
}

func postSupplyOrder(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/supply-orders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSupplyOrderEndpoint(t *testing.T) {
	f := &fakeSupplyOrders{byID: map[string]store.SupplyOrder{}}
	h := NewRouter(Deps{Now: func() time.Time { return standTime }, SupplyOrders: f}).Handler()
	const id = "5b0c0c3e-8a51-4d0e-9d0b-1f2a3b4c5d6e"
	body := `{"id":"` + id + `","items":[{"product_id":"salmon","qty":2000}]}`

	if rec := postSupplyOrder(h, body); rec.Code != 201 || !strings.Contains(rec.Body.String(), `"status":"sent"`) {
		t.Fatalf("первая отправка: %d %s", rec.Code, rec.Body)
	}
	if rec := postSupplyOrder(h, body); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"replayed":true`) {
		t.Fatalf("повтор: %d %s", rec.Code, rec.Body)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/supply-orders/"+id, nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":"`+id+`"`) {
		t.Fatalf("статус: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/supply-orders/nope", nil))
	if rec.Code != 404 {
		t.Fatalf("нет заказа: %d", rec.Code)
	}
}

func TestSupplyOrderValidationAndErrors(t *testing.T) {
	const id = "5b0c0c3e-8a51-4d0e-9d0b-1f2a3b4c5d6e"
	f := &fakeSupplyOrders{byID: map[string]store.SupplyOrder{}}
	h := NewRouter(Deps{Now: func() time.Time { return standTime }, SupplyOrders: f}).Handler()
	for name, body := range map[string]string{
		"не UUID":     `{"id":"x","items":[{"product_id":"a","qty":1}]}`,
		"нет позиций": `{"id":"` + id + `","items":[]}`,
		"нулевое qty": `{"id":"` + id + `","items":[{"product_id":"a","qty":0}]}`,
		"qty > 10^12": `{"id":"` + id + `","items":[{"product_id":"a","qty":1000000000001}]}`,
		"пустой id":   `{"id":"` + id + `","items":[{"product_id":"","qty":1}]}`,
		"дробное qty": `{"id":"` + id + `","items":[{"product_id":"a","qty":1.5}]}`,
		"лишнее поле": `{"id":"` + id + `","items":[{"product_id":"a","qty":1}],"x":1}`,
		"qty строкой": `{"id":"` + id + `","items":[{"product_id":"a","qty":"5"}]}`,
		"не JSON":     `nope`,
	} {
		if rec := postSupplyOrder(h, body); rec.Code != 400 {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	good := `{"id":"` + id + `","items":[{"product_id":"a","qty":1}]}`
	f.err = store.ErrSupplyOrderConflict
	if rec := postSupplyOrder(h, good); rec.Code != 409 || !strings.Contains(rec.Body.String(), "order_conflict") {
		t.Errorf("конфликт: %d %s", rec.Code, rec.Body)
	}
	f.err = store.ErrSupplyOrderProduct
	if rec := postSupplyOrder(h, good); rec.Code != 400 || !strings.Contains(rec.Body.String(), "unknown_product") {
		t.Errorf("продукт: %d %s", rec.Code, rec.Body)
	}
}
