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
	list listCall
}

type listCall struct {
	status string
	limit  int
	before string
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
func (f *fakeOrders) ListOrders(_ context.Context, status string, limit int, before string) ([]store.OrderResponse, int, error) {
	f.list = listCall{status, limit, before}
	if before == zeroID {
		return nil, 0, store.ErrUnknownCursor
	}
	return []store.OrderResponse{}, 1284, nil
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

const zeroID = "00000000-0000-0000-0000-000000000000"

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

func TestPostOrderSource(t *testing.T) {
	item := `"items":[{"dish_id":"soup","portions":1}]`
	for body, want := range map[string]string{
		`{"id":"` + oid + `",` + item + `}`:                "api",
		`{"id":"` + oid + `","source":"api",` + item + `}`: "api",
		`{"id":"` + oid + `","source":"pos",` + item + `}`: "pos",
	} {
		fo := &fakeOrders{resp: store.OrderResponse{ID: oid, Status: store.OrderAccepted}}
		if rec := postOrder(t, fo, body); rec.Code != http.StatusCreated || fo.in.Source != want {
			t.Fatalf("%s: %d source=%q", body, rec.Code, fo.in.Source)
		}
	}
	fo := &fakeOrders{}
	if rec := postOrder(t, fo, `{"id":"`+oid+`","source":"kiosk",`+item+`}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad source: %d", rec.Code)
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

func TestPostOrderOccurredAt(t *testing.T) {
	fo := &fakeOrders{resp: store.OrderResponse{ID: oid, Status: store.OrderAccepted}}
	body := func(occ string) string {
		return `{"id":"` + oid + `","occurred_at":"` + occ + `","items":[{"dish_id":"soup","portions":1}]}`
	}
	if rec := postOrder(t, fo, body("2026-10-10T11:30:00+03:00")); rec.Code != http.StatusCreated ||
		fo.in.OccurredAt == nil || !fo.in.OccurredAt.Equal(time.Date(2026, 10, 10, 8, 30, 0, 0, time.UTC)) {
		t.Fatalf("%d %s %+v", rec.Code, rec.Body, fo.in.OccurredAt)
	}
	fo.n = 0
	if rec := postOrder(t, fo, body("вчера")); rec.Code != http.StatusBadRequest || fo.n != 0 ||
		!strings.Contains(rec.Body.String(), "validation_error") {
		t.Fatalf("bad format: %d %s", rec.Code, rec.Body)
	}
	fo.err = &store.EventWindowError{OccurredAt: standTime.Add(-5 * time.Hour), From: standTime.Add(-2 * time.Hour), To: standTime}
	rec := postOrder(t, fo, body("2026-10-10T07:00:00Z"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "validation_error") ||
		!strings.Contains(rec.Body.String(), "correction") {
		t.Fatalf("outside window: %d %s", rec.Code, rec.Body)
	}
}

func getOrders(t *testing.T, fo *fakeOrders, query string) *httptest.ResponseRecorder {
	t.Helper()
	rt := NewRouter(Deps{Now: func() time.Time { return standTime }, Orders: fo})
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/orders"+query, nil))
	return rec
}

func TestListOrdersTotalHeaderAndPaging(t *testing.T) {
	fo := &fakeOrders{}
	rec := getOrders(t, fo, "?status=accepted&limit=20&before="+oid)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Total-Count") != "1284" {
		t.Fatalf("%d total=%q %s", rec.Code, rec.Header().Get("X-Total-Count"), rec.Body)
	}
	if fo.list != (listCall{"accepted", 20, strings.ToLower(oid)}) {
		t.Fatalf("call: %+v", fo.list)
	}
}

func TestListOrdersRejectsBadCursor(t *testing.T) {
	for _, q := range []string{"?before=17", "?before=" + zeroID + "x", "?before=" + zeroID} {
		if rec := getOrders(t, &fakeOrders{}, q); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, rec.Code)
		}
	}
}
