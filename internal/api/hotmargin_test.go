package api

import (
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

func marginSnap() store.Snapshot {
	return store.Snapshot{
		Dishes: []domain.Dish{
			{ID: "steak", Name: "Стейк", Recipe: []domain.RecipeItem{{ProductID: "salmon", Quantity: 200}, {ProductID: "cream", Quantity: 50}}},
			{ID: "bare", Name: "Без цены", Recipe: []domain.RecipeItem{{ProductID: "salmon", Quantity: 100}}},
			{ID: "nocost", Name: "Продукт без цены", Recipe: []domain.RecipeItem{{ProductID: "mystery", Quantity: 100}}},
			{ID: "loss", Name: "В минус", Recipe: []domain.RecipeItem{{ProductID: "salmon", Quantity: 1000}}},
			{ID: "empty", Name: "Без рецепта"},
		},
		// Лосось 900 ₽/кг = 90000 коп. за 1000 г; сливки 300 ₽/л = 30000 коп. за 1000 мл.
		Price:     map[string]int64{"salmon": 90000, "cream": 30000},
		DishPrice: map[string]int64{"steak": 60000, "nocost": 50000, "loss": 50000, "empty": 50000},
	}
}

// Маржа = цена блюда − Σ норма × цена / 1000: 600 ₽ − (200 г × 90 + 50 мл × 30) коп. = 60000 − (18000 + 1500) = 40500.
func TestDishMargin(t *testing.T) {
	snap := marginSnap()
	if m, ok := dishMargin(snap, "steak"); !ok || m != 40500 {
		t.Fatalf("steak: %d %v", m, ok)
	}
	// Отрицательная маржа показывается как есть: 500 ₽ − 1000 г лосося (900 ₽) = −400 ₽.
	if m, ok := dishMargin(snap, "loss"); !ok || m != -40000 {
		t.Fatalf("loss: %d %v", m, ok)
	}
	for _, id := range []string{"bare", "nocost", "empty", "unknown"} {
		if m, ok := dishMargin(snap, id); ok {
			t.Errorf("%s: маржа %d при неизвестной цене", id, m)
		}
	}
}

func TestDishMarginRoundsCostDown(t *testing.T) {
	snap := store.Snapshot{
		Dishes:    []domain.Dish{{ID: "d", Recipe: []domain.RecipeItem{{ProductID: "p", Quantity: 7}}}},
		Price:     map[string]int64{"p": 999}, // 7 × 999 / 1000 = 6,993 → 6 копеек
		DishPrice: map[string]int64{"d": 100},
	}
	if m, ok := dishMargin(snap, "d"); !ok || m != 94 {
		t.Fatalf("%d %v", m, ok)
	}
}

func ptr(v int64) *int64 { return &v }

// Тай-брейк только внутри подряд идущих предложений с равным приоритетом; разные приоритеты — порядок алгоритма.
func TestTieBreakByMargin(t *testing.T) {
	in := []OfferJSON{
		{DishID: "a", Priority: 10, MarginMinor: ptr(100)},
		{DishID: "b", Priority: 10, MarginMinor: ptr(900)},
		{DishID: "c", Priority: 10}, // маржа неизвестна — в конец группы
		{DishID: "d", Priority: 8, MarginMinor: ptr(5000)},
		{DishID: "e", Priority: 5, MarginMinor: ptr(1)},
		{DishID: "f", Priority: 5, MarginMinor: ptr(1)},
	}
	out := tieBreakByMargin(append([]OfferJSON(nil), in...))
	var got []string
	for _, o := range out {
		got = append(got, o.DishID)
	}
	want := []string{"b", "a", "c", "d", "e", "f"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("порядок %v, ждали %v", got, want)
		}
	}
	// Блюдо с большей маржой, но меньшим приоритетом («d») выше не поднимается.
	if got[3] != "d" {
		t.Fatalf("приоритет перебит маржой: %v", got)
	}
}

// Равный приоритет, но срок разный: порядок по сроку опорной партии (спецификация 2.7) маржой не перебивается.
func TestTieBreakKeepsExpiryOrder(t *testing.T) {
	soon := time.Date(2026, 10, 16, 13, 0, 0, 0, time.UTC)
	later := soon.Add(2 * time.Hour)
	in := []OfferJSON{
		{DishID: "x", Priority: 50, SellUntil: soon, MarginMinor: ptr(100)},
		{DishID: "y", Priority: 50, SellUntil: later, MarginMinor: ptr(9000)},
		{DishID: "z", Priority: 50, SellUntil: later, MarginMinor: ptr(9500)},
	}
	out := tieBreakByMargin(in)
	got := []string{out[0].DishID, out[1].DishID, out[2].DishID}
	// x раньше по сроку и остаётся первым; y и z — один срок, выше маржа у z.
	if got[0] != "x" || got[1] != "z" || got[2] != "y" {
		t.Fatalf("%v", got)
	}
}

func TestTieBreakByMarginEmptyAndSingle(t *testing.T) {
	if got := tieBreakByMargin(nil); len(got) != 0 {
		t.Fatal(got)
	}
	one := []OfferJSON{{DishID: "x", Priority: 3}}
	if got := tieBreakByMargin(one); len(got) != 1 || got[0].DishID != "x" {
		t.Fatal(got)
	}
}

// В ответе горячих поле есть там, где известны цены; у блюда без цены его нет.
func TestHotOffersCarryMargin(t *testing.T) {
	snap := hotSnap()
	snap.DishPrice = map[string]int64{"steak": 120000} // Лосось 200 г × 90000/1000 = 18000 → маржа 102000
	_, resp, body := getHot(t, hotRouter(snap, "b"), "/api/hot-offers")
	if len(resp.Offers) == 0 || resp.Offers[0].DishID != "steak" {
		t.Fatalf("нет предложения: %s", body)
	}
	o := resp.Offers[0]
	if o.MarginMinor == nil || *o.MarginMinor != 102000 {
		t.Fatalf("маржа: %+v", o.MarginMinor)
	}
	if o.ExpectedMarginMinor == nil || *o.ExpectedMarginMinor != 102000*o.TargetToday {
		t.Fatalf("ожидаемая маржа: %+v при target_today=%d", o.ExpectedMarginMinor, o.TargetToday)
	}
	// Без цены блюда — полей нет.
	snap.DishPrice = nil
	_, none, _ := getHot(t, hotRouter(snap, "b"), "/api/hot-offers")
	if len(none.Offers) == 0 || none.Offers[0].MarginMinor != nil || none.Offers[0].ExpectedMarginMinor != nil {
		t.Fatalf("маржа без цены: %+v", none.Offers)
	}
}
