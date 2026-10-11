package reconcile

import (
	"bytes"
	"strings"
	"testing"

	"umirhack/internal/iiko"
)

func TestCompare(t *testing.T) {
	products := []iiko.Product{
		{ID: "flour", Name: "Мука", MainUnit: "кг"},
		{ID: "egg", Name: "Яйцо", MainUnit: "шт"},
		{ID: "milk", Name: "Молоко", MainUnit: "л"},
	}
	balances := []iiko.BalanceItem{
		{Product: "flour", Amount: 2.5, Sum: 125}, // 50 ₽/кг
		{Product: "egg", Amount: 10, Sum: 100},
		{Product: "milk", Amount: 0, Sum: 0},
	}
	ours := []Ours{{"flour", "Мука", 2000}, {"egg", "Яйцо", 10}, {"milk", "Молоко", 1000}}
	rows, err := Compare(ours, balances, products, map[string]float64{"milk": 80})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Row{}
	for _, r := range rows {
		byID[r.ProductID] = r
	}
	if r := byID["flour"]; r.Diff != -500 || r.DiffMinor != -2500 || r.Iiko != 2500 {
		t.Fatalf("flour: %+v", r)
	}
	if r := byID["egg"]; r.Diff != 0 {
		t.Fatalf("egg: %+v", r)
	}
	if r := byID["milk"]; r.Diff != 1000 || r.DiffMinor != 8000 { // цена из накладной
		t.Fatalf("milk: %+v", r)
	}
	d := Discrepancies(rows)
	if len(d) != 2 {
		t.Fatalf("расхождений %d", len(d))
	}
	var buf bytes.Buffer
	if err := WriteTable(&buf, d); err != nil || !strings.Contains(buf.String(), "-25.00") {
		t.Fatalf("таблица: %v\n%s", err, buf.String())
	}
}

func TestCompareUnknownProduct(t *testing.T) {
	if _, err := Compare([]Ours{{"x", "X", 1}}, nil, nil, nil); err == nil {
		t.Fatal("ждали ошибку")
	}
}
