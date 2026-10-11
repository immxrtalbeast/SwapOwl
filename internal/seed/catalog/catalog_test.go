package catalog

import (
	"testing"
	"time"
	"umirhack/internal/domain"
)

func TestCatalogConsistent(t *testing.T) {
	prods := map[string]Product{}
	for _, p := range Products() {
		if p.ID == "" || p.PriceMinorPer1000 <= 0 || p.PackSize <= 0 || p.ShelfLifeHours < 1 {
			t.Fatalf("плохой продукт: %+v", p)
		}
		if prods[p.ID].ID != "" {
			t.Fatalf("повтор продукта %s", p.ID)
		}
		prods[p.ID] = p
	}
	for _, d := range Dishes() {
		if len(d.Recipe) == 0 || d.PriceMinor <= 0 || d.PriorDaily <= 0 {
			t.Fatalf("плохое блюдо: %+v", d)
		}
		for _, it := range d.Recipe {
			if _, ok := prods[it.ProductID]; !ok || it.Quantity <= 0 {
				t.Fatalf("%s: плохая позиция рецепта %+v", d.ID, it)
			}
		}
	}
	if h := WorkingHours(); h.OpenAt >= h.CloseAt {
		t.Fatal("OpenAt должен быть раньше CloseAt")
	}
}

func TestStartingDeliveriesRelativeToT0(t *testing.T) {
	t0 := time.Date(2026, 10, 10, 11, 0, 0, 0, time.UTC)
	ds := StartingDeliveries(t0)
	if len(ds) == 0 {
		t.Fatal("нет стартовых поставок")
	}
	for _, d := range ds {
		if !d.CreatedAt.Before(t0) || len(d.Items) != len(Products()) {
			t.Fatalf("плохая поставка %+v", d)
		}
	}
}

func TestFormatQty(t *testing.T) {
	for _, c := range []struct {
		q    int64
		u    domain.Unit
		want string
		unit string
	}{{1500, "g", "1.5", "kg"}, {999, "g", "999", "g"}, {2000, "ml", "2", "l"}, {7, "pcs", "7", "pcs"}, {1001, "g", "1.001", "kg"}} {
		q, u := FormatQty(domain.Qty(c.q), c.u)
		if q != c.want || u != c.unit {
			t.Fatalf("%d %s: %s %s", c.q, c.u, q, u)
		}
	}
}
