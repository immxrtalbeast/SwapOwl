package invariants

import (
	"encoding/json"
	"testing"
)

// Инвариант 1 (БД). Сохранение количества: поступило − продано − списано = remaining ≥ 0;
// остаток продукта = Σ remaining его непросроченных партий.
func TestInv01QuantityConservation(t *testing.T) {
	s := newStand(t, standOpts{})
	runRandom(s, 1000, 1, func(step int, what string) {
		s.checkConservation(what)
		now := s.now()
		want := s.availableByProduct(now)
		st, b := s.do("GET", "/api/products", nil)
		var list []struct {
			ID        string `json:"id"`
			Available struct {
				Qty int64 `json:"qty"`
			} `json:"available"`
		}
		if st != 200 || json.Unmarshal(b, &list) != nil {
			t.Fatalf("шаг %d: GET /api/products: %d %s", step, st, b)
		}
		for _, p := range list {
			if p.Available.Qty != want[p.ID] {
				t.Fatalf("шаг %d (%s): остаток %s = %d, по партиям %d", step, what, p.ID, p.Available.Qty, want[p.ID])
			}
		}
	})
}
