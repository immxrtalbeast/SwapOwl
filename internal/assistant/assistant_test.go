package assistant

import (
	"strings"
	"testing"
	"time"
)

var msk = time.FixedZone("MSK", 3*3600)

func facts() Facts {
	now := time.Date(2026, 10, 15, 11, 0, 0, 0, msk)
	exp := now.Add(7 * time.Hour)
	next := now.Add(30 * time.Hour)
	return Facts{
		Now: now, Loc: msk,
		Products: []Product{
			{ID: "salmon", Name: "Лосось", Available: Qty{1400, "g"}, Batches: 2, NearestExpiresAt: &exp},
			{ID: "cream", Name: "Сливки", Available: Qty{0, "ml"}},
			{ID: "beef", Name: "Говяжья вырезка", Available: Qty{3000, "g"}, Batches: 1, NearestExpiresAt: &next},
		},
		Alerts: []Alert{
			{ProductName: "Лосось", Remaining: Qty{600, "g"}, ExpiresAt: exp, Doomed: true},
			{ProductName: "Булочка", Remaining: Qty{4, "pcs"}, ExpiresAt: now.Add(-2 * time.Hour), Expired: true},
		},
		Dishes: []Dish{
			{Name: "Крем-суп", Available: false, Missing: []Missing{{"Сливки", Qty{50, "ml"}, Qty{0, "ml"}}}},
			{Name: "Стейк из лосося", Available: true, Servings: 7},
			{Name: "Паста с лососем", Available: true, Servings: 1},
		},
		Supply: []Supply{
			{ProductName: "Говяжья вырезка", Quantity: Qty{2000, "g"}, Urgency: "planned"},
			{ProductName: "Сливки", Quantity: Qty{600, "ml"}, Urgency: "now", BlocksDishes: 2},
		},
		Hot: []Hot{{DishName: "Стейк из лосося", Products: []string{"Лосось"}}, {DishName: "Суп дня", Note: "к обеду"}},
	}
}

func TestIntents(t *testing.T) {
	cases := []struct {
		q, intent string
		has       []string
	}{
		{"Что в стопе?", IntentStop, []string{"Крем-суп", "не хватает «сливки»", "нужно 50 мл, есть 0 мл"}},
		{"что у нас в стоп-листе", IntentStop, []string{"Крем-суп"}},
		{"Что истекает?", IntentExpiring, []string{"Лосось — 600 г, сегодня в 18:00 (не успеем продать)", "Булочка — 4 шт"}},
		{"какие продукты портятся сегодня", IntentExpiring, []string{"Лосось"}},
		{"Что заказать?", IntentOrder, []string{"Сливки — 600 мл, срочно, без него в стопе 2 блюда", "Говяжья вырезка — 2 кг"}},
		{"что надо докупить", IntentOrder, []string{"Сливки"}},
		{"Что предложить гостям?", IntentHot, []string{"Стейк из лосося — продать «лосось» сегодня", "Суп дня — к обеду"}},
		{"сколько осталось лосося?", IntentProduct, []string{"Лосось: на складе 1,4 кг (2 партии)", "сегодня в 18:00", "Истекает 600 г"}},
		{"есть ли сливки", IntentProduct, []string{"Сливки: на складе нет."}},
		{"сколько говядины", IntentProduct, []string{"Говяжья вырезка: на складе 3 кг"}},
		{"можно ли приготовить стейк из лосося", IntentDish, []string{"Стейк из лосося: можно приготовить 7 порций."}},
		{"крем-суп есть?", IntentDish, []string{"Крем-суп: в стоп-листе — не хватает «сливки»"}},
		{"что ты умеешь", IntentHelp, []string{"Что в стопе?"}},
		{"", IntentHelp, nil},
		{"расскажи анекдот", IntentUnknown, []string{"Не поняла вопрос", "Что в стопе?"}},
	}
	for _, c := range cases {
		r := Answer(c.q, facts())
		if r.Intent != c.intent {
			t.Errorf("%q: intent %s, ждали %s\n%s", c.q, r.Intent, c.intent, r.Answer)
			continue
		}
		for _, w := range c.has {
			if !strings.Contains(r.Answer, w) {
				t.Errorf("%q: нет %q в:\n%s", c.q, w, r.Answer)
			}
		}
	}
}

func TestProductVsDishByWords(t *testing.T) {
	// «лосося» — продукт; «стейк из лосося» — блюдо, хотя слово то же.
	if r := Answer("сколько лосося", facts()); r.Intent != IntentProduct {
		t.Fatalf("%v", r)
	}
	if r := Answer("стейк из лосося сколько порций", facts()); r.Intent != IntentDish {
		t.Fatalf("%v", r)
	}
}

func TestEmptyStates(t *testing.T) {
	f := Facts{Now: time.Date(2026, 10, 15, 11, 0, 0, 0, msk), Loc: msk}
	for q, want := range map[string]string{
		"что в стопе":    "Стоп-лист пуст",
		"что истекает":   "Истекающих партий нет",
		"что заказать":   "нечего",
		"что предложить": "Особых предложений нет",
	} {
		if r := Answer(q, f); !strings.Contains(r.Answer, want) {
			t.Errorf("%q: %s", q, r.Answer)
		}
	}
}

func TestDeterministicAndLimited(t *testing.T) {
	f := facts()
	for i := 0; i < 12; i++ {
		f.Dishes = append(f.Dishes, Dish{Name: "Блюдо" + string(rune('А'+i)), Available: false})
	}
	a, b := Answer("что в стопе", f), Answer("что в стопе", f)
	if a != b {
		t.Fatal("ответ зависит не только от данных")
	}
	if !strings.Contains(a.Answer, "… и ещё ") || strings.Count(a.Answer, "• ") > maxLines+1 {
		t.Fatalf("список не ограничен:\n%s", a.Answer)
	}
}

func TestFmtQty(t *testing.T) {
	for in, want := range map[Qty]string{{999, "g"}: "999 г", {1000, "g"}: "1 кг", {1450, "g"}: "1,5 кг", {2500, "ml"}: "2,5 л", {3, "pcs"}: "3 шт"} {
		if got := fmtQty(in); got != want {
			t.Errorf("%v: %s, ждали %s", in, got, want)
		}
	}
}
