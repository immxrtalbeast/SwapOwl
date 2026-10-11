package store

import (
	"context"
	"fmt"
	"time"
)

// ShortageRow — продукт, из-за нехватки которого отклонялись заказы за период.
type ShortageRow struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	Orders      int64   `json:"orders"`
	Missing     QtyJSON `json:"missing"`
}

// ShortagesReport — ответ GET /api/analytics/shortages.
type ShortagesReport struct {
	From      time.Time     `json:"from"`
	To        time.Time     `json:"to"`
	ByProduct []ShortageRow `json:"by_product"`
}

// ShortagesReport — продукты по числу отклонённых заказов и недостающему количеству за [from, to).
// Берётся missing из сохранённого ответа отклонённого заказа; заказ считается один раз на продукт.
func (d *DB) ShortagesReport(ctx context.Context, from, to time.Time) (ShortagesReport, error) {
	rep := ShortagesReport{From: from.UTC(), To: to.UTC(), ByProduct: []ShortageRow{}}
	rows, err := d.Pool.Query(ctx, `
		SELECT m->>'product_id', p.name, p.unit, COUNT(DISTINCT o.id), SUM((m->'missing'->>'qty')::bigint)::bigint
		FROM orders o
		CROSS JOIN LATERAL jsonb_array_elements(o.response->'missing') m
		JOIN products p ON p.id = m->>'product_id'
		WHERE o.status = 'rejected' AND o.at >= $1 AND o.at < $2
		GROUP BY m->>'product_id', p.name, p.unit
		ORDER BY 4 DESC, 5 DESC, 1`, from.UTC(), to.UTC())
	if err != nil {
		return rep, fmt.Errorf("store: shortages report: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r ShortageRow
		if err := rows.Scan(&r.ProductID, &r.ProductName, &r.Missing.Unit, &r.Orders, &r.Missing.Qty); err != nil {
			return rep, fmt.Errorf("store: shortages report: %w", err)
		}
		rep.ByProduct = append(rep.ByProduct, r)
	}
	if err := rows.Err(); err != nil {
		return rep, fmt.Errorf("store: shortages report: %w", err)
	}
	return rep, nil
}
