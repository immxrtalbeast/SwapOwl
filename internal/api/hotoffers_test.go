package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/hotoffers"
	"umirhack/internal/store"
)

type fakeSnap struct{ snap store.Snapshot }

func (f fakeSnap) Snapshot(context.Context, time.Time) (store.Snapshot, error) { return f.snap, nil }

var hotNow = time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)

func hotSnap() store.Snapshot {
	sch := domain.Schedule{Location: time.UTC, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour}
	return store.Snapshot{
		Now: hotNow, Schedule: sch,
		Batches: []domain.Batch{
			{ID: 17, ProductID: "salmon", Quantity: 3000, Remaining: 1400, ExpiresAt: hotNow.Add(5 * time.Hour)},
			{ID: 18, ProductID: "beef", Quantity: 900, Remaining: 900, ExpiresAt: hotNow.Add(72 * time.Hour)},
		},
		Dishes: []domain.Dish{
			{ID: "steak", Name: "Стейк из лосося", Recipe: []domain.RecipeItem{{ProductID: "salmon", Quantity: 200}}},
			{ID: "burger", Name: "Бургер", Recipe: []domain.RecipeItem{{ProductID: "beef", Quantity: 150}, {ProductID: "bun", Quantity: 1}}},
		},
		Products: map[string]store.ProductRef{
			"salmon": {Name: "Лосось", Unit: domain.UnitG}, "beef": {Name: "Говядина", Unit: domain.UnitG},
			"bun": {Name: "Булка", Unit: domain.UnitPcs}},
		Price:    map[string]int64{"salmon": 90000, "beef": 60000, "bun": 1000},
		Stopped:  map[string]bool{"burger": true},
		Lambda:   map[string]float64{"steak": 13, "burger": 5},
		RateHour: map[string]float64{"salmon": 200, "beef": 57.6},
	}
}

func hotRouter(snap store.Snapshot, def string) *Router {
	return NewRouter(Deps{HotOffers: fakeSnap{snap}, Now: func() time.Time { return hotNow },
		HotConfig: HotOffersConfig{DefaultAlgo: def, Rank: hotoffers.Config{TopN: 3, WMin: 0.5, CoverHours: 12, MaxIter: 200}}})
}

func getHot(t *testing.T, rt *Router, path string) (int, HotOffersResponse, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var out HotOffersResponse
	if rec.Code == 200 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code, out, rec.Body.String()
}

func TestHotOffersBothAlgosExplainBatchWithNumbers(t *testing.T) {
	rt := hotRouter(hotSnap(), "a")
	for _, algo := range []string{"a", "b"} {
		code, out, body := getHot(t, rt, "/api/hot-offers?algo="+algo)
		if code != 200 || out.Algo != algo || len(out.Offers) != 1 {
			t.Fatalf("%s: %d %s", algo, code, body)
		}
		o := out.Offers[0]
		if o.DishID != "steak" || o.DishName != "Стейк из лосося" || len(o.Reasons) == 0 {
			t.Fatalf("%s offer: %+v", algo, o)
		}
		if o.Reasons[0].BatchID != 17 || o.Reasons[0].ProductName != "Лосось" {
			t.Fatalf("%s reason: %+v", algo, o.Reasons[0])
		}
		if !o.SellUntil.Equal(hotNow.Add(5*time.Hour)) || o.UsualPortions != 5 { // 13/13 · 5 ч
			t.Fatalf("%s sell_until/usual: %v %d", algo, o.SellUntil, o.UsualPortions)
		}
		atRisk := "1,4 кг" // R называет остаток партии, Bg — часть под риском: 1400 − 200·5 = 400 г
		if algo == "b" {
			atRisk = "400 г"
		}
		for _, want := range []string{"Лосось", "партия №17", atRisk, "5 рабочих часов"} {
			if !strings.Contains(o.Explanation, want) {
				t.Fatalf("%s explanation %q lacks %q", algo, o.Explanation, want)
			}
		}
	}
}

func TestHotOffersDefaultAlgoFromConfigAndValidation(t *testing.T) {
	_, out, _ := getHot(t, hotRouter(hotSnap(), "b"), "/api/hot-offers")
	if out.Algo != "b" {
		t.Fatalf("default algo: %q", out.Algo)
	}
	_, out, _ = getHot(t, hotRouter(hotSnap(), ""), "/api/hot-offers")
	if out.Algo != "a" {
		t.Fatalf("fallback algo: %q", out.Algo)
	}
	if code, _, _ := getHot(t, hotRouter(hotSnap(), "a"), "/api/hot-offers?algo=c"); code != 400 {
		t.Fatalf("bad algo: %d", code)
	}
}

func TestHotOffersEmptyListsAreArraysAndStoppedDishesAbsent(t *testing.T) {
	snap := hotSnap()
	snap.Batches = snap.Batches[1:] // перишабл-партии нет
	code, out, body := getHot(t, hotRouter(snap, "a"), "/api/hot-offers")
	if code != 200 || len(out.Offers) != 0 || !strings.Contains(body, `"offers":[]`) || !strings.Contains(body, `"excluded":[]`) {
		t.Fatalf("empty: %d %s", code, body)
	}
}

func TestHotOffersExcludedOnlyForBgWithHours(t *testing.T) {
	snap := hotSnap()
	snap.Stopped = map[string]bool{}
	snap.Products["bun"] = store.ProductRef{Name: "Булка", Unit: domain.UnitPcs}
	snap.Batches = append(snap.Batches, domain.Batch{ID: 19, ProductID: "bun", Quantity: 100, Remaining: 100, ExpiresAt: hotNow.Add(900 * time.Hour)})
	snap.RateHour["bun"] = 20 // 100 шт хватит на 5 рабочих часов < 12
	_, out, body := getHot(t, hotRouter(snap, "b"), "/api/hot-offers")
	if len(out.Excluded) != 1 || out.Excluded[0].DishID != "burger" || out.Excluded[0].ProductID != "bun" ||
		!strings.Contains(out.Excluded[0].Why, "5 рабочих часов") {
		t.Fatalf("bg excluded: %s", body)
	}
	if _, outA, _ := getHot(t, hotRouter(snap, "a"), "/api/hot-offers"); len(outA.Excluded) != 0 {
		t.Fatalf("a excluded: %+v", outA.Excluded)
	}
}

func TestHotOffersDeterministic(t *testing.T) {
	rt := hotRouter(hotSnap(), "b")
	_, first, _ := getHot(t, rt, "/api/hot-offers")
	for i := 0; i < 5; i++ {
		if _, again, _ := getHot(t, rt, "/api/hot-offers"); !reflect.DeepEqual(first, again) {
			t.Fatalf("run %d differs", i)
		}
	}
}

func TestHotOffersRouteAbsentWithoutSource(t *testing.T) {
	rec := httptest.NewRecorder()
	NewRouter(Deps{}).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/hot-offers", nil))
	if rec.Code != 404 {
		t.Fatalf("code %d", rec.Code)
	}
}

func TestRussianFormatting(t *testing.T) {
	for in, want := range map[int64]string{1: "1 рабочий час", 2: "2 рабочих часа", 5: "5 рабочих часов", 11: "11 рабочих часов", 21: "21 рабочий час"} {
		if got := workingHours(float64(in)); got != want {
			t.Errorf("%d: %q want %q", in, got, want)
		}
	}
	if got := workingHours(2.5); got != "2,5 рабочего часа" {
		t.Errorf("fraction: %q", got)
	}
	for _, c := range []struct {
		q    int64
		u    domain.Unit
		want string
	}{{1400, domain.UnitG, "1,4 кг"}, {350, domain.UnitG, "350 г"}, {2000, domain.UnitMl, "2 л"}, {3, domain.UnitPcs, "3 шт"}} {
		if got := formatQty(c.q, c.u); got != c.want {
			t.Errorf("%d %s: %q want %q", c.q, c.u, got, c.want)
		}
	}
	if portions(7) != "7 порций" || portions(22) != "22 порции" || portions(1) != "1 порция" {
		t.Error("portions plural")
	}
}
