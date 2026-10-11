package store

import (
	"context"
	"fmt"
	"time"
)

// MoneyRow — строка отчёта «Деньги»: продукт (и причина для списаний) за период.
type MoneyRow struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	Reason      string  `json:"reason,omitempty"`
	Quantity    QtyJSON `json:"quantity"`
	CostMinor   int64   `json:"cost_minor"`
}

// MoneyDay — потери за один календарный день ресторана.
type MoneyDay struct {
	Date      string `json:"date"`
	CostMinor int64  `json:"cost_minor"`
}

// WriteoffsReport — ответ GET /api/analytics/writeoffs. Деньги — копейки (bigint, 7.1):
// стоимость группы = floor(Σ quantity × price_minor_per_1000 / 1000). TotalMinor — сумма строк
// ByProduct; ByDay округляется по дням отдельно, поэтому его сумма может отличаться на копейки.
type WriteoffsReport struct {
	From       time.Time  `json:"from"`
	To         time.Time  `json:"to"`
	TotalMinor int64      `json:"total_minor"`
	ByProduct  []MoneyRow `json:"by_product"`
	ByDay      []MoneyDay `json:"by_day"`
}

// ShortfallsReport — ответ GET /api/analytics/shortfalls: расхождения кассы (продажа из кассы
// без остатка, 2.3а) по продуктам. Стоимость считается так же, как у списаний.
type ShortfallsReport struct {
	From       time.Time  `json:"from"`
	To         time.Time  `json:"to"`
	TotalMinor int64      `json:"total_minor"`
	ByProduct  []MoneyRow `json:"by_product"`
	ByDay      []MoneyDay `json:"by_day"`
}

// WriteoffsReport — списания (movements.kind = 'writeoff') за [from, to) по продуктам и причинам.
// Причина NULL показывается как «other». Дни — по часовому поясу loc.
func (d *DB) WriteoffsReport(ctx context.Context, from, to time.Time, loc *time.Location) (WriteoffsReport, error) {
	rep := WriteoffsReport{From: from.UTC(), To: to.UTC(), ByProduct: []MoneyRow{}, ByDay: []MoneyDay{}}
	var err error
	rep.ByProduct, rep.ByDay, rep.TotalMinor, err = d.moneyReport(ctx, "movements m", "m.kind = 'writeoff'", "m.at", true, from, to, loc)
	if err != nil {
		return rep, fmt.Errorf("store: writeoffs report: %w", err)
	}
	return rep, nil
}

// ShortfallsReport — расхождения кассы из shortfalls за [from, to) по продуктам.
func (d *DB) ShortfallsReport(ctx context.Context, from, to time.Time, loc *time.Location) (ShortfallsReport, error) {
	rep := ShortfallsReport{From: from.UTC(), To: to.UTC()}
	var err error
	rep.ByProduct, rep.ByDay, rep.TotalMinor, err = d.moneyReport(ctx, "shortfalls m", "true", "m.at", false, from, to, loc)
	if err != nil {
		return rep, fmt.Errorf("store: shortfalls report: %w", err)
	}
	return rep, nil
}

// moneyReport — общий расчёт: источник (таблица с product_id, quantity, at), фильтр и колонка времени
// приходят константами из этого файла, не от пользователя.
func (d *DB) moneyReport(ctx context.Context, source, where, atCol string, byReason bool, from, to time.Time, loc *time.Location) ([]MoneyRow, []MoneyDay, int64, error) {
	if loc == nil {
		loc = time.UTC
	}
	reason, group := "''", "m.product_id, p.name, p.unit"
	if byReason {
		reason = "COALESCE(m.reason, 'other')"
		group += ", " + reason
	}
	rows, err := d.Pool.Query(ctx, fmt.Sprintf(`
		SELECT m.product_id, p.name, p.unit, %[1]s AS reason, SUM(m.quantity)::bigint,
		       div(SUM(m.quantity::numeric * p.price_minor_per_1000), 1000)::bigint
		FROM %[2]s JOIN products p ON p.id = m.product_id
		WHERE %[3]s AND %[4]s >= $1 AND %[4]s < $2
		GROUP BY %[5]s
		ORDER BY 6 DESC, m.product_id, 4`, reason, source, where, atCol, group), from.UTC(), to.UTC())
	if err != nil {
		return nil, nil, 0, err
	}
	defer rows.Close()
	byProduct := []MoneyRow{}
	var total int64
	for rows.Next() {
		var r MoneyRow
		if err := rows.Scan(&r.ProductID, &r.ProductName, &r.Quantity.Unit, &r.Reason, &r.Quantity.Qty, &r.CostMinor); err != nil {
			return nil, nil, 0, err
		}
		total += r.CostMinor
		byProduct = append(byProduct, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, 0, err
	}
	rows.Close()

	drows, err := d.Pool.Query(ctx, fmt.Sprintf(`
		SELECT to_char((%[3]s AT TIME ZONE $3), 'YYYY-MM-DD') AS day,
		       div(SUM(m.quantity::numeric * p.price_minor_per_1000), 1000)::bigint
		FROM %[1]s JOIN products p ON p.id = m.product_id
		WHERE %[2]s AND %[3]s >= $1 AND %[3]s < $2
		GROUP BY 1 ORDER BY 1`, source, where, atCol), from.UTC(), to.UTC(), loc.String())
	if err != nil {
		return nil, nil, 0, err
	}
	defer drows.Close()
	byDay := []MoneyDay{}
	for drows.Next() {
		var day MoneyDay
		if err := drows.Scan(&day.Date, &day.CostMinor); err != nil {
			return nil, nil, 0, err
		}
		byDay = append(byDay, day)
	}
	return byProduct, byDay, total, drows.Err()
}
