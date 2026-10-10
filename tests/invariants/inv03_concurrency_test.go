package invariants

import (
	"fmt"
	"sync"
	"testing"
)

// Инвариант 3 (БД). Параллельные заказы не продают одну порцию дважды:
// 50 горутин по одной порции при остатке на 7 порций — ровно 7 принято, 43 отклонено.
func TestInv03ConcurrentOrders(t *testing.T) {
	dishes := map[string]map[string]int64{}
	for round := 0; round < 20; round++ {
		dishes[fmt.Sprintf("d%d", round)] = map[string]int64{fmt.Sprintf("p%d", round): 100}
	}
	s := newStand(t, standOpts{Dishes: dishes})
	s.pause()
	for round := 0; round < 20; round++ {
		prod, dish := fmt.Sprintf("p%d", round), fmt.Sprintf("d%d", round)
		s.mustDeliver(prod, 700, 48)

		var wg sync.WaitGroup
		codes := make([]int, 50)
		for i := range codes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[i], _ = s.order(newUUID(), orderLine{dish, 1})
			}()
		}
		wg.Wait()
		acc, rej := 0, 0
		for _, c := range codes {
			switch c {
			case 201:
				acc++
			case 409:
				rej++
			default:
				t.Fatalf("раунд %d: неожиданный статус %d", round, c)
			}
		}
		if acc != 7 || rej != 43 {
			t.Fatalf("раунд %d: принято %d, отклонено %d; ожидалось 7 и 43", round, acc, rej)
		}
		if left := s.queryInt(`SELECT COALESCE(SUM(remaining),0) FROM batches WHERE product_id = $1`, prod); left != 0 {
			t.Fatalf("раунд %d: остаток %d, ожидался 0", round, left)
		}
		s.checkConservation(fmt.Sprintf("раунд %d", round))
	}
}
