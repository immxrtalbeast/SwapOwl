package invariants

import "testing"

// Инвариант 2 (БД). Атомарность заказа: потребность суммируется по блюдам заказа;
// заказ либо списывает всё, либо не меняет ни одной партии.
func TestInv02OrderAtomicity(t *testing.T) {
	s := newStand(t, standOpts{Dishes: map[string]map[string]int64{
		"a": {"shared": 100}, "b": {"shared": 100}}})
	s.pause()
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

// Инвариант 2 (БД), продажа из кассы (source: "pos", .ai/solution.md 2.3а): продажа уже
// состоялась, поэтому отказа нет. Списывается по FEFO всё доступное, недостача по продукту
// пишется в shortfalls; sale + shortfalls = суммарная потребность, ни одна партия не в минусе.
func TestInv02PosSaleWritesOffAvailableAndRecordsShortfall(t *testing.T) {
	s := newStand(t, standOpts{Dishes: map[string]map[string]int64{
		"a": {"shared": 100}, "b": {"shared": 100}}})
	s.pause()
	s.mustDeliver("shared", 100, 48) // две партии: 150 г при потребности 200 г
	s.mustDeliver("shared", 50, 72)

	id := newUUID()
	body := orderBody(id, orderLine{"a", 1}, orderLine{"b", 1})
	body["source"] = "pos"
	if st, b := s.do("POST", "/api/orders", body); st != 201 {
		t.Fatalf("продажа из кассы: ожидался 201, получено %d %s", st, b)
	}

	if n := s.queryInt(`SELECT count(*) FROM batches WHERE product_id = 'shared' AND remaining <> 0`); n != 0 {
		t.Fatalf("после продажи из кассы у %d партий остался остаток, ожидалось 0", n)
	}
	sold := s.queryInt(`SELECT COALESCE(SUM(quantity), 0) FROM movements
		WHERE kind = 'sale' AND product_id = 'shared' AND order_id = $1`, id)
	short := s.queryInt(`SELECT COALESCE(SUM(quantity), 0) FROM shortfalls
		WHERE product_id = 'shared' AND order_id = $1`, id)
	if sold != 150 || short != 50 {
		t.Fatalf("sale = %d (ожидалось 150), shortfalls = %d (ожидалось 50)", sold, short)
	}
	if sold+short != 200 {
		t.Fatalf("sale + shortfalls = %d ≠ потребности 200", sold+short)
	}
	s.checkConservation("после продажи из кассы")
}
