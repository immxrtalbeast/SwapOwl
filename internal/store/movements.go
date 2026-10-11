package store

import (
	"context"
	"fmt"
	"time"

	"umirhack/internal/domain"
)

// Пределы GET /api/movements?limit=.
const (
	DefaultMovementsLimit = 100
	MaxMovementsLimit     = 1000
)

// MovementView — строка журнала движений GET /api/movements.
type MovementView struct {
	ID        int64     `json:"id"`
	BatchID   int64     `json:"batch_id"`
	ProductID string    `json:"product_id"`
	Kind      string    `json:"kind"`
	Reason    *string   `json:"reason"`
	Quantity  QtyJSON   `json:"quantity"`
	OrderID   *string   `json:"order_id"`
	At        time.Time `json:"at"`
}

// ListMovements — журнал движений, новые первыми: (at, id) по убыванию. productID и kind —
// необязательные фильтры; limit в 1…MaxMovementsLimit, 0 — DefaultMovementsLimit.
func (d *DB) ListMovements(ctx context.Context, productID, kind string, limit int) ([]MovementView, error) {
	if limit <= 0 {
		limit = DefaultMovementsLimit
	}
	if limit > MaxMovementsLimit {
		limit = MaxMovementsLimit
	}
	rows, err := d.Pool.Query(ctx, `
		SELECT m.id, m.batch_id, m.product_id, m.kind, m.reason, m.quantity, m.order_id::text, m.at, p.unit
		FROM movements m JOIN products p ON p.id = m.product_id
		WHERE ($1 = '' OR m.product_id = $1) AND ($2 = '' OR m.kind = $2)
		ORDER BY m.at DESC, m.id DESC LIMIT $3`, productID, kind, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list movements: %w", err)
	}
	defer rows.Close()
	out := []MovementView{}
	for rows.Next() {
		var m MovementView
		var u string
		if err := rows.Scan(&m.ID, &m.BatchID, &m.ProductID, &m.Kind, &m.Reason, &m.Quantity.Qty, &m.OrderID, &m.At, &u); err != nil {
			return nil, fmt.Errorf("store: list movements: %w", err)
		}
		m.Quantity.Unit = domain.Unit(u)
		out = append(out, m)
	}
	return out, rows.Err()
}
