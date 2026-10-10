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
//
// Запас заведомо больше, чем продадут заказы (1000 порций против 50), поэтому к границе у партии
// остаётся большой remaining, и «дворнику» есть что списывать. Часы стоят на паузе за 1 мс до
// expires_at; ровно 50 заказов по одной порции стартуют одновременно с прыжком через границу
// (прыжок выполняет проход «дворника», .ai/solution.md 2.4).
func TestInv04OrdersVersusWiper(t *testing.T) {
	const rounds = 20
	dishes := map[string]map[string]int64{}
	for r := 0; r < rounds; r++ {
		dishes[fmt.Sprintf("d%d", r)] = map[string]int64{fmt.Sprintf("p%d", r): 100}
	}
	s := newStand(t, standOpts{Dishes: dishes})
	s.pause()
	for round := 0; round < rounds; round++ {
		prod, dish := fmt.Sprintf("p%d", round), fmt.Sprintf("d%d", round)
		s.mustDeliver(prod, 100*1000, 24)
		s.jump(24*time.Hour - time.Millisecond)

		start := make(chan struct{})
		var wg sync.WaitGroup
		codes := make([]int, 50)
		for i := range codes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				codes[i], _ = s.order(newUUID(), orderLine{dish, 1})
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s.jump(2 * time.Millisecond) // через expires_at
		}()
		close(start)
		wg.Wait()

		for _, c := range codes {
			if c != 201 && c != 409 {
				t.Fatalf("раунд %d: неожиданный статус заказа %d", round, c)
			}
		}
		if left := s.queryInt(`SELECT COALESCE(SUM(remaining),0) FROM batches WHERE product_id = $1`, prod); left != 0 {
			t.Fatalf("раунд %d: у просроченной партии remaining = %d, ожидался 0", round, left)
		}
		sold := s.queryInt(`SELECT COALESCE(SUM(quantity),0) FROM movements WHERE product_id = $1 AND kind = 'sale'`, prod)
		written := s.queryInt(`SELECT COALESCE(SUM(quantity),0) FROM movements WHERE product_id = $1 AND kind = 'writeoff'`, prod)
		if sold+written != 100*1000 {
			t.Fatalf("раунд %d: продано %d + списано %d ≠ поступило %d", round, sold, written, 100*1000)
		}
		if written == 0 {
			t.Fatalf("раунд %d: нет списания по сроку — гонка с «дворником» не состоялась", round)
		}
		s.checkConservation(fmt.Sprintf("раунд %d", round))
		if strings.Contains(s.log.String(), "deadlock detected") {
			t.Fatalf("раунд %d: в журнале сервиса deadlock detected", round)
		}
	}
}
