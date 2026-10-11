package store

import (
	"context"
	"fmt"
	"time"

	"umirhack/internal/domain"
)

// AlertParams — параметры ленты предупреждений: порог «истекает», расписание и W_min «обречённой» партии.
type AlertParams struct {
	Expiry   domain.ExpiryConfig
	Schedule domain.Schedule
	WMin     float64
}

// AlertView — строка GET /api/alerts (.ai/solution.md, 7.4).
type AlertView struct {
	BatchID         int64     `json:"batch_id"`
	ProductID       string    `json:"product_id"`
	ProductName     string    `json:"product_name"`
	Remaining       QtyJSON   `json:"remaining"`
	ExpiresAt       time.Time `json:"expires_at"`
	TimeLeftSeconds int64     `json:"time_left_seconds"`
	Status          string    `json:"status"`
	Doomed          bool      `json:"doomed"`
}

// ListAlerts — лента предупреждений на момент now: истекающие партии с остатком и просроченные за
// последние 24 ч, у которых было что терять. Список — производная от партий и времени, как и
// статус: «дворник» для него не нужен. У просроченной remaining — потерянное количество
// (остаток плюс уже списанное по сроку).
func (d *DB) ListAlerts(ctx context.Context, now time.Time, p AlertParams) ([]AlertView, error) {
	now = now.UTC()
	// Отбор в SQL — верхняя граница порога «истекает» (точная проверка — в domain.BuildAlerts),
	// чтобы не читать все живые партии на каждый запрос.
	rows, err := d.Pool.Query(ctx, `
		SELECT b.id, b.product_id, pr.name, pr.unit, b.quantity,
		       b.remaining + COALESCE(w.lost, 0), b.received_at, b.expires_at
		FROM batches b
		JOIN products pr ON pr.id = b.product_id
		LEFT JOIN LATERAL (
			SELECT SUM(m.quantity)::bigint AS lost FROM movements m
			WHERE m.batch_id = b.id AND m.kind = 'writeoff' AND m.reason = 'expired'
		) w ON true
		WHERE (b.remaining > 0 AND b.expires_at > $1
		       AND b.expires_at - $1::timestamptz <
		           GREATEST($2::float8 * EXTRACT(EPOCH FROM b.expires_at - b.received_at), $3::float8 * 3600) * interval '1 second')
		   OR (b.expires_at <= $1 AND b.expires_at >= $1 - $4::bigint * interval '1 second'
		       AND b.remaining + COALESCE(w.lost, 0) > 0)
		ORDER BY b.expires_at, b.id`,
		now, p.Expiry.Share, p.Expiry.MinHours, int64(domain.AlertExpiredWindow/time.Second))
	if err != nil {
		return nil, fmt.Errorf("store: list alerts: %w", err)
	}
	defer rows.Close()
	var batches []domain.Batch
	type ref struct {
		name string
		unit domain.Unit
	}
	refs := map[int64]ref{}
	for rows.Next() {
		var b domain.Batch
		var q, rem int64
		var name, unit string
		if err := rows.Scan(&b.ID, &b.ProductID, &name, &unit, &q, &rem, &b.ReceivedAt, &b.ExpiresAt); err != nil {
			return nil, fmt.Errorf("store: list alerts: %w", err)
		}
		b.Quantity, b.Remaining = domain.Qty(q), domain.Qty(rem)
		batches = append(batches, b)
		refs[b.ID] = ref{name, domain.Unit(unit)}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list alerts: %w", err)
	}
	out := []AlertView{}
	for _, a := range domain.BuildAlerts(batches, now, p.Expiry, p.Schedule, p.WMin) {
		r := refs[a.BatchID]
		out = append(out, AlertView{
			BatchID: a.BatchID, ProductID: a.ProductID, ProductName: r.name,
			Remaining: QtyJSON{Qty: int64(a.Remaining), Unit: r.unit}, ExpiresAt: a.ExpiresAt,
			TimeLeftSeconds: int64(a.TimeLeft / time.Second), Status: string(a.Status), Doomed: a.Doomed,
		})
	}
	return out, nil
}

// Monitor привязывает параметры ленты к БД: то, что нужно слою API (api.Monitor).
type Monitor struct {
	DB     *DB
	Params AlertParams
}

// ListAlerts — лента предупреждений с параметрами Monitor.
func (m Monitor) ListAlerts(ctx context.Context, now time.Time) ([]AlertView, error) {
	return m.DB.ListAlerts(ctx, now, m.Params)
}

// ListMovements — журнал движений.
func (m Monitor) ListMovements(ctx context.Context, productID, kind string, limit int) ([]MovementView, error) {
	return m.DB.ListMovements(ctx, productID, kind, limit)
}
