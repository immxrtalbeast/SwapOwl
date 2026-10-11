package simulator

import (
	"reflect"
	"testing"
	"time"

	"umirhack/internal/domain"
)

func TestBenchDeterministic(t *testing.T) {
	a, err := Bench(1, 2, 2, []float64{1.5, 1.2})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Bench(1, 2, 2, []float64{1.5, 1.2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("один seed дал разные результаты")
	}
	mains := 0
	for _, c := range a.Cells {
		if c.Main {
			mains++
			if c.Model != ModelM2 || c.U != 1.5 {
				t.Fatalf("основная точка — M2, u=1.5: %+v", c)
			}
		}
	}
	if mains != len(Scenarios()) || len(a.Cells) != len(Scenarios())*2*2 {
		t.Fatalf("клеток %d, основных %d", len(a.Cells), mains)
	}
}

func TestRunInvariants(t *testing.T) {
	p := Params{Seed: 3, Days: 3, U: 1.5, Cfg: DefaultConfig()}
	ev, err := Events(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range Algos {
		r, err := Run(p, a, ev)
		if err != nil {
			t.Fatal(err)
		}
		if r.Orders == 0 || r.ReceivedCost <= 0 || r.WriteoffCost < 0 || r.WriteoffCost > r.ReceivedCost {
			t.Fatalf("%s: неверный итог %+v", a, r)
		}
		if a == AlgoC0 && r.HotLines != 0 {
			t.Fatalf("C0 не должен замещать блюда: %+v", r)
		}
	}
}

func TestWarehouseFEFOAndExpiry(t *testing.T) {
	t0 := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	w := &warehouse{}
	w.receive("x", 100, t0, 10*time.Hour) // ID 1, позже
	w.receive("x", 100, t0, 5*time.Hour)  // ID 2, раньше
	if w.consume(map[string]domain.Qty{"x": 300}, t0) {
		t.Fatal("нехватка должна отказать целиком")
	}
	if w.stock(t0)["x"] != 200 {
		t.Fatal("отказ не должен менять склад")
	}
	if !w.consume(map[string]domain.Qty{"x": 150}, t0) {
		t.Fatal("заказ должен пройти")
	}
	for _, b := range w.batches {
		if b.ID == 2 && b.Remaining != 0 || b.ID == 1 && b.Remaining != 50 {
			t.Fatalf("FEFO нарушен: %+v", w.batches)
		}
	}
	var lost domain.Qty
	w.expire(t0.Add(10*time.Hour), func(b domain.Batch) { lost += b.Remaining })
	if lost != 50 || w.stock(t0.Add(10 * time.Hour))["x"] != 0 {
		t.Fatalf("просрочка: lost=%d", lost)
	}
}

func TestScenariosAndControls(t *testing.T) {
	scs := Scenarios()
	if len(scs) != 5 {
		t.Fatalf("сценариев %d, нужно 5", len(scs))
	}
	for _, sc := range scs {
		// Рисковый продукт — поставка больше суточной потребности; блюд с ним больше k = 3.
		risky := map[string]bool{}
		for _, p := range sc.World.Products {
			if p.ShelfLifeHours <= 72 {
				risky[p.ID] = true
			}
		}
		n := 0
		for _, d := range sc.World.Dishes {
			for _, it := range d.Recipe {
				if risky[it.ProductID] {
					n++
					break
				}
			}
		}
		if n <= DefaultConfig().TopN {
			t.Fatalf("%s: блюд с рисковыми ингредиентами %d, нужно больше %d", sc.Name, n, DefaultConfig().TopN)
		}
		t0, _, _ := Start()
		ev, err := sc.World.Events(1, t0, 3)
		if err != nil || len(ev) == 0 {
			t.Fatalf("%s: %v, событий %d", sc.Name, err, len(ev))
		}
		for _, m := range []Model{ModelM1, ModelM2} {
			p := Params{Seed: 1, Days: 3, U: 1.5, Model: m, Cfg: DefaultConfig()}
			var c0 Result
			for _, a := range Algos {
				r, err := RunWorld(sc.World, p, a, ev)
				if err != nil {
					t.Fatal(err)
				}
				if a == AlgoC0 {
					c0 = r
					if r.HotLines != 0 {
						t.Fatalf("C0 без рекомендаций: %+v", r)
					}
					continue
				}
				if r.HotLines == 0 {
					t.Fatalf("%s %s %s: рекомендации не влияют на спрос", sc.Name, m, a)
				}
				if r.Orders != c0.Orders && m == ModelM2 {
					t.Fatalf("M2: число заказов должно совпадать с C0: %+v %+v", r, c0)
				}
			}
		}
	}
}

func TestControlsSkipStopListAndAreDeterministic(t *testing.T) {
	dishes := []domain.Dish{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}}
	stopped := map[string]bool{"a": true}
	r1 := randomOffers(7, 3, dishes, stopped, 3)
	r2 := randomOffers(7, 3, dishes, stopped, 3)
	if !reflect.DeepEqual(r1, r2) || len(r1) != 3 {
		t.Fatalf("RND недетерминирован или не ровно k: %v %v", r1, r2)
	}
	seen := map[string]bool{}
	for _, o := range r1 {
		if o.DishID == "a" || seen[o.DishID] {
			t.Fatalf("RND: стоп-лист или повтор: %v", r1)
		}
		seen[o.DishID] = true
	}
	pop := popularOffers(map[string]float64{"a": 99, "b": 1, "c": 5, "d": 5, "e": 3}, dishes, stopped, 3)
	if pop[0].DishID != "c" || pop[1].DishID != "d" || pop[2].DishID != "e" {
		t.Fatalf("POP: %v", pop)
	}
	t0 := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	recipes := map[string]map[string]domain.Qty{"a": {"x": 1}, "b": {"x": 1}, "c": {"y": 1}, "d": {"z": 1}}
	batches := []domain.Batch{
		{ProductID: "x", ExpiresAt: t0.Add(1 * time.Hour)},
		{ProductID: "y", ExpiresAt: t0.Add(5 * time.Hour)},
		{ProductID: "z", ExpiresAt: t0.Add(3 * time.Hour)},
	}
	c1 := nearestOffers(t0, batches, dishes, recipes, stopped, 3)
	if len(c1) != 3 || c1[0].DishID != "b" || c1[1].DishID != "d" || c1[2].DishID != "c" {
		t.Fatalf("C1: %v", c1)
	}
}
