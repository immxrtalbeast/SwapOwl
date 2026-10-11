package generator

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	seed "umirhack/internal/seed/catalog"
)

var (
	from = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to   = from.Add(7 * 24 * time.Hour)
)

func gen(t *testing.T, s int64, a, b time.Time) []Event {
	t.Helper()
	ev, err := Generate(s, a, b)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestDeterministic(t *testing.T) {
	a, b := gen(t, 42, from, to), gen(t, 42, from, to)
	if len(a) == 0 {
		t.Fatal("нет событий")
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("один seed дал разные последовательности")
	}
	if reflect.DeepEqual(a, gen(t, 43, from, to)) {
		t.Fatal("разные seed дали одинаковый результат")
	}
}

func TestSplitIntervalsConcatenate(t *testing.T) {
	mid := from.Add(53*time.Hour + 17*time.Minute)
	whole := gen(t, 7, from, to)
	parts := append(gen(t, 7, from, mid), gen(t, 7, mid, to)...)
	if !reflect.DeepEqual(whole, parts) {
		t.Fatal("склейка соседних интервалов не равна целому")
	}
}

func TestEventsInIntervalAndSorted(t *testing.T) {
	ev := gen(t, 1, from, to)
	for i, e := range ev {
		if e.At.Before(from) || !e.At.Before(to) {
			t.Fatalf("событие %d вне интервала: %v", i, e.At)
		}
		if i > 0 && e.At.Before(ev[i-1].At) {
			t.Fatalf("события не отсортированы на %d", i)
		}
	}
}

func TestOrdersOnlyInWorkingHours(t *testing.T) {
	h := seed.WorkingHours()
	loc, _ := time.LoadLocation(h.TZ)
	orders, ids := 0, map[string]bool{}
	dishes := map[string]bool{}
	for _, d := range seed.Dishes() {
		dishes[d.ID] = true
	}
	for _, e := range gen(t, 99, from, from.Add(30*24*time.Hour)) {
		if e.Kind != KindOrder {
			continue
		}
		orders++
		l := e.At.In(loc)
		off := time.Duration(l.Hour())*time.Hour + time.Duration(l.Minute())*time.Minute + time.Duration(l.Second())*time.Second
		if off < h.OpenAt || off >= h.CloseAt {
			t.Fatalf("заказ вне часов работы: %v", l)
		}
		if ids[e.Order.ID] {
			t.Fatalf("повтор id заказа %s", e.Order.ID)
		}
		ids[e.Order.ID] = true
		seen := map[string]bool{}
		for _, it := range e.Order.Items {
			if !dishes[it.DishID] || it.Portions < 1 || seen[it.DishID] {
				t.Fatalf("плохая позиция заказа: %+v", it)
			}
			seen[it.DishID] = true
		}
		if len(e.Order.Items) == 0 {
			t.Fatal("пустой заказ")
		}
	}
	if orders == 0 {
		t.Fatal("нет заказов")
	}
}

// parseQty — разбор по таблице 7.3 .ai/solution.md; пакет units вне границ этой таски.
var qtyRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

func parseQty(q, unit string, base seed.Product) (int64, bool) {
	if !qtyRe.MatchString(q) {
		return 0, false
	}
	mult, dim := int64(1), unit
	switch unit {
	case "kg":
		mult, dim = 1000, "g"
	case "l":
		mult, dim = 1000, "ml"
	case "g", "ml", "pcs":
	default:
		return 0, false
	}
	if dim != string(base.Unit) {
		return 0, false
	}
	whole, frac, _ := strings.Cut(q, ".")
	var v int64
	for _, c := range whole {
		v = v*10 + int64(c-'0')
	}
	v *= mult
	if frac != "" {
		if mult == 1 || len(frac) > 3 {
			return 0, false
		}
		for len(frac) < 3 {
			frac += "0"
		}
		var f int64
		for _, c := range frac {
			f = f*10 + int64(c-'0')
		}
		v += f
	}
	return v, v > 0 && v <= 1_000_000_000_000
}

func TestQuantitiesParse(t *testing.T) {
	prods := map[string]seed.Product{}
	for _, p := range seed.Products() {
		prods[p.ID] = p
	}
	check := func(ds []seed.Delivery) {
		for _, d := range ds {
			for _, it := range d.Items {
				p, ok := prods[it.ProductID]
				if !ok {
					t.Fatalf("неизвестный продукт %s", it.ProductID)
				}
				v, ok := parseQty(it.Quantity, it.Unit, p)
				// Партии демо-сценария (D-SEED-*) заданы условиями сценария и не кратны упаковке.
				if !ok || (v%int64(p.PackSize) != 0 && !strings.HasPrefix(d.ID, "D-SEED-")) {
					t.Fatalf("%s: %q %s не разбирается или не кратно упаковке (%d)", it.ProductID, it.Quantity, it.Unit, v)
				}
				if it.ShelfLifeHours < 1 || it.ShelfLifeHours > 87600 {
					t.Fatalf("%s: срок %d вне 1…87600", it.ProductID, it.ShelfLifeHours)
				}
			}
		}
	}
	check(seed.StartingDeliveries(time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)))
	var ds []seed.Delivery
	for _, e := range gen(t, 5, from, to) {
		if e.Kind == KindDelivery {
			ds = append(ds, *e.Delivery)
		}
	}
	if len(ds) != 7 {
		t.Fatalf("ожидалась поставка в день, получено %d", len(ds))
	}
	check(ds)
}

func TestParseQtyTable(t *testing.T) {
	g := seed.Product{Unit: "g"}
	for q, want := range map[string]int64{"1.5": 1500, "0.001": 1} {
		if v, ok := parseQty(q, "kg", g); !ok || v != want {
			t.Fatalf("%s kg: %d %v", q, v, ok)
		}
	}
	if _, ok := parseQty("0.0005", "kg", g); ok {
		t.Fatal("дробь мельче базовой должна отклоняться")
	}
}
