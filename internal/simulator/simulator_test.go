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
	if !a.Points[0].Main || a.Points[1].Main {
		t.Fatal("основная точка должна идти первой")
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
