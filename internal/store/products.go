package store

import (
	"context"
	"fmt"
	"time"

	"umirhack/internal/domain"
)

// ProductStock — строка GET /api/products (.ai/solution.md, 7.4). Доступно = remaining > 0 AND
// now < expires_at; Expiring — сумма остатков доступных партий в статусе «истекает».
type ProductStock struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	Unit             domain.Unit `json:"unit"`
	Available        QtyJSON     `json:"available"`
	Expiring         QtyJSON     `json:"expiring"`
	BatchesCount     int         `json:"batches_count"`
	NearestExpiresAt *time.Time  `json:"nearest_expires_at"`
}

// ListProductStock — остатки по продуктам на момент now (время берёт вызывающий из clock).
// Статус «истекает» считает domain.Status с d.Expiry.
func (d *DB) ListProductStock(ctx context.Context, now time.Time) ([]ProductStock, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT p.id, p.name, p.unit,
		       COALESCE(SUM(b.remaining), 0)::bigint, count(b.id)::int, MIN(b.expires_at)
		FROM products p
		LEFT JOIN batches b ON b.product_id = p.id AND b.remaining > 0 AND b.expires_at > $1
		GROUP BY p.id ORDER BY p.name, p.id`, now)
	if err != nil {
		return nil, fmt.Errorf("store: list products: %w", err)
	}
	out := []ProductStock{}
	idx := map[string]int{}
	for rows.Next() {
		var s ProductStock
		var u string
		if err := rows.Scan(&s.ID, &s.Name, &u, &s.Available.Qty, &s.BatchesCount, &s.NearestExpiresAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("store: list products: %w", err)
		}
		s.Unit, s.Available.Unit, s.Expiring.Unit = domain.Unit(u), domain.Unit(u), domain.Unit(u)
		idx[s.ID] = len(out)
		out = append(out, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list products: %w", err)
	}

	brows, err := d.Pool.Query(ctx, `
		SELECT product_id, remaining, received_at, expires_at FROM batches
		WHERE remaining > 0 AND expires_at > $1`, now)
	if err != nil {
		return nil, fmt.Errorf("store: list products: %w", err)
	}
	defer brows.Close()
	for brows.Next() {
		var pid string
		var b domain.Batch
		var rem int64
		if err := brows.Scan(&pid, &rem, &b.ReceivedAt, &b.ExpiresAt); err != nil {
			return nil, fmt.Errorf("store: list products: %w", err)
		}
		if domain.Status(b, now, d.Expiry) == domain.StatusExpiring {
			if i, ok := idx[pid]; ok {
				out[i].Expiring.Qty += rem
			}
		}
	}
	return out, brows.Err()
}
