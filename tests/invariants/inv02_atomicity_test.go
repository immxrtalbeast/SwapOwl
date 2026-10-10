package invariants

import "testing"

// Инвариант 2 (БД). Атомарность заказа: потребность суммируется по блюдам заказа;
// заказ либо списывает всё, либо не меняет ни одной партии.
func TestInv02OrderAtomicity(t *testing.T) {
	s := newStand(t, standOpts{})
	s.pause()
	s.addDish("a", map[string]int64{"shared": 100})
	s.addDish("b", map[string]int64{"shared": 100})
	s.mustDeliver("shared", 150, 48) // каждому блюду хватает, обоим — нет

	before := s.batchesSnapshot()
	st, body := s.order(newUUID(), orderLine{"a", 1}, orderLine{"b", 1})
	if st != 409 || errCode(body) != "insufficient_stock" {
		t.Fatalf("ожидался 409 insufficient_stock, получено %d %s", st, body)
	}
	if after := s.batchesSnapshot(); after != before {
		t.Fatalf("партии изменились:\nдо:    %s\nпосле: %s", before, after)
	}
	if n := s.queryInt(`SELECT count(*) FROM movements WHERE kind = 'sale'`); n != 0 {
		t.Fatalf("в журнале %d движений sale после отказа", n)
	}
}
