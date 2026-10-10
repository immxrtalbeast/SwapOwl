package invariants

import (
	"fmt"
	"math/rand"
	"time"
)

// randomDishes — справочник для случайных последовательностей (№1 и №8).
var randomDishes = map[string]map[string]int64{
	"d1": {"p1": 100, "p2": 50},
	"d2": {"p2": 200},
	"d3": {"p3": 150, "p1": 30},
}

// runRandom — случайная последовательность поставок, заказов и прыжков времени
// с фиксированным seed. after вызывается после каждого шага.
func runRandom(s *stand, steps int, seed int64, after func(step int, what string)) {
	s.t.Helper()
	rng := rand.New(rand.NewSource(seed))
	s.pause()
	products := []string{"p1", "p2", "p3"}
	dishes := []string{"d1", "d2", "d3"}
	for _, p := range products {
		s.mustDeliver(p, 1000, 48)
	}
	for i := 0; i < steps; i++ {
		var what string
		switch r := rng.Intn(10); {
		case r < 3:
			p := products[rng.Intn(len(products))]
			what = fmt.Sprintf("поставка %s", p)
			if st, b := s.deliver(p, int64(1+rng.Intn(2000)), 6+rng.Intn(90)); st != 201 {
				s.t.Fatalf("шаг %d: %s: %d %s", i, what, st, b)
			}
		case r < 8:
			d := dishes[rng.Intn(len(dishes))]
			what = "заказ " + d
			st, b := s.order(newUUID(), orderLine{d, 1 + rng.Intn(5)})
			if st != 201 && st != 409 {
				s.t.Fatalf("шаг %d: %s: %d %s", i, what, st, b)
			}
		default:
			h := 0.5 + rng.Float64()*30
			what = fmt.Sprintf("прыжок %.2f ч", h)
			s.jump(time.Duration(h * float64(time.Hour)))
		}
		after(i, what)
	}
}
