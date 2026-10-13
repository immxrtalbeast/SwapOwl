package invariants

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// Инвариант 1 (БД) с корректировками (2.3б) и вскрытием упаковки: поступило − продано − списано + возвращено
// = remaining по журналу движений после каждого шага случайной последовательности, где кроме поставок, заказов
// и прыжков есть брак, инвентаризация, отмена заказа и вскрытие (дочерняя партия). Остаток продукта —
// Σ remaining непросроченных партий.
func TestInv01ConservationWithCorrections(t *testing.T) {
	s := newStand(t, standOpts{Dishes: randomDishes})
	// p1 вскрывается: после вскрытия годен 24 ч (FS-139).
	s.exec(`UPDATE products SET shelf_after_open_hours = 24 WHERE id = 'p1'`)
	// Срок по умолчанию — как в настоящем справочнике: излишек инвентаризации получает партию со сроком.
	s.exec(`UPDATE products SET shelf_life_hours = 72`)
	rng := rand.New(rand.NewSource(7))
	s.pause()
	products := []string{"p1", "p2", "p3"}
	dishes := []string{"d1", "d2", "d3"}
	for _, p := range products {
		s.mustDeliver(p, 2000, 72)
	}
	var accepted []string
	seq := 0
	id := func(prefix string) string { seq++; return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), seq) }

	for i := 0; i < 600; i++ {
		var what string
		var st int
		var b []byte
		switch r := rng.Intn(20); {
		case r < 4:
			p := products[rng.Intn(len(products))]
			what = "поставка " + p
			st, b = s.deliver(p, int64(1+rng.Intn(2000)), 6+rng.Intn(90))
			s.expect(i, what, st, b, 201)
		case r < 10:
			d := dishes[rng.Intn(len(dishes))]
			oid := newUUID()
			what = "заказ " + d
			st, b = s.order(oid, orderLine{d, 1 + rng.Intn(4)})
			s.expect(i, what, st, b, 201, 409)
			if st == 201 {
				accepted = append(accepted, oid)
			}
		case r < 12:
			if len(accepted) == 0 {
				continue
			}
			k := rng.Intn(len(accepted))
			oid := accepted[k]
			accepted = append(accepted[:k], accepted[k+1:]...)
			what = "отмена заказа " + oid
			st, b = s.do("POST", "/api/orders/"+oid+"/cancel", map[string]any{"id": id("cancel")})
			s.expect(i, what, st, b, 201)
		case r < 14:
			bid, p, rem := s.randomBatch(rng, "", false)
			if bid == 0 {
				continue
			}
			what = fmt.Sprintf("брак партии %d (%s)", bid, p)
			st, b = s.do("POST", "/api/writeoffs", map[string]any{"id": id("wo"), "batch_id": bid,
				"qty": fmt.Sprint(1 + rng.Int63n(rem)), "unit": "g", "reason": "spoilage"})
			s.expect(i, what, st, b, 201)
		case r < 16:
			p := products[rng.Intn(len(products))]
			avail := s.availableByProduct(s.now())[p]
			counted := avail + int64(rng.Intn(600)) - 300
			if counted < 0 {
				counted = 0
			}
			what = fmt.Sprintf("инвентаризация %s: %d при %d", p, counted, avail)
			st, b = s.do("POST", "/api/inventory", map[string]any{"id": id("inv"), "product_id": p,
				"counted": fmt.Sprint(counted), "unit": "g"})
			s.expect(i, what, st, b, 201)
		case r < 18:
			bid, _, rem := s.randomBatch(rng, "p1", true)
			if bid == 0 {
				continue
			}
			what = fmt.Sprintf("вскрытие партии %d", bid)
			st, b = s.do("POST", fmt.Sprintf("/api/batches/%d/open", bid), map[string]any{"id": id("open"),
				"qty": fmt.Sprint(1 + rng.Int63n(rem)), "unit": "g"})
			s.expect(i, what, st, b, 201)
		default:
			h := 0.5 + rng.Float64()*20
			what = fmt.Sprintf("прыжок %.2f ч", h)
			s.jump(time.Duration(h * float64(time.Hour)))
		}
		s.checkConservation(fmt.Sprintf("шаг %d (%s)", i, what))
		s.checkAvailable(i, what)
	}
	// Последовательность должна реально задеть новые виды движений, иначе тест ничего не доказывает.
	for _, kind := range []string{"return", "adjustment", "transfer_out", "transfer_in", "writeoff"} {
		if n := s.queryInt(`SELECT count(*) FROM movements WHERE kind = $1`, kind); n == 0 {
			t.Errorf("за прогон нет ни одного движения %s", kind)
		}
	}
}

// expect — статус ответа из допустимых, иначе падение с шагом и телом.
func (s *stand) expect(step int, what string, st int, body []byte, ok ...int) {
	s.t.Helper()
	for _, c := range ok {
		if st == c {
			return
		}
	}
	s.t.Fatalf("шаг %d: %s: %d %s", step, what, st, body)
}

// randomBatch — случайная доступная (remaining > 0, не просрочена) партия; product — фильтр или "";
// unopened — только невскрытые: вскрытую партию повторно не вскрывают (409 already_opened).
func (s *stand) randomBatch(rng *rand.Rand, product string, unopened bool) (id int64, prod string, remaining int64) {
	s.t.Helper()
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, product_id, remaining FROM batches
		WHERE remaining > 0 AND expires_at > $1 AND ($2 = '' OR product_id = $2) AND (NOT $3 OR opened_at IS NULL)
		ORDER BY id`, s.now(), product, unopened)
	if err != nil {
		s.t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		id  int64
		p   string
		rem int64
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.p, &r.rem); err != nil {
			s.t.Fatal(err)
		}
		all = append(all, r)
	}
	if len(all) == 0 {
		return 0, "", 0
	}
	r := all[rng.Intn(len(all))]
	return r.id, r.p, r.rem
}

// checkAvailable — остаток продукта в API = Σ remaining непросроченных партий.
func (s *stand) checkAvailable(step int, what string) {
	s.t.Helper()
	want := s.availableByProduct(s.now())
	st, b := s.do("GET", "/api/products", nil)
	var list []struct {
		ID        string `json:"id"`
		Available struct {
			Qty int64 `json:"qty"`
		} `json:"available"`
	}
	if st != 200 || json.Unmarshal(b, &list) != nil {
		s.t.Fatalf("шаг %d: GET /api/products: %d %s", step, st, b)
	}
	for _, p := range list {
		if p.Available.Qty != want[p.ID] {
			s.t.Fatalf("шаг %d (%s): остаток %s = %d, по партиям %d", step, what, p.ID, p.Available.Qty, want[p.ID])
		}
	}
}
