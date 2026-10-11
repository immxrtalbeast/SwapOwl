package store

import (
	"context"
	"testing"
	"time"
)

func TestWriteoffsAndShortfallsReports(t *testing.T) {
	db, _ := stockDB(t)
	ctx := context.Background()
	for _, q := range []string{
		`UPDATE products SET price_minor_per_1000 = 1500 WHERE id = 'salmon'`, // 15 ₽ за 1000 г
		`UPDATE products SET price_minor_per_1000 = 333 WHERE id = 'cream'`,
		`INSERT INTO deliveries (id, number, created_at, received_at, body_hash, response) VALUES ('D1','1','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z','\x00','{}')`,
	} {
		if _, err := db.Pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	// Партии и движения вставляем напрямую: отчёт читает только movements и shortfalls.
	var b1, b2 int64
	ins := `INSERT INTO batches (delivery_id, line_no, product_id, quantity, remaining, received_at, expires_at)
	        VALUES ('D1', $1, $2, 10000, 0, '2026-10-01T00:00:00Z', '2026-10-05T00:00:00Z') RETURNING id`
	if err := db.Pool.QueryRow(ctx, ins, 1, "salmon").Scan(&b1); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, ins, 2, "cream").Scan(&b2); err != nil {
		t.Fatal(err)
	}
	mv := func(batch int64, prod, kind string, reason any, qty int64, at string) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, `INSERT INTO movements (batch_id, product_id, kind, reason, quantity, at) VALUES ($1,$2,$3,$4,$5,$6)`,
			batch, prod, kind, reason, qty, at); err != nil {
			t.Fatal(err)
		}
	}
	mv(b1, "salmon", "writeoff", "expired", 2500, "2026-10-05T10:00:00Z") // 2500×1500/1000 = 3750
	mv(b1, "salmon", "writeoff", "expired", 1000, "2026-10-06T10:00:00Z") // 1500
	mv(b1, "salmon", "writeoff", "spoiled", 100, "2026-10-06T11:00:00Z")  // 150
	mv(b2, "cream", "writeoff", nil, 1000, "2026-10-06T12:00:00Z")        // 333, причина other
	mv(b1, "salmon", "sale", nil, 5000, "2026-10-06T12:00:00Z")           // продажа — не списание
	mv(b1, "salmon", "writeoff", "expired", 9000, "2026-10-20T00:00:00Z") // вне периода

	from := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	rep, err := db.WriteoffsReport(ctx, from, to, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if rep.TotalMinor != 3750+1500+150+333 || len(rep.ByProduct) != 3 {
		t.Fatalf("writeoffs: %+v", rep)
	}
	if first := rep.ByProduct[0]; first.ProductID != "salmon" || first.Reason != "expired" || first.Quantity.Qty != 3500 || first.CostMinor != 5250 {
		t.Fatalf("first row: %+v", first)
	}
	if len(rep.ByDay) != 2 || rep.ByDay[0].Date != "2026-10-05" || rep.ByDay[0].CostMinor != 3750 ||
		rep.ByDay[1].Date != "2026-10-06" || rep.ByDay[1].CostMinor != 1500+150+333 {
		t.Fatalf("by day: %+v", rep.ByDay)
	}
	var other bool
	for _, r := range rep.ByProduct {
		other = other || (r.ProductID == "cream" && r.Reason == "other")
	}
	if !other {
		t.Fatalf("NULL reason must be 'other': %+v", rep.ByProduct)
	}

	// Часовой пояс сдвигает день: 22:00Z 6 октября в Москве уже 7-е.
	mv(b1, "salmon", "writeoff", "expired", 1000, "2026-10-06T22:00:00Z")
	msk := time.FixedZone("MSK", 3*3600)
	rep, err = db.WriteoffsReport(ctx, from, to, msk)
	if err != nil {
		t.Fatal(err)
	}
	if got := rep.ByDay[len(rep.ByDay)-1]; got.Date != "2026-10-07" || got.CostMinor != 1500 {
		t.Fatalf("tz day: %+v", rep.ByDay)
	}

	// Расхождения кассы.
	if _, err := db.Pool.Exec(ctx, `INSERT INTO orders (id, body_hash, status, at, response, source) VALUES ('`+o1+`','\x00','accepted','2026-10-06T00:00:00Z','{}','pos')`); err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		prod string
		qty  int64
		at   string
	}{{"salmon", 700, "2026-10-06T09:00:00Z"}, {"cream", 3, "2026-10-30T09:00:00Z"}} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO shortfalls (order_id, product_id, quantity, at) VALUES ($1,$2,$3,$4)`, o1, s.prod, s.qty, s.at); err != nil {
			t.Fatal(err)
		}
	}
	sr, err := db.ShortfallsReport(ctx, from, to, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if sr.TotalMinor != 1050 || len(sr.ByProduct) != 1 || sr.ByProduct[0].Quantity.Qty != 700 || len(sr.ByDay) != 1 {
		t.Fatalf("shortfalls: %+v", sr)
	}
}
