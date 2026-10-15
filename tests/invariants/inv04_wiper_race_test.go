package invariants

import (
	"context"
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
// (прыжок выполняет проход «дворника», спецификация 2.4).
//
// Без окна опоздания (LATE_EVENTS_HOURS=0) «дворник» списывает ровно на expires_at — эта гонка.
// С окном (2.4а) граница сдвигается на expires_at + окно — см. TestInv04LateSalesVersusWiper.
func TestInv04OrdersVersusWiper(t *testing.T) {
	const rounds = 20
	dishes := map[string]map[string]int64{}
	for r := 0; r < rounds; r++ {
		dishes[fmt.Sprintf("d%d", r)] = map[string]int64{fmt.Sprintf("p%d", r): 100}
	}
	s := newStand(t, standOpts{Dishes: dishes, Env: []string{"LATE_EVENTS_HOURS=0"}})
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

// Инвариант 4 (БД) с окном опоздания (спецификация 2.4а): «дворник» списывает партию не раньше
// expires_at + окно, а до этого её может взять продажа задним числом с occurred_at < expires_at.
// Гонка на новой границе: часы стоят за 10 мс до expires_at + окно; 50 продаж с occurred_at за 5 мс
// до expires_at стартуют одновременно с прыжком на 20 мс через границу. Продажа до прыжка принимается
// (201), после — уже вне окна (400). Просроченная партия не получает остаток обратно, дедлоков нет.
func TestInv04LateSalesVersusWiper(t *testing.T) {
	const rounds = 20
	const window = 2 * time.Hour
	dishes := map[string]map[string]int64{}
	for r := 0; r < rounds; r++ {
		dishes[fmt.Sprintf("d%d", r)] = map[string]int64{fmt.Sprintf("p%d", r): 100}
	}
	s := newStand(t, standOpts{Dishes: dishes, Env: []string{"LATE_EVENTS_HOURS=2"}})
	s.pause()
	for round := 0; round < rounds; round++ {
		prod, dish := fmt.Sprintf("p%d", round), fmt.Sprintf("d%d", round)
		s.mustDeliver(prod, 100*1000, 24)
		var expires time.Time
		if err := s.pool.QueryRow(context.Background(),
			`SELECT expires_at FROM batches WHERE product_id = $1`, prod).Scan(&expires); err != nil {
			t.Fatal(err)
		}
		s.jump(24*time.Hour + window - 10*time.Millisecond)
		if left := s.queryInt(`SELECT COALESCE(SUM(remaining),0) FROM batches WHERE product_id = $1`, prod); left == 0 {
			t.Fatalf("раунд %d: партия списана раньше конца окна опоздания", round)
		}
		occurred := expires.Add(-5 * time.Millisecond).UTC().Format(time.RFC3339Nano)

		start := make(chan struct{})
		var wg sync.WaitGroup
		codes := make([]int, 50)
		bodies := make([][]byte, 50)
		for i := range codes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				body := orderBody(newUUID(), orderLine{dish, 1})
				body["occurred_at"] = occurred
				codes[i], bodies[i] = s.do("POST", "/api/orders", body)
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s.jump(20 * time.Millisecond) // через expires_at + окно
		}()
		close(start)
		wg.Wait()

		for i, c := range codes {
			if c != 201 && !(c == 400 && errCode(bodies[i]) == "validation_error") {
				t.Fatalf("раунд %d: неожиданный ответ на продажу задним числом %d %s", round, c, bodies[i])
			}
		}
		if left := s.queryInt(`SELECT COALESCE(SUM(remaining),0) FROM batches WHERE product_id = $1`, prod); left != 0 {
			t.Fatalf("раунд %d: у просроченной партии после окна remaining = %d, ожидался 0", round, left)
		}
		sold := s.queryInt(`SELECT COALESCE(SUM(quantity),0) FROM movements WHERE product_id = $1 AND kind = 'sale'`, prod)
		written := s.queryInt(`SELECT COALESCE(SUM(quantity),0) FROM movements WHERE product_id = $1 AND kind = 'writeoff'`, prod)
		if sold+written != 100*1000 {
			t.Fatalf("раунд %d: продано %d + списано %d ≠ поступило %d", round, sold, written, 100*1000)
		}
		if written == 0 {
			t.Fatalf("раунд %d: нет списания по сроку — гонка с «дворником» не состоялась", round)
		}
		if n := s.queryInt(`SELECT count(*) FROM movements WHERE product_id = $1 AND kind = 'writeoff'
			AND at <> (SELECT expires_at FROM batches WHERE product_id = $1)`, prod); n != 0 {
			t.Fatalf("раунд %d: %d движений списания не со временем expires_at", round, n)
		}
		s.checkConservation(fmt.Sprintf("раунд %d", round))
		if strings.Contains(s.log.String(), "deadlock detected") {
			t.Fatalf("раунд %d: в журнале сервиса deadlock detected", round)
		}
	}
}
