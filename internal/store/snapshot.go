package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/domain"
)

// SnapshotParams — параметры оценки спроса и расписание (конфиг: N, n0, OPEN_AT, CLOSE_AT, зона).
type SnapshotParams struct {
	WindowDays float64 // N
	PriorDays  float64 // n0 > 0
	Schedule   domain.Schedule
}

// Snapshot — общий согласованный снимок склада для чистых расчётов поверх БД: горячие предложения,
// предложение поставок, деньги под риском. Ничего не хранится: всё — функция БД и now.
type Snapshot struct {
	Now      time.Time
	Schedule domain.Schedule
	Batches  []domain.Batch        // доступные: remaining > 0 AND now < expires_at, порядок (expires_at, id)
	Dishes   []domain.Dish         // с рецептами, порядок (name, id)
	Products map[string]ProductRef // справочник продуктов
	Price    map[string]int64      // product_id → копеек за 1000 базовых единиц
	Stopped  map[string]bool       // блюда в стоп-листе
	Lambda   map[string]float64    // λ_d, порций в день
	RateDay  map[string]float64    // rate_p_день
	RateHour map[string]float64    // rate_p_час
}

// Available — доступный остаток продукта по снимку.
func (s Snapshot) Available() map[string]domain.Qty {
	out := map[string]domain.Qty{}
	for _, b := range s.Batches {
		out[b.ProductID] += b.Remaining
	}
	return out
}

// Snapshotter привязывает параметры к БД: то, что нужно слою API (api.HotOffersSource).
type Snapshotter struct {
	DB     *DB
	Params SnapshotParams
}

// Snapshot — снимок на момент now с параметрами Snapshotter.
func (s Snapshotter) Snapshot(ctx context.Context, now time.Time) (Snapshot, error) {
	return s.DB.Snapshot(ctx, now, s.Params)
}

// Snapshot читает всё одной читающей транзакцией (REPEATABLE READ, READ ONLY): партии, меню, цены,
// историю заказов и прыжки часов видны согласованно. λ_d — по окну N с n0 и вычетом clock_jumps.
func (d *DB) Snapshot(ctx context.Context, now time.Time, p SnapshotParams) (Snapshot, error) {
	now = now.UTC()
	snap := Snapshot{Now: now, Schedule: p.Schedule, Products: map[string]ProductRef{},
		Price: map[string]int64{}, Stopped: map[string]bool{}}
	fail := func(err error) (Snapshot, error) { return Snapshot{}, fmt.Errorf("store: snapshot: %w", err) }

	tx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fail(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// query выполняет запрос и вызывает scan на каждой строке.
	query := func(sql string, args []any, scan func(pgx.Rows) error) error {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}

	if err := query(`SELECT id, name, unit, price_minor_per_1000 FROM products ORDER BY id`, nil, func(r pgx.Rows) error {
		var id, name, u string
		var price int64
		if err := r.Scan(&id, &name, &u, &price); err != nil {
			return err
		}
		snap.Products[id] = ProductRef{Name: name, Unit: domain.Unit(u)}
		snap.Price[id] = price
		return nil
	}); err != nil {
		return fail(err)
	}

	if err := query(`SELECT id, product_id, quantity, remaining, received_at, expires_at FROM batches
		WHERE remaining > 0 AND expires_at > $1 ORDER BY expires_at, id`, []any{now}, func(r pgx.Rows) error {
		var b domain.Batch
		var q, rem int64
		if err := r.Scan(&b.ID, &b.ProductID, &q, &rem, &b.ReceivedAt, &b.ExpiresAt); err != nil {
			return err
		}
		b.Quantity, b.Remaining = domain.Qty(q), domain.Qty(rem)
		snap.Batches = append(snap.Batches, b)
		return nil
	}); err != nil {
		return fail(err)
	}

	priors := map[string]float64{}
	if err := query(`SELECT d.id, d.name, d.prior_daily::float8, r.product_id, r.quantity
		FROM dishes d JOIN recipe_items r ON r.dish_id = d.id
		ORDER BY d.name, d.id, r.product_id`, nil, func(r pgx.Rows) error {
		var id, name, pid string
		var prior float64
		var q int64
		if err := r.Scan(&id, &name, &prior, &pid, &q); err != nil {
			return err
		}
		if n := len(snap.Dishes); n == 0 || snap.Dishes[n-1].ID != id {
			snap.Dishes = append(snap.Dishes, domain.Dish{ID: id, Name: name})
			priors[id] = prior
		}
		last := &snap.Dishes[len(snap.Dishes)-1]
		last.Recipe = append(last.Recipe, domain.RecipeItem{ProductID: pid, Quantity: domain.Qty(q)})
		return nil
	}); err != nil {
		return fail(err)
	}

	// История спроса: порции в принятых и отклонённых заказах (2.6).
	var history []domain.OrderedDish
	if err := query(`SELECT o.at, i.dish_id, i.portions, o.status = 'rejected' FROM orders o JOIN order_items i ON i.order_id = o.id
		WHERE o.at <= $1 ORDER BY o.at, o.id, i.dish_id`, []any{now}, func(r pgx.Rows) error {
		var o domain.OrderedDish
		if err := r.Scan(&o.At, &o.DishID, &o.Portions, &o.Rejected); err != nil {
			return err
		}
		history = append(history, o)
		return nil
	}); err != nil {
		return fail(err)
	}

	var jumps []domain.TimeJump
	if err := query(`SELECT from_at, to_at FROM clock_jumps ORDER BY from_at`, nil, func(r pgx.Rows) error {
		var j domain.TimeJump
		if err := r.Scan(&j.From, &j.To); err != nil {
			return err
		}
		jumps = append(jumps, j)
		return nil
	}); err != nil {
		return fail(err)
	}

	// Начало истории стенда: окно не заходит раньше первого заказа или первой партии, иначе у свежего
	// экземпляра активные сутки были бы равны N и λ занижалась бы до доли априора.
	var since *time.Time
	if err := tx.QueryRow(ctx, `SELECT LEAST((SELECT MIN(at) FROM orders), (SELECT MIN(received_at) FROM batches))`).
		Scan(&since); err != nil {
		return fail(err)
	}
	in := domain.DemandInput{Now: now, WindowDays: p.WindowDays, PriorDays: p.PriorDays,
		Priors: priors, History: history, Jumps: jumps, Since: now}
	if since != nil {
		// DishDemand учитывает заказы строго позже начала окна, поэтому сдвигаем начало на микросекунду:
		// первый заказ истории не должен выпадать из оценки. Нет истории — нет активных суток, λ = априор.
		in.Since = since.Add(-time.Microsecond)
	}
	if snap.Lambda, err = domain.DishDemand(in); err != nil {
		return fail(err)
	}
	snap.RateDay = domain.ProductDemandPerDay(snap.Dishes, snap.Lambda)
	snap.RateHour = make(map[string]float64, len(snap.RateDay))
	for pid, perDay := range snap.RateDay {
		snap.RateHour[pid] = domain.RatePerHour(perDay, p.Schedule)
	}

	stopped := domain.StopList(snap.Dishes, snap.Available())
	for _, s := range stopped {
		snap.Stopped[s.Dish.ID] = true
	}
	return snap, nil
}
