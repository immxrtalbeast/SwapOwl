package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"umirhack/internal/events"
	"umirhack/internal/store"
)

type fakeHolds struct {
	list    []store.DishHold
	set     store.DishHold
	deleted string
	err     error
}

func (f *fakeHolds) ListDishHolds(context.Context) ([]store.DishHold, error) { return f.list, nil }
func (f *fakeHolds) SetDishHold(_ context.Context, h store.DishHold) (store.DishHold, error) {
	f.set = h
	return h, f.err
}
func (f *fakeHolds) DeleteDishHold(_ context.Context, id string) error {
	f.deleted = id
	return nil
}

func holdsRouter(f *fakeHolds, bus *events.Bus) *Router {
	return NewRouter(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), DishHolds: f, Events: bus, Now: func() time.Time { return standTime }})
}

func holdDo(rt *Router, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, req)
	return rec
}

func TestDishHoldPutListDelete(t *testing.T) {
	f := &fakeHolds{list: []store.DishHold{{DishID: "soup", Note: "нет плиты", SetAt: standTime}}}
	rt := holdsRouter(f, nil)

	rec := holdDo(rt, "PUT", "/api/dish-holds/soup", `{"note":"  сломана плита  "}`)
	if rec.Code != 200 || f.set.DishID != "soup" || f.set.Note != "сломана плита" || !f.set.SetAt.Equal(standTime.UTC()) {
		t.Fatalf("PUT: %d %s %+v", rec.Code, rec.Body, f.set)
	}
	// Заметка необязательна.
	if rec := holdDo(rt, "PUT", "/api/dish-holds/soup", `{}`); rec.Code != 200 || f.set.Note != "" {
		t.Fatalf("PUT без заметки: %d %s", rec.Code, rec.Body)
	}
	rec = holdDo(rt, "GET", "/api/dish-holds", "")
	var got []store.DishHold
	if err := json.Unmarshal(rec.Body.Bytes(), &got); rec.Code != 200 || err != nil || len(got) != 1 || got[0].DishID != "soup" {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body)
	}
	if rec := holdDo(rt, "DELETE", "/api/dish-holds/soup", ""); rec.Code != 204 || f.deleted != "soup" {
		t.Fatalf("DELETE: %d", rec.Code)
	}
}

func TestDishHoldValidationAndUnknownDish(t *testing.T) {
	f := &fakeHolds{}
	rt := holdsRouter(f, nil)
	if rec := holdDo(rt, "PUT", "/api/dish-holds/soup", `{"note":"`+strings.Repeat("я", 201)+`"}`); rec.Code != 400 || !strings.Contains(rec.Body.String(), "note") {
		t.Fatalf("длинная заметка: %d %s", rec.Code, rec.Body)
	}
	if rec := holdDo(rt, "PUT", "/api/dish-holds/soup", `{"note":`); rec.Code != 400 {
		t.Fatalf("битый JSON: %d", rec.Code)
	}
	if f.set.DishID != "" {
		t.Fatalf("невалидный запрос дошёл до хранилища: %+v", f.set)
	}
	f.err = store.ErrDishHoldDish
	rec := holdDo(rt, "PUT", "/api/dish-holds/unicorn", `{}`)
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), CodeUnknownDish) {
		t.Fatalf("неизвестное блюдо: %d %s", rec.Code, rec.Body)
	}
}

// Пауза публикует stoplist.changed (открытые экраны перечитывают меню), не выдавая себя за изменение остатков.
func TestDishHoldPublishesStoplistChanged(t *testing.T) {
	bus := events.New(func() time.Time { return standTime })
	sub := bus.Subscribe()
	defer sub.Unsubscribe()
	rt := holdsRouter(&fakeHolds{}, bus)
	if rec := holdDo(rt, "PUT", "/api/dish-holds/soup", `{}`); rec.Code != 200 {
		t.Fatalf("%d", rec.Code)
	}
	select {
	case ev := <-sub.C:
		if ev.Type != events.StoplistChanged {
			t.Fatalf("тип %q", ev.Type)
		}
		var d struct {
			Added   []string `json:"added"`
			Removed []string `json:"removed"`
			Reason  string   `json:"reason"`
			DishID  string   `json:"dish_id"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		if len(d.Added) != 0 || len(d.Removed) != 0 || d.Reason != "manual_hold" || d.DishID != "soup" {
			t.Fatalf("пауза не меняет состав авто-стоп-листа: %s", ev.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("событие не пришло")
	}
}

// Заказ с блюдом на паузе — 409 dish_on_hold с перечнем блюд и заметкой, не insufficient_stock.
func TestPostOrderOnHeldDishIs409(t *testing.T) {
	fo := &fakeOrders{resp: store.OrderResponse{ID: oid, Status: store.OrderRejected,
		Held: []store.HeldJSON{{DishID: "soup", Note: "сломана плита"}}}}
	rec := postOrder(t, fo, `{"id":"`+oid+`","items":[{"dish_id":"soup","portions":1}]}`)
	var body struct {
		Error struct {
			Code    string
			Details struct {
				Dishes []store.HeldJSON
				Status string
			}
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict || body.Error.Code != CodeDishOnHold || len(body.Error.Details.Dishes) != 1 ||
		body.Error.Details.Dishes[0].Note != "сломана плита" || body.Error.Details.Status != store.OrderRejected {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

// Без DishHolds маршрутов паузы нет — стенд без миграции ведёт себя как раньше.
func TestDishHoldRoutesAbsentWithoutDeps(t *testing.T) {
	rt := NewRouter(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return standTime }})
	if rec := holdDo(rt, "GET", "/api/dish-holds", ""); rec.Code != 404 {
		t.Fatalf("%d", rec.Code)
	}
}
