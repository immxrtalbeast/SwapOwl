package store

import (
	"context"
	"testing"
	"time"

	"umirhack/internal/domain"
)

// Продукт с тремя партиями: ок, истекает, просрочена. Просрочена не попадает ни в одно поле.
func TestListProductStockExpiring(t *testing.T) {
	db, _ := stockDB(t)
	ctx := context.Background()
	db.Expiry = domain.ExpiryConfig{Share: 0.2, MinHours: 12, MaxShare: 0.5}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO deliveries (id, number, created_at, received_at, body_hash, response) VALUES ('D1','1',$1,$1,'\x00','{}')`, t0); err != nil {
		t.Fatal(err)
	}
	ins := func(line int, rem int64, recv, exp time.Time) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, `INSERT INTO batches (delivery_id, line_no, product_id, quantity, remaining, received_at, expires_at)
			VALUES ('D1', $1, 'salmon', 10000, $2, $3, $4)`, line, rem, recv, exp); err != nil {
			t.Fatal(err)
		}
	}
	ins(1, 4000, t0.Add(-time.Hour), t0.Add(100*time.Hour))  // ок
	ins(2, 1500, t0.Add(-48*time.Hour), t0.Add(5*time.Hour)) // истекает: осталось 5 ч < 12 ч
	ins(3, 700, t0.Add(-72*time.Hour), t0.Add(-1*time.Hour)) // просрочена, ещё не списана

	stock, err := db.ListProductStock(ctx, t0)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ProductStock{}
	for _, s := range stock {
		byID[s.ID] = s
	}
	s := byID["salmon"]
	if s.Available.Qty != 5500 || s.Expiring.Qty != 1500 || s.Expiring.Unit != "g" || s.BatchesCount != 2 ||
		s.NearestExpiresAt == nil || !s.NearestExpiresAt.Equal(t0.Add(5*time.Hour)) {
		t.Fatalf("salmon: %+v", s)
	}
	if c := byID["cream"]; c.Expiring.Qty != 0 || c.Available.Qty != 0 || c.BatchesCount != 0 || c.NearestExpiresAt != nil {
		t.Fatalf("cream: %+v", c)
	}
}
