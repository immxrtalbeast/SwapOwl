package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/domain"
)

// ErrOrderConflict — тот же id заказа, другое тело.
var ErrOrderConflict = errors.New("store: order id reused with different content")

// Статусы заказа.
const (
	OrderAccepted = "accepted"
	OrderRejected = "rejected"
)

// Источники заказа.
const (
	SourceAPI = "api"
	SourcePOS = "pos"
)

// OrderItem — строка заказа.
type OrderItem struct {
	DishID   string `json:"dish_id"`
	Portions int    `json:"portions"`
}

// OrderInput — заказ после разбора и проверки на границе API. Needs — суммарная потребность
// по всем блюдам (проверенная арифметика, считается до транзакции).
type OrderInput struct {
	ID     string
	Source string // SourceAPI (по умолчанию, "") | SourcePOS
	// OccurredAt — когда продажа произошла фактически (необязательно); входит в хэш. Задано —
	// партии выбираются на этот момент, orders.at и movements.at = OccurredAt (2.4а).
	OccurredAt *time.Time
	Items      []OrderItem
	Needs      []domain.Need
}

func (in OrderInput) isPOS() bool { return in.Source == SourcePOS }

// WriteoffJSON — «партия — количество» принятого заказа.
type WriteoffJSON struct {
	ProductID string    `json:"product_id"`
	BatchID   int64     `json:"batch_id"`
	Qty       QtyJSON   `json:"qty"`
	ExpiresAt time.Time `json:"expires_at"`
}

// MissingJSON — нехватка продукта в отклонённом заказе.
type MissingJSON struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	Required    QtyJSON `json:"required"`
	Available   QtyJSON `json:"available"`
	Missing     QtyJSON `json:"missing"`
}

// ShortfallJSON — недостача продукта в продаже из кассы.
type ShortfallJSON struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	Quantity    QtyJSON `json:"quantity"`
}

// OrderResponse — тело ответа и сохранённый ответ заказа.
type OrderResponse struct {
	ID         string          `json:"id"`
	Status     string          `json:"status"`
	At         time.Time       `json:"at"`
	Replayed   bool            `json:"replayed"`
	Items      []OrderItem     `json:"items"`
	Source     string          `json:"source"`
	Writeoffs  []WriteoffJSON  `json:"writeoffs"`
	Missing    []MissingJSON   `json:"missing"`
	Shortfalls []ShortfallJSON `json:"shortfalls"`
}

// BodyHash — хэш id, источника и позиций (dish_id, порции), отсортированных по dish_id.
// Источник api хэшируется как пустой, чтобы хэши прежних api-заказов не менялись.
func (in OrderInput) BodyHash() []byte {
	items := append([]OrderItem(nil), in.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].DishID < items[j].DishID })
	occ := ""
	if in.OccurredAt != nil {
		occ = in.OccurredAt.UTC().Format(time.RFC3339Nano)
	}
	src := ""
	if in.isPOS() {
		src = SourcePOS
	}
	raw, _ := json.Marshal(struct {
		ID     string      `json:"id"`
		Source string      `json:"source,omitempty"`
		Occ    string      `json:"occurred_at,omitempty"`
		Items  []OrderItem `json:"items"`
	}{in.ID, src, occ, items})
	sum := sha256.Sum256(raw)
	return sum[:]
}

// RecipeLine — норма продукта на порцию.
type RecipeLine struct {
	ProductID string  `json:"product_id"`
	Quantity  QtyJSON `json:"quantity"`
}

// DishView — блюдо с рецептом.
type DishView struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Recipe []RecipeLine `json:"recipe"`
}

// ListDishes возвращает блюда с рецептами, по имени и id.
func (d *DB) ListDishes(ctx context.Context) ([]DishView, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT d.id, d.name, r.product_id, r.quantity, p.unit
		FROM dishes d
		JOIN recipe_items r ON r.dish_id = d.id
		JOIN products p ON p.id = r.product_id
		ORDER BY d.name, d.id, r.product_id`)
	if err != nil {
		return nil, fmt.Errorf("store: list dishes: %w", err)
	}
	defer rows.Close()
	out := []DishView{}
	for rows.Next() {
		var id, name, pid, u string
		var q int64
		if err := rows.Scan(&id, &name, &pid, &q, &u); err != nil {
			return nil, fmt.Errorf("store: list dishes: %w", err)
		}
		if len(out) == 0 || out[len(out)-1].ID != id {
			out = append(out, DishView{ID: id, Name: name, Recipe: []RecipeLine{}})
		}
		last := &out[len(out)-1]
		last.Recipe = append(last.Recipe, RecipeLine{ProductID: pid, Quantity: QtyJSON{q, domain.Unit(u)}})
	}
	return out, rows.Err()
}

// DishRecipes возвращает рецепты запрошенных блюд; неизвестные в карту не попадают.
func (d *DB) DishRecipes(ctx context.Context, ids []string) (map[string][]domain.RecipeItem, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT d.id, r.product_id, r.quantity
		FROM dishes d LEFT JOIN recipe_items r ON r.dish_id = d.id
		WHERE d.id = ANY($1) ORDER BY d.id, r.product_id`, ids)
	if err != nil {
		return nil, fmt.Errorf("store: dish recipes: %w", err)
	}
	defer rows.Close()
	out := map[string][]domain.RecipeItem{}
	for rows.Next() {
		var id string
		var pid *string
		var q *int64
		if err := rows.Scan(&id, &pid, &q); err != nil {
			return nil, fmt.Errorf("store: dish recipes: %w", err)
		}
		if _, ok := out[id]; !ok {
			out[id] = []domain.RecipeItem{}
		}
		if pid != nil && q != nil {
			out[id] = append(out[id], domain.RecipeItem{ProductID: *pid, Quantity: domain.Qty(*q)})
		}
	}
	return out, rows.Err()
}

// PlaceOrder принимает заказ одной транзакцией в порядке 2.3: вставка заказа (идемпотентность) →
// блокировки продуктов → now() → партии → PlanWriteoff → относительные UPDATE и движения sale.
// Нехватки в обоих исходах коммитятся: отклонённый заказ сохраняется, партии не тронуты.
func (d *DB) PlaceOrder(ctx context.Context, in OrderInput, now func() time.Time) (OrderResponse, error) {
	hash := in.BodyHash()
	source := SourceAPI
	if in.isPOS() {
		source = SourcePOS
	}
	var resp OrderResponse
	err := d.InTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`INSERT INTO orders (id, body_hash, status, at, response, source) VALUES ($1, $2, 'rejected', now(), '{}', $3)
			 ON CONFLICT (id) DO NOTHING`, in.ID, hash, source)
		if err != nil {
			return fmt.Errorf("store: insert order: %w", err)
		}
		if tag.RowsAffected() == 0 {
			var stored, raw []byte
			if err := tx.QueryRow(ctx, `SELECT body_hash, response FROM orders WHERE id = $1`, in.ID).Scan(&stored, &raw); err != nil {
				return fmt.Errorf("store: read order: %w", err)
			}
			if string(stored) != string(hash) {
				return ErrOrderConflict
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				return fmt.Errorf("store: stored order response: %w", err)
			}
			resp.Replayed = true
			return nil
		}
		for _, it := range in.Items {
			if _, err := tx.Exec(ctx, `INSERT INTO order_items (order_id, dish_id, portions) VALUES ($1, $2, $3)`,
				in.ID, it.DishID, it.Portions); err != nil {
				return fmt.Errorf("store: insert order item: %w", err)
			}
		}

		ids := make([]string, len(in.Needs))
		for i, n := range in.Needs {
			ids[i] = n.ProductID
		}
		if err := LockProducts(ctx, tx, ids); err != nil {
			return err
		}
		at := now().UTC()
		// Событие задним числом: партия подходит, если уже была на складе и ещё не истекла на occurred_at.
		// Без occurred_at условие по received_at не добавляется — поведение прежнее.
		sinceReceived := false
		if in.OccurredAt != nil {
			if err := checkOccurredAt(in.OccurredAt.UTC(), at, d.LateEvents); err != nil {
				return err
			}
			at, sinceReceived = in.OccurredAt.UTC(), true
		}

		rows, err := tx.Query(ctx,
			`SELECT id, product_id, quantity, remaining, received_at, expires_at FROM batches
			 WHERE product_id = ANY($1) AND remaining > 0 AND expires_at > $2 AND (NOT $3 OR received_at <= $2)
			 ORDER BY product_id, expires_at, id`, ids, at, sinceReceived)
		if err != nil {
			return fmt.Errorf("store: read batches: %w", err)
		}
		var batches []domain.Batch
		for rows.Next() {
			var b domain.Batch
			if err := rows.Scan(&b.ID, &b.ProductID, &b.Quantity, &b.Remaining, &b.ReceivedAt, &b.ExpiresAt); err != nil {
				rows.Close()
				return fmt.Errorf("store: read batches: %w", err)
			}
			batches = append(batches, b)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("store: read batches: %w", err)
		}

		names, unitsOf, err := productInfo(ctx, tx, ids)
		if err != nil {
			return err
		}
		var plan []domain.Writeoff
		var short []domain.Shortage
		var shortfalls []domain.Shortfall
		if in.isPOS() {
			plan, shortfalls = domain.PlanSale(in.Needs, batches)
		} else {
			plan, short = domain.PlanWriteoff(in.Needs, batches)
		}
		resp = OrderResponse{ID: in.ID, Source: source, At: at, Items: in.Items,
			Writeoffs: []WriteoffJSON{}, Missing: []MissingJSON{}, Shortfalls: []ShortfallJSON{}}
		if short != nil {
			resp.Status = OrderRejected
			for _, s := range short {
				u := unitsOf[s.ProductID]
				resp.Missing = append(resp.Missing, MissingJSON{ProductID: s.ProductID, ProductName: names[s.ProductID],
					Required: QtyJSON{int64(s.Required), u}, Available: QtyJSON{int64(s.Available), u}, Missing: QtyJSON{int64(s.Missing), u}})
			}
		} else {
			resp.Status = OrderAccepted
			expires := map[int64]time.Time{}
			for _, b := range batches {
				expires[b.ID] = b.ExpiresAt
			}
			for _, s := range shortfalls {
				if _, err := tx.Exec(ctx, `INSERT INTO shortfalls (order_id, product_id, quantity, at) VALUES ($1, $2, $3, $4)`,
					in.ID, s.ProductID, int64(s.Qty), at); err != nil {
					return fmt.Errorf("store: insert shortfall: %w", err)
				}
				resp.Shortfalls = append(resp.Shortfalls, ShortfallJSON{ProductID: s.ProductID, ProductName: names[s.ProductID],
					Quantity: QtyJSON{int64(s.Qty), unitsOf[s.ProductID]}})
			}
			for _, w := range plan {
				tag, err := tx.Exec(ctx, `UPDATE batches SET remaining = remaining - $2 WHERE id = $1 AND remaining >= $2`, w.BatchID, int64(w.Qty))
				if err != nil {
					return fmt.Errorf("store: update batch %d: %w", w.BatchID, err)
				}
				if tag.RowsAffected() != 1 {
					return fmt.Errorf("store: lock protocol violated: batch %d changed under lock", w.BatchID)
				}
				if _, err := tx.Exec(ctx,
					`INSERT INTO movements (batch_id, product_id, kind, quantity, order_id, at) VALUES ($1, $2, 'sale', $3, $4, $5)`,
					w.BatchID, w.ProductID, int64(w.Qty), in.ID, at); err != nil {
					return fmt.Errorf("store: insert sale: %w", err)
				}
				resp.Writeoffs = append(resp.Writeoffs, WriteoffJSON{ProductID: w.ProductID, BatchID: w.BatchID,
					Qty: QtyJSON{int64(w.Qty), unitsOf[w.ProductID]}, ExpiresAt: expires[w.BatchID]})
			}
		}
		raw, err := json.Marshal(resp)
		if err != nil {
			return fmt.Errorf("store: marshal order: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET status = $2, at = $3, response = $4 WHERE id = $1`,
			in.ID, resp.Status, at, raw); err != nil {
			return fmt.Errorf("store: save order: %w", err)
		}
		return nil
	})
	return resp, err
}

func productInfo(ctx context.Context, tx pgx.Tx, ids []string) (map[string]string, map[string]domain.Unit, error) {
	rows, err := tx.Query(ctx, `SELECT id, name, unit FROM products WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("store: product info: %w", err)
	}
	defer rows.Close()
	names, us := map[string]string{}, map[string]domain.Unit{}
	for rows.Next() {
		var id, name, u string
		if err := rows.Scan(&id, &name, &u); err != nil {
			return nil, nil, fmt.Errorf("store: product info: %w", err)
		}
		names[id], us[id] = name, domain.Unit(u)
	}
	return names, us, rows.Err()
}

// ListOrders — история заказов, новые сверху; status "" | "accepted" | "rejected".
func (d *DB) ListOrders(ctx context.Context, status string, limit int) ([]OrderResponse, error) {
	rows, err := d.Pool.Query(ctx,
		`SELECT response FROM orders WHERE ($1 = '' OR status = $1) ORDER BY at DESC, id LIMIT $2`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list orders: %w", err)
	}
	defer rows.Close()
	out := []OrderResponse{}
	for rows.Next() {
		var raw []byte
		var r OrderResponse
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("store: list orders: %w", err)
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("store: list orders: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
