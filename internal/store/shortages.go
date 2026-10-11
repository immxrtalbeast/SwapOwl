package store

import (
	"context"
	"fmt"
	"time"
)

// ShortageRow — продукт, которого не хватило отклонённым заказам: сколько заказов он завалил
// и сколько в сумме не хватило.
type ShortageRow struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	Orders      int64   `json:"rejected_orders"`
	Missing     QtyJSON `json:"missing"`
}

// ShortagesReport — ответ GET /api/analytics/shortages: дефицит по отклонённым заказам (api-заказ
// отклоняется целиком, 2.3а). Расхождения кассы сюда не входят — они в ShortfallsReport.
type ShortagesReport struct {
	From           time.Time     `json:"from"`
	To             time.Time     `json:"to"`
	RejectedOrders int64         `json:"rejected_orders"`
	ByProduct      []ShortageRow `json:"by_product"`
}

// ShortagesReport — отклонённые заказы за [from, to), разобранные по недостающим продуктам
// (orders.response->'missing'). Порядок: больше отклонённых заказов, затем больше недостающего, затем id.
func (d *DB) ShortagesReport(ctx context.Context, from, to time.Time) (ShortagesReport, error) {
	rep := ShortagesReport{From: from.UTC(), To: to.UTC(), ByProduct: []ShortageRow{}}
	if err := d.Pool.QueryRow(ctx,
		`SELECT count(*) FROM orders WHERE status = 'rejected' AND at >= $1 AND at < $2`,
		from.UTC(), to.UTC()).Scan(&rep.RejectedOrders); err != nil {
		return rep, fmt.Errorf("store: shortages report: %w", err)
	}
	rows, err := d.Pool.Query(ctx, `
		SELECT m->>'product_id', COALESCE(p.name, m->>'product_name'), COALESCE(p.unit, m->'missing'->>'unit'),
		       count(*), SUM((m->'missing'->>'qty')::bigint)::bigint
		FROM orders o, jsonb_array_elements(o.response->'missing') m
		LEFT JOIN products p ON p.id = m->>'product_id'
		WHERE o.status = 'rejected' AND o.at >= $1 AND o.at < $2
		GROUP BY 1, 2, 3
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
