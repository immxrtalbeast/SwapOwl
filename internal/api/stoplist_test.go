package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

type fakeMenu struct{ snap store.MenuSnapshot }

func (f fakeMenu) MenuSnapshot(context.Context, time.Time) (store.MenuSnapshot, error) {
	return f.snap, nil
}

func testSnap() store.MenuSnapshot {
	return store.MenuSnapshot{
		Dishes: []domain.Dish{
			{ID: "soup", Name: "Суп", Recipe: []domain.RecipeItem{{ProductID: "veg", Quantity: 100}}},
			{ID: "steak", Name: "Стейк", Recipe: []domain.RecipeItem{{ProductID: "beef", Quantity: 200}, {ProductID: "veg", Quantity: 50}}},
		},
		Available: map[string]domain.Qty{"veg": 350},
		Products: map[string]store.ProductRef{
			"veg": {Name: "Овощи", Unit: domain.UnitG}, "beef": {Name: "Говядина", Unit: domain.UnitG},
		},
	}
}

func TestStopListAndServings(t *testing.T) {
	rt := NewRouter(Deps{Menu: fakeMenu{testSnap()}, Now: func() time.Time { return time.Unix(0, 0) }})

	get := func(path string) []DishAvailability {
		rec := httptest.NewRecorder()
		rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		var out []DishAvailability
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	all := get("/api/dishes")
	if len(all) != 2 || !all[0].Available || all[0].Servings != 3 || all[1].Available || all[1].Servings != 0 {
		t.Fatalf("servings: %+v", all)
	}
	stop := get("/api/stop-list")
	if len(stop) != 1 || stop[0].ID != "steak" || len(stop[0].Missing) != 1 {
		t.Fatalf("stop-list: %+v", stop)
	}
	r := stop[0].Missing[0]
	if r.ProductID != "beef" || r.ProductName != "Говядина" || r.Missing.Qty != 200 || r.Available.Qty != 0 {
		t.Fatalf("reason: %+v", r)
	}
}

func TestStopListEmptyIsArray(t *testing.T) {
	snap := testSnap()
	snap.Available["beef"] = 1000
	rt := NewRouter(Deps{Menu: fakeMenu{snap}})
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stop-list", nil))
	if got := rec.Body.String(); got != "[]\n" && got != "[]" {
		t.Fatalf("body %q", got)
	}
}
