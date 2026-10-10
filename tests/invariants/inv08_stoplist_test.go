package invariants

import "testing"

// Инвариант 8 (БД). Стоп-лист — функция остатков: после любого события, включая
// наступление срока без других событий, ответ API равен пересчёту с нуля по партиям и рецептам.
func TestInv08StopListIsFunctionOfStock(t *testing.T) {
	s := newStand(t, standOpts{})
	runRandom(s, 1000, 1, func(step int, what string) {
		want := s.referenceStopList(s.now())
		if got := s.apiStopList(); !sameSet(got, want) {
			t.Fatalf("шаг %d (%s): стоп-лист API %s, эталон %s", step, what, keys(got), keys(want))
		}
	})
}

// Отдельный случай: меняется только время.
func TestInv08StopListOnlyClockJump(t *testing.T) {
	s := newStand(t, standOpts{})
	s.pause()
	s.addDish("dish", map[string]int64{"prod": 100})
	s.mustDeliver("prod", 500, 10)
	if s.apiStopList()["dish"] {
		t.Fatal("блюдо в стоп-листе при полном складе")
	}
	s.jumpHours(10)
	if !s.apiStopList()["dish"] {
		t.Fatal("после наступления срока блюдо должно быть в стоп-листе без других событий")
	}
}
