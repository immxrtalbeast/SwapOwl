package invariants

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// Инвариант 4 (БД). Заказы и проход «дворника» на границе истечения: просроченная
// партия не получает остаток обратно, взаимных блокировок нет.
func TestInv04OrdersVersusWiper(t *testing.T) {
	s := newStand(t, standOpts{})
	for round := 0; round < 20; round++ {
		prod, dish := fmt.Sprintf("p%d", round), fmt.Sprintf("d%d", round)
		s.pause()
		s.addDish(dish, map[string]int64{prod: 100})
		s.mustDeliver(prod, 700, 24)
		expires := s.now().Add(24 * time.Hour)
		// Ставим часы за 100 мс до границы и отпускаем их на реальной скорости.
		s.jump(24*time.Hour - 100*time.Millisecond)
		s.speed(1)

		var wg sync.WaitGroup
		var bad sync.Map
		for w := 0; w < 50; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for s.now().Before(expires.Add(300 * time.Millisecond)) {
					if c, b := s.order(newUUID(), orderLine{dish, 1}); c != 201 && c != 409 {
						bad.Store(c, string(b))
					}
				}
			}()
		}
		wg.Wait()
		bad.Range(func(k, v any) bool {
			t.Errorf("раунд %d: статус %v: %v", round, k, v)
			return true
		})
		s.pause()

		deadline := time.Now().Add(15 * time.Second)
		for s.queryInt(`SELECT COALESCE(SUM(remaining),0) FROM batches WHERE product_id = $1`, prod) != 0 {
			if time.Now().After(deadline) {
				t.Fatalf("раунд %d: у просроченной партии остался remaining > 0", round)
			}
			time.Sleep(100 * time.Millisecond)
		}
		out := s.queryInt(`SELECT COALESCE(SUM(quantity),0) FROM movements WHERE product_id = $1 AND kind IN ('sale','writeoff')`, prod)
		if out != 700 {
			t.Fatalf("раунд %d: продано и списано %d, поступило 700", round, out)
		}
		s.checkConservation(fmt.Sprintf("раунд %d", round))
		if strings.Contains(s.log.String(), "deadlock detected") {
			t.Fatalf("раунд %d: в журнале сервиса deadlock detected", round)
		}
	}
}
