package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Querier — то, что нужно IsEmpty: pgxpool.Pool, pgx.Tx и pgx.Conn подходят.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// IsEmpty сообщает, что в БД нет ни справочников, ни поставок, ни заказов.
func IsEmpty(ctx context.Context, q Querier) (bool, error) {
	var empty bool
	err := q.QueryRow(ctx, `SELECT NOT (EXISTS (SELECT 1 FROM products) OR EXISTS (SELECT 1 FROM dishes)
		OR EXISTS (SELECT 1 FROM deliveries) OR EXISTS (SELECT 1 FROM orders))`).Scan(&empty)
	if err != nil {
		return false, fmt.Errorf("seed: проверка пустоты БД: %w", err)
	}
	return empty, nil
}

// Load пишет сид в переданную транзакцию: продукты, блюда, рецепты, стартовые поставки и
// HistoryDays суток истории до t0. Времена пишутся в таблицы напрямую (единственное
// исключение из received_at = clock.Now(), спецификация, 2.8). Справочники идемпотентны (после сброса по 7.1 сидовые products, dishes и
// recipe_items остаются), поставки, партии, заказы и движения ожидаются пустыми.
func Load(ctx context.Context, tx pgx.Tx, t0 time.Time) error {
	h, err := buildHistory(t0)
	if err != nil {
		return err
	}
	b := &pgx.Batch{}
	queueCatalog(b, Products(), Dishes())
	for _, d := range h.deliveries {
		raw, err := json.Marshal(d.Resp)
		if err != nil {
			return fmt.Errorf("seed: ответ поставки %s: %w", d.ID, err)
		}
		b.Queue(`INSERT INTO deliveries (id, number, created_at, received_at, body_hash, response) VALUES ($1,$2,$3,$4,$5,$6)`,
			d.ID, d.Number, d.CreatedAt, d.Receive, d.Hash, raw)
	}
	for _, x := range h.batches {
		b.Queue(`INSERT INTO batches (id, delivery_id, line_no, product_id, quantity, remaining, received_at, expires_at, written_off_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			x.ID, x.DeliveryID, x.Line, x.ProductID, x.Quantity, x.Remaining, x.ReceivedAt, x.ExpiresAt, x.WrittenOffAt)
	}
	for _, o := range h.orders {
		raw, err := json.Marshal(o.Resp)
		if err != nil {
			return fmt.Errorf("seed: ответ заказа %s: %w", o.ID, err)
		}
		b.Queue(`INSERT INTO orders (id, body_hash, status, at, response) VALUES ($1,$2,$3,$4,$5)`, o.ID, o.Hash, o.Status, o.At, raw)
		for _, it := range o.Items {
			b.Queue(`INSERT INTO order_items (order_id, dish_id, portions) VALUES ($1,$2,$3)
				ON CONFLICT (order_id, dish_id) DO UPDATE SET portions = order_items.portions + EXCLUDED.portions`, o.ID, it.DishID, it.Portions)
		}
	}
	for _, m := range h.moves {
		var reason, order any
		if m.Reason != "" {
			reason = m.Reason
		}
		if m.OrderID != "" {
			order = m.OrderID
		}
		b.Queue(`INSERT INTO movements (batch_id, product_id, kind, reason, quantity, order_id, at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			m.BatchID, m.ProductID, m.Kind, reason, m.Qty, order, m.At)
	}
	// Идентификаторы партий заданы явно, чтобы движения ссылались на них; счётчик догоняем.
	b.Queue(`SELECT setval(pg_get_serial_sequence('batches', 'id'), GREATEST((SELECT COALESCE(MAX(id), 0) FROM batches), 1))`)

	res := tx.SendBatch(ctx, b)
	for i := 0; i < b.Len(); i++ {
		if _, err := res.Exec(); err != nil {
			_ = res.Close()
			return fmt.Errorf("seed: загрузка, запрос %d: %w", i, err)
		}
	}
	return res.Close()
}

// nullableHours — 0 в сиде означает «не задан», в БД это null.
func nullableHours(h int) *int {
	if h == 0 {
		return nil
	}
	return &h
}

// LoadCatalog пишет только справочники (продукты, блюда, рецепты) — для профилей стенда со своим
// складом, например ab: без истории, поставки и заказы приходят отдельно. Идемпотентно.
func LoadCatalog(ctx context.Context, tx pgx.Tx, products []Product, dishes []Dish) error {
	b := &pgx.Batch{}
	queueCatalog(b, products, dishes)
	res := tx.SendBatch(ctx, b)
	for i := 0; i < b.Len(); i++ {
		if _, err := res.Exec(); err != nil {
			_ = res.Close()
			return fmt.Errorf("seed: справочники, запрос %d: %w", i, err)
		}
	}
	return res.Close()
}

func queueCatalog(b *pgx.Batch, products []Product, dishes []Dish) {
	for _, p := range products {
		// shelf_life_hours — обычный срок поставщика: его явно передаёт «Принять предложенное» (FS-160); без него
		// поставка из предложения требует ввода срока вручную.
		b.Queue(`INSERT INTO products (id, name, unit, price_minor_per_1000, lead_days, pack_size, shelf_after_open_hours, shelf_life_hours) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, unit = EXCLUDED.unit,
				price_minor_per_1000 = EXCLUDED.price_minor_per_1000, lead_days = EXCLUDED.lead_days,
				pack_size = EXCLUDED.pack_size, shelf_after_open_hours = EXCLUDED.shelf_after_open_hours,
				shelf_life_hours = EXCLUDED.shelf_life_hours, auto_created = false`,
			p.ID, p.Name, string(p.Unit), p.PriceMinorPer1000, p.LeadDays, int64(p.PackSize), nullableHours(p.ShelfAfterOpenHours), nullableHours(p.ShelfLifeHours))
	}
	for _, d := range dishes {
		b.Queue(`INSERT INTO dishes (id, name, price_minor, prior_daily) VALUES ($1,$2,$3,$4)
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, price_minor = EXCLUDED.price_minor, prior_daily = EXCLUDED.prior_daily`,
			d.ID, d.Name, d.PriceMinor, d.PriorDaily)
		for _, r := range d.Recipe {
			b.Queue(`INSERT INTO recipe_items (dish_id, product_id, quantity) VALUES ($1,$2,$3)
				ON CONFLICT (dish_id, product_id) DO UPDATE SET quantity = EXCLUDED.quantity`,
				d.ID, r.ProductID, int64(r.Quantity))
		}
	}
}
