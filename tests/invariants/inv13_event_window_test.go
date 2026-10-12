package invariants

import (
	"fmt"
	"testing"
	"time"
)

// Инвариант 13 (БД), окно событий (2.4а): время нового события не меньше now − LATE_EVENTS_HOURS и не больше now.
// Поставка и заказ с occurred_at вне окна — 400 validation_error, склад не меняется; на границах окна и внутри — приняты.
func TestInv13EventWindow(t *testing.T) {
	s := newStand(t, standOpts{Dishes: map[string]map[string]int64{"d": {"p": 100}}, Env: []string{"LATE_EVENTS_HOURS=2"}})
	s.pause()
	s.mustDeliver("p", 100000, 240)
	// Склад принят до начала окна: иначе заказ задним числом не находит партию, принятую «сейчас».
	s.jump(3 * time.Hour)
	now := s.now()
	at := func(d time.Duration) string { return now.Add(d).UTC().Format(time.RFC3339Nano) }
	seq := 0
	delivery := func(occurred string) (int, []byte) {
		seq++
		return s.do("POST", "/api/deliveries", map[string]any{
			"id": fmt.Sprintf("W-%d-%d", time.Now().UnixNano(), seq), "number": "1", "created_at": now.Format(time.RFC3339),
			"occurred_at": occurred,
			"items":       []map[string]any{{"product_id": "p", "name": "p", "unit": "g", "quantity": "100", "shelf_life_hours": 48}},
		})
	}
	order := func(occurred string) (int, []byte) {
		body := orderBody(newUUID(), orderLine{"d", 1})
		body["occurred_at"] = occurred
		return s.do("POST", "/api/orders", body)
	}

	for _, c := range []struct {
		name string
		d    time.Duration
		ok   bool
	}{
		{"раньше окна на 1 мин", -2*time.Hour - time.Minute, false},
		{"в будущем на 1 мин", time.Minute, false},
		{"нижняя граница окна + 1 с", -2*time.Hour + time.Second, true},
		{"внутри окна", -time.Hour, true},
		{"ровно now", 0, true},
	} {
		before := s.batchesSnapshot()
		for _, k := range []struct {
			kind string
			call func(string) (int, []byte)
		}{{"поставка", delivery}, {"заказ", order}} {
			kind := k.kind
			st, b := k.call(at(c.d))
			if c.ok {
				if st != 201 {
					t.Errorf("%s, %s: ожидался приём, %d %s", kind, c.name, st, b)
				}
				continue
			}
			if st != 400 || errCode(b) != "validation_error" {
				t.Errorf("%s, %s: ожидался 400 validation_error, %d %s", kind, c.name, st, b)
			}
		}
		if !c.ok && s.batchesSnapshot() != before {
			t.Errorf("%s: отклонённые события изменили партии", c.name)
		}
		s.checkConservation(c.name)
	}
}
