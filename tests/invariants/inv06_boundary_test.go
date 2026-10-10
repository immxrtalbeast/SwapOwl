package invariants

import (
	"encoding/json"
	"testing"
	"time"
)

// Инвариант 6 (БД). Строгая граница срока: при now < expires_at партия доступна,
// при now ≥ expires_at — нет. Проход «дворника» не вызывается: корректность не должна от него зависеть.
func TestInv06StrictExpiryBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		jump     time.Duration
		accepted bool
	}{
		{"за микросекунду до срока", 72*time.Hour - time.Microsecond, true},
		{"ровно в срок", 72 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStand(t, standOpts{})
			s.pause()
			s.addDish("dish", map[string]int64{"prod": 100})
			s.mustDeliver("prod", 100, 72)
			start := s.now()
			s.jump(tc.jump)
			if got := s.now().Sub(start); got != tc.jump {
				t.Fatalf("часы стенда сдвинулись на %v, ожидалось %v", got, tc.jump)
			}

			st, body := s.order(newUUID(), orderLine{"dish", 1})
			if tc.accepted && st != 201 {
				t.Fatalf("до срока заказ должен приниматься: %d %s", st, body)
			}
			if !tc.accepted {
				if st != 409 || errCode(body) != "insufficient_stock" {
					t.Fatalf("в момент срока ожидался 409 insufficient_stock: %d %s", st, body)
				}
				_, b := s.do("GET", "/api/products", nil)
				var list []struct {
					ID        string `json:"id"`
					Available struct {
						Qty int64 `json:"qty"`
					} `json:"available"`
				}
				if err := json.Unmarshal(b, &list); err != nil {
					t.Fatal(err)
				}
				for _, p := range list {
					if p.ID == "prod" && p.Available.Qty != 0 {
						t.Fatalf("просроченная партия входит в остаток: %d", p.Available.Qty)
					}
				}
				if !s.apiStopList()["dish"] {
					t.Fatal("блюдо с просроченным ингредиентом не в стоп-листе")
				}
			}
		})
	}
}
