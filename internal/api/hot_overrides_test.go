package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/events"
	"umirhack/internal/hotoffers"
	"umirhack/internal/store"
)

type fakeOverrides struct {
	list []store.HotOverride
	set  store.HotOverride
	err  error
}

func (f *fakeOverrides) ListHotOverrides(context.Context) ([]store.HotOverride, error) {
	return f.list, nil
}
func (f *fakeOverrides) SetHotOverride(_ context.Context, o store.HotOverride) (store.HotOverride, error) {
	f.set = o
	return o, f.err
}
func (f *fakeOverrides) DeleteHotOverride(context.Context, string) error { return nil }

func overridesRouter(f *fakeOverrides, bus *events.Bus) *Router {
	snap := hotSnap()
	// Суп дня — на говядине со сроком через трое суток: в расчёт не попадает.
	snap.Dishes = append(snap.Dishes, domain.Dish{ID: "soup", Name: "Суп дня", Recipe: []domain.RecipeItem{{ProductID: "beef", Quantity: 10}}})
	return NewRouter(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), HotOffers: fakeSnap{snap}, HotOverrides: f, Events: bus, Now: func() time.Time { return hotNow },
		HotConfig: HotOffersConfig{DefaultAlgo: "b", Rank: hotoffers.Config{TopN: 3, WMin: 0.5, CoverHours: 12, MaxIter: 200}}})
}

// Скрытое блюдо уходит из offers, закреплённое вне расчёта — в manager_picks, закреплённое из стоп-листа — в excluded;
// у offers по-прежнему только расчёт с партиями-причинами (инвариант 6).
func TestHotOffersApplyManagerOverrides(t *testing.T) {
	_, base, _ := getHot(t, overridesRouter(&fakeOverrides{}, nil), "/api/hot-offers")
	if len(base.Offers) == 0 || base.Offers[0].DishID != "steak" {
		t.Fatalf("база: %+v", base.Offers)
	}
	f := &fakeOverrides{list: []store.HotOverride{
		{DishID: "steak", Mode: store.HotHide, SetAt: hotNow},
		{DishID: "soup", Mode: store.HotPin, Note: "предлагать к обеду", SetAt: hotNow},
		{DishID: "burger", Mode: store.HotPin, SetAt: hotNow},
	}}
	_, got, _ := getHot(t, overridesRouter(f, nil), "/api/hot-offers")
	for _, o := range got.Offers {
		if o.DishID == "steak" {
			t.Fatal("скрытое блюдо осталось в offers")
		}
		if len(o.Reasons) == 0 {
			t.Fatalf("в offers блюдо без партии-причины: %+v", o)
		}
	}
	if len(got.Hidden) != 1 || got.Hidden[0].DishID != "steak" || !got.Hidden[0].InOffers {
		t.Fatalf("hidden %+v", got.Hidden)
	}
	if len(got.ManagerPicks) != 1 || got.ManagerPicks[0].DishID != "soup" || got.ManagerPicks[0].Note != "предлагать к обеду" || got.ManagerPicks[0].DishName != "Суп дня" {
		t.Fatalf("picks %+v", got.ManagerPicks)
	}
	found := false
	for _, e := range got.Excluded {
		if e.DishID == "burger" && strings.Contains(e.Why, "стоп-листе") {
			found = true
		}
	}
	if !found {
		t.Fatalf("закреплённый бургер из стоп-листа не в excluded: %+v", got.Excluded)
	}
}

func TestHotOffersPinnedOfferIsMarked(t *testing.T) {
	f := &fakeOverrides{list: []store.HotOverride{{DishID: "steak", Mode: store.HotPin, Note: "шеф советует", SetAt: hotNow}}}
	_, got, _ := getHot(t, overridesRouter(f, nil), "/api/hot-offers")
	if len(got.Offers) == 0 || !got.Offers[0].Pinned || got.Offers[0].ManagerNote != "шеф советует" || len(got.ManagerPicks) != 0 {
		t.Fatalf("offers %+v picks %+v", got.Offers, got.ManagerPicks)
	}
}

func putOverride(rt *Router, dish, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/api/hot-offers/overrides/"+dish, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, req)
	return rec
}

func TestPutHotOverrideValidatesAndPublishes(t *testing.T) {
	bus := events.New(func() time.Time { return hotNow })
	sub := bus.Subscribe()
	defer sub.Unsubscribe()
	f := &fakeOverrides{}
	rt := overridesRouter(f, bus)
	for name, body := range map[string]string{
		"неизвестный режим": `{"mode":"top"}`,
		"длинная заметка":   `{"mode":"pin","note":"` + strings.Repeat("я", 201) + `"}`,
		"лишнее поле":       `{"mode":"pin","x":1}`,
	} {
		if rec := putOverride(rt, "soup", body); rec.Code != 400 {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if rec := putOverride(rt, "soup", `{"mode":"pin","note":"  к обеду "}`); rec.Code != 200 || f.set.Note != "к обеду" || f.set.DishID != "soup" || !f.set.SetAt.Equal(hotNow) {
		t.Fatalf("pin: %d %s %+v", rec.Code, rec.Body, f.set)
	}
	select {
	case ev := <-sub.C:
		if ev.Type != events.HotOffersChanged {
			t.Fatalf("событие %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("нет hotoffers.changed")
	}
	f.err = store.ErrHotOverrideDish
	if rec := putOverride(rt, "unicorn", `{"mode":"hide"}`); rec.Code != 404 || !strings.Contains(rec.Body.String(), CodeUnknownDish) {
		t.Fatalf("unknown: %d %s", rec.Code, rec.Body)
	}
}
