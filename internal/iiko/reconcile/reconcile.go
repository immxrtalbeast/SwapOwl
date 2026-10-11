// Package reconcile сверяет остатки нашего склада с остатками iiko (теневой режим, solution.md 8.4).
// Только сравнение: ничего не читает по сети и ничего не корректирует.
package reconcile

import (
	"fmt"
	"io"
	"math"
	"sort"
	"text/tabwriter"

	"umirhack/internal/iiko"
	"umirhack/internal/iiko/mapping"
)

// Ours — остаток продукта у нас (GET /api/products): целые базовые единицы.
type Ours struct {
	ID   string
	Name string
	Qty  int64
}

// Row — строка сверки. Количества в базовых единицах (g, ml, pcs); Diff = Ours − Iiko.
type Row struct {
	ProductID string `json:"product_id"`
	Name      string `json:"name"`
	Unit      string `json:"unit"`
	Ours      int64  `json:"ours"`
	Iiko      int64  `json:"iiko"`
	Diff      int64  `json:"diff"`
	// DiffMinor — разница в копейках по цене iiko: Sum/Amount остатка, а при нулевом остатке —
	// цена последней накладной. Ноль, если цены нет.
	DiffMinor int64 `json:"diff_minor"`
}

// Compare строит строки по всем продуктам, известным хотя бы одной стороне; порядок — по id.
// Продукт, которого нет в остатках iiko, считается нулевым остатком iiko.
// prices — руб за основную единицу (mapping.Prices), запасной источник цены.
func Compare(ours []Ours, balances []iiko.BalanceItem, products []iiko.Product, prices map[string]float64) ([]Row, error) {
	type side struct {
		name   string
		unit   mapping.Unit
		hasU   bool
		ours   int64
		iiko   float64
		sum    float64
		inBoth [2]bool
	}
	m := map[string]*side{}
	get := func(id string) *side {
		if s, ok := m[id]; ok {
			return s
		}
		s := &side{}
		m[id] = s
		return s
	}
	for _, p := range products {
		if u, err := mapping.UnitOf(p.MainUnit); err == nil {
			s := get(p.ID)
			s.name, s.unit, s.hasU = p.Name, u, true
		}
	}
	for _, o := range ours {
		s := get(o.ID)
		s.ours, s.inBoth[0] = o.Qty, true
		if s.name == "" {
			s.name = o.Name
		}
	}
	for _, b := range balances {
		s := get(b.Product)
		s.iiko += b.Amount
		s.sum += b.Sum
		s.inBoth[1] = true
	}
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rows := make([]Row, 0, len(ids))
	for _, id := range ids {
		s := m[id]
		if !s.hasU {
			return nil, fmt.Errorf("продукт %q: нет в справочнике iiko, единица неизвестна", id)
		}
		iikoQty := int64(math.Round(s.iiko * float64(s.unit.Factor)))
		r := Row{ProductID: id, Name: s.name, Unit: s.unit.Base, Ours: s.ours, Iiko: iikoQty, Diff: s.ours - iikoQty}
		price := prices[id] // руб за основную единицу
		if s.iiko > 0 {
			price = s.sum / s.iiko
		}
		r.DiffMinor = int64(math.Round(float64(r.Diff) / float64(s.unit.Factor) * price * 100))
		rows = append(rows, r)
	}
	return rows, nil
}

// Discrepancies оставляет строки с ненулевой разницей.
func Discrepancies(rows []Row) []Row {
	out := []Row{}
	for _, r := range rows {
		if r.Diff != 0 {
			out = append(out, r)
		}
	}
	return out
}

// Rubles форматирует копейки как «±123.45».
func Rubles(minor int64) string {
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	return fmt.Sprintf("%s%d.%02d", sign, minor/100, minor%100)
}

// WriteTable печатает таблицу: продукт, у нас, в iiko, разница, разница в рублях.
func WriteTable(w io.Writer, rows []Row) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ПРОДУКТ\tУ НАС\tВ IIKO\tРАЗНИЦА\tРАЗНИЦА, ₽")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s (%s)\t%d %s\t%d %s\t%+d %s\t%s\n", r.Name, r.ProductID, r.Ours, r.Unit, r.Iiko, r.Unit, r.Diff, r.Unit, Rubles(r.DiffMinor))
	}
	return tw.Flush()
}
