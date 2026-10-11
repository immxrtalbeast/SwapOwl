package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestShortagesReport(t *testing.T) {
	db, _ := stockDB(t)
	ctx := context.Background()
	ord := func(id, status, at, resp string) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, `INSERT INTO orders (id, body_hash, status, at, response) VALUES ($1, '\x00', $2, $3, $4::jsonb)`,
			id, status, at, resp); err != nil {
			t.Fatal(err)
		}
	}
	miss := func(p string, q int) string {
		return `{"product_id":"` + p + `","missing":{"qty":` + itoa(q) + `,"unit":"g"}}`
	}
	ord("00000000-0000-0000-0000-000000000001", "rejected", "2026-10-05T10:00:00Z", `{"missing":[`+miss("salmon", 300)+`,`+miss("cream", 50)+`]}`)
	ord("00000000-0000-0000-0000-000000000002", "rejected", "2026-10-06T10:00:00Z", `{"missing":[`+miss("salmon", 200)+`]}`)
	ord("00000000-0000-0000-0000-000000000003", "accepted", "2026-10-06T11:00:00Z", `{"missing":[]}`)
	ord("00000000-0000-0000-0000-000000000004", "rejected", "2026-10-20T10:00:00Z", `{"missing":[`+miss("salmon", 999)+`]}`)

	rep, err := db.ShortagesReport(ctx, time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.ByProduct) != 2 {
		t.Fatalf("rows: %+v", rep.ByProduct)
	}
	if r := rep.ByProduct[0]; r.ProductID != "salmon" || r.Orders != 2 || r.Missing.Qty != 500 {
		t.Fatalf("first: %+v", r)
	}
	if r := rep.ByProduct[1]; r.ProductID != "cream" || r.Orders != 1 || r.Missing.Qty != 50 {
		t.Fatalf("second: %+v", r)
	}
}

func itoa(n int) string { return fmt.Sprint(n) }
