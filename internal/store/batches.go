package store

import (
	"context"
	"fmt"
	"time"

	"umirhack/internal/domain"
)

// ProductStock — строка GET /api/products. Доступно = remaining > 0 AND now < expires_at.
type ProductStock struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	Unit             domain.Unit `json:"unit"`
	Available        QtyJSON     `json:"available"`
	BatchesCount     int         `json:"batches_count"`
	NearestExpiresAt *time.Time  `json:"nearest_expires_at"`
}

// BatchView — строка GET /api/batches.
type BatchView struct {
	ID                int64     `json:"id"`
	ProductID         string    `json:"product_id"`
	DeliveryID        string    `json:"delivery_id"`
	DeliveryCreatedAt time.Time `json:"delivery_created_at"`
	Quantity          QtyJSON   `json:"quantity"`
	Remaining         QtyJSON   `json:"remaining"`
	ReceivedAt        time.Time `json:"received_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	TimeLeftSeconds   int64     `json:"time_left_seconds"`
	Status            string    `json:"status"`
}

// ListProductStock — остатки по продуктам на момент now (время берёт вызывающий из clock).
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
	defer rows.Close()
	out := []ProductStock{}
	for rows.Next() {
		var s ProductStock
		var u string
		if err := rows.Scan(&s.ID, &s.Name, &u, &s.Available.Qty, &s.BatchesCount, &s.NearestExpiresAt); err != nil {
			return nil, fmt.Errorf("store: list products: %w", err)
		}
		s.Unit, s.Available.Unit = domain.Unit(u), domain.Unit(u)
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListBatches — партии без списанных в ноль, порядок (expires_at, id).
// productID и status ("ok" | "expired") — необязательные фильтры.
func (d *DB) ListBatches(ctx context.Context, now time.Time, productID, status string) ([]BatchView, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT b.id, b.product_id, b.delivery_id, dl.created_at, b.quantity, b.remaining,
		       b.received_at, b.expires_at, p.unit
		FROM batches b
		JOIN deliveries dl ON dl.id = b.delivery_id
		JOIN products p ON p.id = b.product_id
		WHERE b.remaining > 0
		  AND ($2 = '' OR b.product_id = $2)
		  AND ($3 = '' OR ($3 = 'ok' AND b.expires_at > $1) OR ($3 = 'expired' AND b.expires_at <= $1))
		ORDER BY b.expires_at, b.id`, now, productID, status)
	if err != nil {
		return nil, fmt.Errorf("store: list batches: %w", err)
	}
	defer rows.Close()
	out := []BatchView{}
	for rows.Next() {
		var b BatchView
		var u string
		if err := rows.Scan(&b.ID, &b.ProductID, &b.DeliveryID, &b.DeliveryCreatedAt, &b.Quantity.Qty,
			&b.Remaining.Qty, &b.ReceivedAt, &b.ExpiresAt, &u); err != nil {
			return nil, fmt.Errorf("store: list batches: %w", err)
		}
		b.Quantity.Unit, b.Remaining.Unit = domain.Unit(u), domain.Unit(u)
		b.TimeLeftSeconds = int64(b.ExpiresAt.Sub(now) / time.Second)
		b.Status = "ok"
		if !now.Before(b.ExpiresAt) {
			b.Status = "expired"
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
