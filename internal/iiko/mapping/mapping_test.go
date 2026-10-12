package mapping

import (
	"testing"
	"time"

	"umirhack/internal/iiko"
)

func TestBuildCatalog(t *testing.T) {
	products := []iiko.Product{
		{ID: "rice", Name: "Рис", Type: "GOODS", MainUnit: "кг", ShelfLifeHours: 720},
		{ID: "egg", Name: "Яйцо", Type: "GOODS", MainUnit: "шт"},
		{ID: "pilaf", Name: "Плов", Type: "DISH", Price: 450},
		{ID: "bare", Name: "Без техкарты", Type: "DISH", Price: 1},
	}
	charts := []iiko.AssemblyChart{{AssembledProductID: "pilaf", AssembledAmount: 1,
		Items: []iiko.ChartItem{{ProductID: "rice", AmountIn: 0.15}, {ProductID: "egg", AmountIn: 1}}}}
	cat, units, skipped := BuildCatalog(products, charts, map[string]float64{"rice": 150}, Defaults{PriceMinorPer1000: 7, LeadDays: 2, PackSize: 1000, PriorDaily: 10})
	if len(cat.Products) != 2 || len(cat.Dishes) != 1 || len(skipped) != 1 || units["rice"].Base != "g" {
		t.Fatalf("%+v %v", cat, skipped)
	}
	if cat.Products[0].PriceMinorPer1000 != 15000 || cat.Products[1].PriceMinorPer1000 != 7 || cat.Products[0].ShelfLifeHours != 720 {
		t.Fatalf("продукты: %+v", cat.Products)
	}
	d := cat.Dishes[0]
	if d.PriceMinor != 45000 || d.Recipe[0].Quantity != "150" || d.Recipe[0].Unit != "g" {
		t.Fatalf("блюдо: %+v", d)
	}
}

func TestBuildDelivery(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	units := map[string]Unit{"rice": {"g", "kg", 1000}}
	inv := iiko.IncomingInvoice{ID: "inv-1", DocumentNumber: "7", DateIncoming: "2026-01-02T09:30:00", Status: "PROCESSED",
		Items: []iiko.InvoiceItem{{Product: "rice", Amount: 2.5}, {Product: "ghost", Amount: 1}}}
	d, _, ok := BuildDelivery(inv, units, map[string]int{"rice": 48}, loc)
	if !ok || d.ID != "inv-1" || len(d.Items) != 1 || d.Items[0].Quantity != "2500" || d.Items[0].ShelfLifeHours != 48 || d.CreatedAt != "2026-01-02T09:30:00+03:00" || d.OccurredAt != "2026-01-02T09:30:00+03:00" {
		t.Fatalf("%+v %v", d, ok)
	}
	inv.Status = "NEW"
	if _, _, ok := BuildDelivery(inv, units, nil, loc); ok {
		t.Fatal("непроведённая накладная передана")
	}
}

func TestQtyBaseUnits(t *testing.T) {
	kg := Unit{"g", "kg", 1000}
	if got := Qty(0.0125, kg); got != "13" { // 12,5 г округляется до целого грамма
		t.Fatalf("0.0125 кг: %s", got)
	}
	if got := Qty(0.1/3, kg); got != "33" {
		t.Fatalf("0.1/3 кг: %s", got)
	}
	cat, _, _ := BuildCatalog(
		[]iiko.Product{{ID: "p", Type: "GOODS", MainUnit: "кг"}, {ID: "d", Type: "DISH", Price: 1}},
		[]iiko.AssemblyChart{{AssembledProductID: "d", AssembledAmount: 3, Items: []iiko.ChartItem{{ProductID: "p", AmountIn: 0.1}}}},
		nil, Defaults{})
	if r := cat.Dishes[0].Recipe[0]; r.Quantity != "33" || r.Unit != "g" {
		t.Fatalf("рецепт: %+v", r)
	}
	cat, _, _ = BuildCatalog(
		[]iiko.Product{{ID: "p", Type: "GOODS", MainUnit: "кг"}, {ID: "d", Type: "DISH", Price: 1}},
		[]iiko.AssemblyChart{{AssembledProductID: "d", AssembledAmount: 1, Items: []iiko.ChartItem{{ProductID: "p", AmountIn: 0.0001}}}},
		nil, Defaults{})
	if r := cat.Dishes[0].Recipe[0]; r.Quantity != "1" {
		t.Fatalf("минимум 1: %+v", r)
	}
}

func TestBuildOrdersPerCheck(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	sales := []Sale{
		{Date: "2026-01-02", DishID: "soup", Portions: 2, CheckID: "c3", OpenTime: "2026-01-02T19:40:00"},
		{Date: "2026-01-02", DishID: "pilaf", Portions: 1, CheckID: "c1", OpenTime: "2026-01-02T12:05:00"},
		{Date: "2026-01-02", DishID: "soup", Portions: 1, CheckID: "c1", OpenTime: "2026-01-02T12:05:00"},
		{Date: "2026-01-02", DishID: "soup", Portions: 3, CheckID: "c2", OpenTime: "2026-01-02T15:30:00"},
		{Date: "2026-01-02", DishID: "tea", Portions: 0, CheckID: "c2", OpenTime: "2026-01-02T15:30:00"},
	}
	got := BuildOrders(sales, loc)
	if len(got) != 3 {
		t.Fatalf("три чека — три заказа: %+v", got)
	}
	want := []string{"2026-01-02T12:05:00+03:00", "2026-01-02T15:30:00+03:00", "2026-01-02T19:40:00+03:00"}
	for i, o := range got {
		if o.OccurredAt != want[i] || o.Source != "pos" {
			t.Fatalf("заказ %d: %+v", i, o)
		}
	}
	if got[0].ID != CheckOrderID("c1") || len(got[0].Items) != 2 || got[0].Items[0].DishID != "pilaf" {
		t.Fatalf("чек c1: %+v", got[0])
	}
	// Повтор синхронизации (в другом порядке строк) — те же id.
	again := BuildOrders([]Sale{sales[3], sales[2], sales[1], sales[0]}, loc)
	for i := range got {
		if again[i].ID != got[i].ID {
			t.Fatalf("id не стабильны: %s ≠ %s", again[i].ID, got[i].ID)
		}
	}
}

func TestBuildOrdersWithoutCheck(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	sales := []Sale{
		{Date: "2026-01-02", DishID: "soup", Portions: 2, OpenTime: "2026-01-02T13:00:00"},
		{Date: "2026-01-02", DishID: "soup", Portions: 1, OpenTime: "2026-01-02T14:00:00"},
		{Date: "2026-01-02", DishID: "pilaf", Portions: 1},
	}
	got := BuildOrders(sales, loc)
	if len(got) != 3 || got[0].ID == got[1].ID || got[1].ID == got[2].ID {
		t.Fatalf("заказ на позицию: %+v", got)
	}
	if got[0].OccurredAt != "" { // без времени — раньше остальных и без occurred_at
		t.Fatalf("время выдумано: %+v", got[0])
	}
	if got[1].OccurredAt != "2026-01-02T13:00:00+03:00" {
		t.Fatalf("%+v", got[1])
	}
}
