package events

import (
	"encoding/json"
	"reflect"
	"sort"
)

// coalesce склеивает два подряд идущих события одного типа без значимых данных (7.5):
// клиент всё равно перечитывает состояние, поэтому важен лишь итог. Результат несёт
// id и "at" последнего события. order.*, delivery.accepted и batch.* не склеиваются.
func coalesce(prev, next Event) (Event, bool) {
	if prev.Type != next.Type {
		return Event{}, false
	}
	var a, b map[string]any
	if json.Unmarshal(prev.Data, &a) != nil || json.Unmarshal(next.Data, &b) != nil {
		return Event{}, false
	}
	var m map[string]any
	switch next.Type {
	case StockChanged:
		m = b
		m["product_ids"] = union(a["product_ids"], b["product_ids"])
	case StoplistChanged:
		// итоговая разница: блюдо, добавленное и затем снятое (или наоборот), из события уходит
		aAdd, aRem, bAdd, bRem := asSet(a["added"]), asSet(a["removed"]), asSet(b["added"]), asSet(b["removed"])
		m = b
		m["added"] = minus(union(a["added"], b["added"]), bRem, aRem)
		m["removed"] = minus(union(a["removed"], b["removed"]), bAdd, aAdd)
	case HotOffersChanged:
		// список разных алгоритмов — разные сигналы
		if !reflect.DeepEqual(withoutAt(a), withoutAt(b)) {
			return Event{}, false
		}
		m = b
	case ClockChanged:
		m = b // значимо только последнее состояние часов
	default:
		return Event{}, false
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return Event{}, false
	}
	return Event{ID: next.ID, Type: next.Type, Data: raw}, true
}

func withoutAt(m map[string]any) map[string]any {
	c := make(map[string]any, len(m))
	for k, v := range m {
		if k != "at" {
			c[k] = v
		}
	}
	return c
}

func asSet(v any) map[string]struct{} {
	set := map[string]struct{}{}
	list, _ := v.([]any)
	for _, x := range list {
		if s, ok := x.(string); ok {
			set[s] = struct{}{}
		}
	}
	return set
}

// union — отсортированное объединение двух списков строк (nil считается пустым).
func union(a, b any) []string {
	set := asSet(a)
	for k := range asSet(b) {
		set[k] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func minus(list []string, drops ...map[string]struct{}) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		keep := true
		for _, d := range drops {
			if _, ok := d[s]; ok {
				keep = false
			}
		}
		if keep {
			out = append(out, s)
		}
	}
	return out
}
