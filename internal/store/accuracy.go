package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/domain"
)

// DishAccuracyJSON — точность прогноза спроса по блюду.
type DishAccuracyJSON struct {
	DishID          string  `json:"dish_id"`
	DishName        string  `json:"dish_name"`
	ActualPerDay    float64 `json:"actual_per_day"`
	PredictedPerDay float64 `json:"predicted_per_day"`
	WAPEPct         float64 `json:"wape_pct"`
	BaselineWAPEPct float64 `json:"baseline_wape_pct"`
}

// AccuracyReport — GET /api/analytics/forecast-accuracy.
type AccuracyReport struct {
	Days            int                `json:"days"`
	DaysUsed        int                `json:"days_used"`
	EnoughData      bool               `json:"enough_data"`
	WAPEPct         float64            `json:"wape_pct"`
	BaselineWAPEPct float64            `json:"baseline_wape_pct"`
	Dishes          []DishAccuracyJSON `json:"dishes"`
}

// ForecastAccuracy — Snapshotter.ForecastAccuracy: бэктест спроса с параметрами Snapshotter.
func (s Snapshotter) ForecastAccuracy(ctx context.Context, now time.Time, days int) (AccuracyReport, error) {
	return s.DB.ForecastAccuracy(ctx, now, days, s.Params)
}

// ForecastAccuracy читает приоры, историю заказов и прыжки часов одной читающей транзакцией и сравнивает прогноз
// спроса (domain.DishDemand на начало каждых прошедших суток) с фактом. Ничего не хранится и не пишется.
func (d *DB) ForecastAccuracy(ctx context.Context, now time.Time, days int, p SnapshotParams) (AccuracyReport, error) {
	now = now.UTC()
	fail := func(err error) (AccuracyReport, error) {
		return AccuracyReport{}, fmt.Errorf("store: forecast accuracy: %w", err)
	}
	tx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fail(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	names := map[string]string{}
	priors := map[string]float64{}
	rows, err := tx.Query(ctx, `SELECT id, name, prior_daily::float8 FROM dishes ORDER BY name, id`)
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		var id, name string
		var prior float64
		if err := rows.Scan(&id, &name, &prior); err != nil {
			rows.Close()
			return fail(err)
		}
		names[id], priors[id] = name, prior
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fail(err)
	}

	// История целиком до now, как в Snapshot: окно оценки переходит через прыжки часов назад на суммарную длину
	// прыжков, поэтому нижняя граница по числу суток обрезала бы заказы и занижала бы λ.
	var history []domain.OrderedDish
	hr, err := tx.Query(ctx, `SELECT o.at, i.dish_id, i.portions, o.status = 'rejected' FROM orders o JOIN order_items i ON i.order_id = o.id
		WHERE o.at <= $1 ORDER BY o.at, o.id, i.dish_id`, now)
	if err != nil {
		return fail(err)
	}
	for hr.Next() {
		var o domain.OrderedDish
		if err := hr.Scan(&o.At, &o.DishID, &o.Portions, &o.Rejected); err != nil {
			hr.Close()
			return fail(err)
		}
		history = append(history, o)
	}
	hr.Close()
	if err := hr.Err(); err != nil {
		return fail(err)
	}

	var jumps []domain.TimeJump
	jr, err := tx.Query(ctx, `SELECT from_at, to_at FROM clock_jumps ORDER BY from_at`)
	if err != nil {
		return fail(err)
	}
	for jr.Next() {
		var j domain.TimeJump
		if err := jr.Scan(&j.From, &j.To); err != nil {
			jr.Close()
			return fail(err)
		}
		jumps = append(jumps, j)
	}
	jr.Close()
	if err := jr.Err(); err != nil {
		return fail(err)
	}

	// Как в Snapshot: окно не заходит раньше первого заказа или первой партии.
	var since *time.Time
	if err := tx.QueryRow(ctx, `SELECT LEAST((SELECT MIN(at) FROM orders), (SELECT MIN(received_at) FROM batches))`).Scan(&since); err != nil {
		return fail(err)
	}
	in := domain.BacktestInput{Now: now, Days: days, WindowDays: p.WindowDays, PriorDays: p.PriorDays,
		Priors: priors, History: history, Jumps: jumps}
	if since != nil {
		in.Since = since.Add(-time.Microsecond)
	}
	res, err := domain.DemandBacktest(in)
	if err != nil {
		return fail(err)
	}
	rep := AccuracyReport{Days: days, DaysUsed: res.DaysUsed, EnoughData: res.EnoughData,
		WAPEPct: res.WAPEPct, BaselineWAPEPct: res.BaselineWAPEPct, Dishes: []DishAccuracyJSON{}}
	for _, r := range res.Dishes {
		if r.ActualPerDay == 0 && r.PredictedPerDay == 0 {
			continue
		}
		rep.Dishes = append(rep.Dishes, DishAccuracyJSON{DishID: r.DishID, DishName: names[r.DishID],
			ActualPerDay: round1f(r.ActualPerDay), PredictedPerDay: round1f(r.PredictedPerDay),
			WAPEPct: r.WAPEPct, BaselineWAPEPct: r.BaselineWAPEPct})
	}
	// Больше всего ошибаются — сверху: именно там прогнозу есть куда расти.
	sort.SliceStable(rep.Dishes, func(i, j int) bool { return rep.Dishes[i].WAPEPct > rep.Dishes[j].WAPEPct })
	return rep, nil
}

func round1f(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }
