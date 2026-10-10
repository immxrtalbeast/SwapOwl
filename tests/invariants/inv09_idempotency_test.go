package invariants

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
)

// стираем replayed, чтобы сравнивать «тот же ответ».
func withoutReplayed(t *testing.T, b []byte) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("не JSON: %s", b)
	}
	delete(m, "replayed")
	out, _ := json.Marshal(m)
	return out
}

// Инвариант 9 (БД). Идемпотентность заказа.
func TestInv09OrderIdempotency(t *testing.T) {
	s := newStand(t, standOpts{Dishes: map[string]map[string]int64{"dish": {"prod": 100}}})
	s.pause()
	s.mustDeliver("prod", 5000, 48)

	id := newUUID()
	st1, b1 := s.order(id, orderLine{"dish", 2})
	if st1 != 201 {
		t.Fatalf("первый заказ: %d %s", st1, b1)
	}
	st2, b2 := s.order(id, orderLine{"dish", 2})
	if st2 != 200 || !bytes.Equal(withoutReplayed(t, b1), withoutReplayed(t, b2)) {
		t.Fatalf("повтор: %d %s; ожидался 200 и тот же ответ, что %s", st2, b2, b1)
	}
	st3, b3 := s.order(id, orderLine{"dish", 3})
	if st3 != 409 || errCode(b3) != "order_conflict" {
		t.Fatalf("другое тело: %d %s; ожидался 409 order_conflict", st3, b3)
	}
	if n := s.queryInt(`SELECT count(*) FROM movements WHERE kind = 'sale' AND order_id = $1`, id); n != 1 {
		t.Fatalf("движений sale %d, ожидалось 1 (один набор для одной партии)", n)
	}

	// 10 одновременных одинаковых POST.
	id2 := newUUID()
	var wg sync.WaitGroup
	codes := make([]int, 10)
	for i := range codes {
		wg.Add(1)
		go func() { defer wg.Done(); codes[i], _ = s.order(id2, orderLine{"dish", 2}) }()
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		switch c {
		case 201:
			created++
		case 200:
		default:
			t.Fatalf("параллельный повтор: статус %d", c)
		}
	}
	if created != 1 {
		t.Fatalf("создано %d раз, ожидался один раз", created)
	}
	if n := s.queryInt(`SELECT COALESCE(SUM(quantity),0) FROM movements WHERE kind = 'sale' AND order_id = $1`, id2); n != 200 {
		t.Fatalf("продано %d, ожидалось 200 (2 порции по 100)", n)
	}
	s.checkConservation("после повторов")
}
