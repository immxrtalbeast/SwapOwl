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

type fakeOrders struct {
	in   store.OrderInput
	resp store.OrderResponse
	err  error
	n    int
}

func (f *fakeOrders) DishRecipes(_ context.Context, ids []string) (map[string][]domain.RecipeItem, error) {
	m := map[string][]domain.RecipeItem{}
	for _, id := range ids {
		switch id {
		case "soup":
			m[id] = []domain.RecipeItem{{ProductID: "salmon", Quantity: 100}}
		case "sauce":
			m[id] = []domain.RecipeItem{{ProductID: "salmon", Quantity: 100}, {ProductID: "cream", Quantity: 50}}
		case "huge":
			m[id] = []domain.RecipeItem{{ProductID: "salmon", Quantity: 900_000_000}}
		}
	}
	return m, nil
}
func (f *fakeOrders) PlaceOrder(_ context.Context, in store.OrderInput, _ func() time.Time) (store.OrderResponse, error) {
	f.in, f.n = in, f.n+1
	return f.resp, f.err
}
func (f *fakeOrders) ListOrders(context.Context, string, int) ([]store.OrderResponse, error) {
	return []store.OrderResponse{}, nil
}
func (f *fakeOrders) ListDishes(context.Context) ([]store.DishView, error) { return nil, nil }

func postOrder(t *testing.T, fo *fakeOrders, body string) *httptest.ResponseRecorder {
	t.Helper()
	rt := NewRouter(Deps{Now: func() time.Time { return standTime }, Orders: fo})
	req := httptest.NewRequest(http.MethodPost, "/api/orders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, req)
	return rec
}

const oid = "5B0C0C3E-8A51-4D0E-9D0B-1F2A3B4C5D6E"

func TestPostOrderSumsNeedsAcrossDishes(t *testing.T) {
	fo := &fakeOrders{resp: store.OrderResponse{ID: oid, Status: store.OrderAccepted}}
	rec := postOrder(t, fo, `{"id":"`+oid+`","items":[{"dish_id":"soup","portions":2},{"dish_id":"sauce","portions":3}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	want := []domain.Need{{ProductID: "cream", Qty: 150}, {ProductID: "salmon", Qty: 500}}
	if len(fo.in.Needs) != 2 || fo.in.Needs[0] != want[0] || fo.in.Needs[1] != want[1] || fo.in.ID != strings.ToLower(oid) {
		t.Fatalf("needs: %+v", fo.in)
	}
}

func TestPostOrderRejectedIs409WithMissing(t *testing.T) {
	fo := &fakeOrders{resp: store.OrderResponse{ID: oid, Status: store.OrderRejected,
		Missing: []store.MissingJSON{{ProductID: "salmon"}}}}
	rec := postOrder(t, fo, `{"id":"`+oid+`","items":[{"dish_id":"soup","portions":1}]}`)
	var body struct {
		Error struct {
			Code    string
			Details map[string]any
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusConflict || body.Error.Code != CodeInsufficient || body.Error.Details["status"] != "rejected" || body.Error.Details["missing"] == nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestPostOrderReplayAndConflict(t *testing.T) {
	fo := &fakeOrders{resp: store.OrderResponse{ID: oid, Status: store.OrderAccepted, Replayed: true}}
	if rec := postOrder(t, fo, `{"id":"`+oid+`","items":[{"dish_id":"soup","portions":1}]}`); rec.Code != http.StatusOK {
		t.Fatalf("replay: %d", rec.Code)
	}
	fo.err = store.ErrOrderConflict
	rec := postOrder(t, fo, `{"id":"`+oid+`","items":[{"dish_id":"soup","portions":1}]}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), CodeOrderConflict) {
		t.Fatalf("conflict: %d %s", rec.Code, rec.Body)
	}
}

func TestPostOrderValidation(t *testing.T) {
	cases := map[string]string{
		"плохой id":    `{"id":"x","items":[{"dish_id":"soup","portions":1}]}`,
		"пустой заказ": `{"id":"` + oid + `","items":[]}`,
		"дубль блюда":  `{"id":"` + oid + `","items":[{"dish_id":"soup","portions":1},{"dish_id":"soup","portions":1}]}`,
		"порции 0":     `{"id":"` + oid + `","items":[{"dish_id":"soup","portions":0}]}`,
		"порции 10001": `{"id":"` + oid + `","items":[{"dish_id":"soup","portions":10001}]}`,
		"без порций":   `{"id":"` + oid + `","items":[{"dish_id":"soup"}]}`,
		"неизвестное":  `{"id":"` + oid + `","items":[{"dish_id":"nope","portions":1}]}`,
		"переполнение": `{"id":"` + oid + `","items":[{"dish_id":"huge","portions":10000}]}`,
		"лишнее поле":  `{"id":"` + oid + `","items":[{"dish_id":"soup","portions":1}],"x":1}`,
	}
	for name, body := range cases {
		fo := &fakeOrders{}
		if rec := postOrder(t, fo, body); rec.Code != http.StatusBadRequest || fo.n != 0 {
			t.Errorf("%s: %d %s (store calls %d)", name, rec.Code, rec.Body, fo.n)
		}
	}
}
