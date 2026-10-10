package domain

import "testing"

func TestServings(t *testing.T) {
	r := []RecipeItem{{"a", 100}, {"b", 30}}
	cases := []struct {
		name  string
		avail map[string]Qty
		want  int64
	}{
		{"limited by b", map[string]Qty{"a": 1000, "b": 95}, 3},
		{"limited by a", map[string]Qty{"a": 250, "b": 1000}, 2},
		{"missing product", map[string]Qty{"a": 1000}, 0},
		{"just short", map[string]Qty{"a": 99, "b": 30}, 0},
		{"exact", map[string]Qty{"a": 100, "b": 30}, 1},
	}
	for _, c := range cases {
		if got := Servings(r, c.avail); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
	if Servings(nil, nil) != 0 {
		t.Error("empty recipe must give 0")
	}
}

func TestStopListReasons(t *testing.T) {
	dishes := []Dish{
		{ID: "ok", Recipe: []RecipeItem{{"a", 10}}},
		{ID: "stop", Recipe: []RecipeItem{{"a", 10}, {"b", 5}, {"c", 2}}},
	}
	got := StopList(dishes, map[string]Qty{"a": 50, "b": 3})
	if len(got) != 1 || got[0].Dish.ID != "stop" {
		t.Fatalf("stop list: %+v", got)
	}
	m := got[0].Missing
	if len(m) != 2 || m[0] != (Shortage{"b", 5, 3, 2}) || m[1] != (Shortage{"c", 2, 0, 2}) {
		t.Fatalf("reasons: %+v", m)
	}
}

// Стоп-лист зависит только от остатков: пересчёт после «истечения» партии даёт то же, что и с нуля.
func TestStopListIsFunctionOfAvailable(t *testing.T) {
	d := []Dish{{ID: "x", Recipe: []RecipeItem{{"a", 10}}}}
	if len(StopList(d, map[string]Qty{"a": 10})) != 0 {
		t.Fatal("must be available")
	}
	if len(StopList(d, map[string]Qty{})) != 1 {
		t.Fatal("must be stopped when nothing is available")
	}
}
