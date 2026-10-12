package catalog

import (
	"testing"
	"time"
	"umirhack/internal/domain"
)

func TestCatalogConsistent(t *testing.T) {
	prods := map[string]Product{}
	for _, p := range Products() {
		if p.ID == "" || p.PriceMinorPer1000 <= 0 || p.PackSize <= 0 || p.ShelfLifeHours < 1 {
			t.Fatalf("плохой продукт: %+v", p)
		}
		if prods[p.ID].ID != "" {
			t.Fatalf("повтор продукта %s", p.ID)
		}
		prods[p.ID] = p
	}
	for _, d := range Dishes() {
		if len(d.Recipe) == 0 || d.PriceMinor <= 0 || d.PriorDaily <= 0 {
			t.Fatalf("плохое блюдо: %+v", d)
		}
		for _, it := range d.Recipe {
			if _, ok := prods[it.ProductID]; !ok || it.Quantity <= 0 {
				t.Fatalf("%s: плохая позиция рецепта %+v", d.ID, it)
			}
		}
	}
	if h := WorkingHours(); h.OpenAt >= h.CloseAt {
		t.Fatal("OpenAt должен быть раньше CloseAt")
	}
}

func TestStartingDeliveriesRelativeToT0(t *testing.T) {
	t0 := time.Date(2026, 10, 10, 11, 0, 0, 0, time.UTC)
	ds := StartingDeliveries(t0)
	if len(ds) == 0 {
		t.Fatal("нет стартовых поставок")
	}
	for _, d := range ds {
		if !d.CreatedAt.Before(t0) || len(d.Items) == 0 {
			t.Fatalf("плохая поставка %+v", d)
		}
	}
}

// Условия демо-сценария: Л-1 (2 кг, истекает через 5 ч), Л-2 (4 кг, через 3 суток), С-1 (600 мл, через 26 ч).
func TestStartingDeliveriesDemoBatches(t *testing.T) {
	t0 := time.Date(2026, 10, 10, 11, 0, 0, 0, time.UTC)
	type batch struct {
		qty     string
		expires time.Duration
	}
	got := map[string][]batch{}
	for _, d := range StartingDeliveries(t0) {
		for _, it := range d.Items {
			if DemoProductIDs()[it.ProductID] {
				exp := d.CreatedAt.Add(time.Duration(it.ShelfLifeHours) * time.Hour).Sub(t0)
				got[it.ProductID] = append(got[it.ProductID], batch{it.Quantity + it.Unit, exp})
			}
		}
	}
	h := time.Hour
	want := map[string][]batch{
		"salmon": {{"4kg", 72 * h}, {"2kg", 5 * h}},
		"cream":  {{"600ml", 26 * h}},
	}
	if len(got["salmon"]) != 2 || len(got["cream"]) != 1 {
		t.Fatalf("партии: %+v", got)
	}
	for k, w := range want {
		for i := range w {
			if got[k][i] != w[i] {
				t.Fatalf("%s[%d]: %+v, ждали %+v", k, i, got[k][i], w[i])
			}
		}
	}
}

func TestDemoDishes(t *testing.T) {
	byID := map[string]Dish{}
	free := 0
	for _, d := range Dishes() {
		byID[d.ID] = d
		hasDemo := false
		for _, it := range d.Recipe {
			hasDemo = hasDemo || DemoProductIDs()[it.ProductID]
		}
		if !hasDemo {
			free++
		}
	}
	norm := func(id, product string) domain.Qty {
		for _, it := range byID[id].Recipe {
			if it.ProductID == product {
				return it.Quantity
			}
		}
		return 0
	}
	if norm("salmon-pasta", "salmon") != 150 || norm("salmon-pasta", "cream") != 100 || norm("salmon-pasta", "pasta") != 120 ||
		norm("salmon-steak", "salmon") != 200 || norm("cream-soup", "cream") != 200 || norm("cream-soup", "potato") != 250 {
		t.Fatalf("нормы блюд не совпадают со сценарием: %+v", byID)
	}
	if free < 5 {
		t.Fatalf("блюд без лосося и сливок %d, нужно не меньше 5", free)
	}
}

func TestFormatQty(t *testing.T) {
	for _, c := range []struct {
		q    int64
		u    domain.Unit
		want string
		unit string
	}{{1500, "g", "1.5", "kg"}, {999, "g", "999", "g"}, {2000, "ml", "2", "l"}, {7, "pcs", "7", "pcs"}, {1001, "g", "1.001", "kg"}} {
		q, u := FormatQty(domain.Qty(c.q), c.u)
		if q != c.want || u != c.unit {
			t.Fatalf("%d %s: %s %s", c.q, c.u, q, u)
		}
	}
}

// Цена в разумном коридоре: 1–500 ₽ за штуку, 1–3000 ₽ за кг или литр (FS-125).
func TestProductPricesPlausible(t *testing.T) {
	for _, p := range Products() {
		rub, hi := p.PriceMinorPer1000/100, int64(3000) // ₽ за кг или л
		if p.Unit == domain.UnitPcs {
			rub, hi = p.PriceMinorPer1000/1000/100, 500 // ₽ за штуку
		}
		if rub < 1 || rub > hi {
			t.Errorf("%s: %d ₽ за единицу вне коридора 1–%d", p.ID, rub, hi)
		}
	}
}
