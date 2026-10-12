package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// OutboxSupplyOrder — вид исходящего сообщения «заказ поставщику» (FS-121). Новое значение в колонке kind.
const OutboxSupplyOrder = "supply_order"

// Статусы заказа поставщику на экране «Поставки» — по подтверждению outbox.
const (
	SupplyOrderSent      = "sent"      // в очереди, адаптер ещё не подтвердил
	SupplyOrderDelivered = "delivered" // адаптер подтвердил доставку в iiko
	SupplyOrderError     = "error"     // последняя попытка доставки не удалась, адаптер повторит
)

var (
	// ErrSupplyOrderConflict — тот же id, другое содержимое.
	ErrSupplyOrderConflict = errors.New("store: supply order id reused with different items")
	// ErrSupplyOrderNotFound — заказа с таким id нет.
	ErrSupplyOrderNotFound = errors.New("store: supply order not found")
	// ErrSupplyOrderProduct — неизвестный продукт в позиции.
	ErrSupplyOrderProduct = errors.New("store: unknown product in supply order")
)

// SupplyOrderItem — позиция заказа: количество в базовых единицах продукта.
type SupplyOrderItem struct {
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name,omitempty"`
	Qty         int64  `json:"qty"`
	Unit        string `json:"unit,omitempty"`
}

// SupplyOrder — заказ поставщику и его состояние доставки.
type SupplyOrder struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	CreatedAt time.Time         `json:"created_at"`
	Items     []SupplyOrderItem `json:"items"`
	Attempts  int               `json:"attempts"`
	LastError *string           `json:"last_error"`
	Replayed  bool              `json:"replayed"`
}

// supplyOrderPayload — payload сообщения supply_order.
type supplyOrderPayload struct {
	ID        string            `json:"id"`
	CreatedAt time.Time         `json:"created_at"`
	Items     []SupplyOrderItem `json:"items"`
}

func sameSupplyItems(a, b []SupplyOrderItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ProductID != b[i].ProductID || a[i].Qty != b[i].Qty {
			return false
		}
	}
	return true
}

// EnqueueSupplyOrder кладёт заказ поставщику в outbox одной транзакцией вместе с проверкой id.
// Повтор с тем же id и составом возвращает тот же заказ (Replayed) без второй строки outbox;
// другой состав — ErrSupplyOrderConflict. Позиции нормализуются: один продукт — одна строка, порядок по id.
// Отдельной таблицы нет: идемпотентность — по payload.id под advisory-блокировкой id.
func (d *DB) EnqueueSupplyOrder(ctx context.Context, id string, items []SupplyOrderItem, now time.Time) (SupplyOrder, error) {
	merged := map[string]int64{}
	for _, it := range items {
		merged[it.ProductID] += it.Qty
	}
	norm := make([]SupplyOrderItem, 0, len(merged))
	for p, q := range merged {
		norm = append(norm, SupplyOrderItem{ProductID: p, Qty: q})
	}
	sort.Slice(norm, func(i, j int) bool { return norm[i].ProductID < norm[j].ProductID })

	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return SupplyOrder{}, fmt.Errorf("store: supply order: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // откат после коммита безвреден
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('supply_order:' || $1, 0))`, id); err != nil {
		return SupplyOrder{}, fmt.Errorf("store: supply order lock: %w", err)
	}
	if existing, err := getSupplyOrder(ctx, tx, id); err == nil {
		if !sameSupplyItems(existing.Items, norm) {
			return SupplyOrder{}, ErrSupplyOrderConflict
		}
		existing.Replayed = true
		return existing, nil
	} else if !errors.Is(err, ErrSupplyOrderNotFound) {
		return SupplyOrder{}, err
	}
	for i, it := range norm {
		var name, unit string
		err := tx.QueryRow(ctx, `SELECT name, unit FROM products WHERE id = $1`, it.ProductID).Scan(&name, &unit)
		if errors.Is(err, pgx.ErrNoRows) {
			return SupplyOrder{}, fmt.Errorf("%w: %s", ErrSupplyOrderProduct, it.ProductID)
		}
		if err != nil {
			return SupplyOrder{}, fmt.Errorf("store: supply order product: %w", err)
		}
		norm[i].ProductName, norm[i].Unit = name, unit
	}
	payload, err := json.Marshal(supplyOrderPayload{ID: id, CreatedAt: now.UTC(), Items: norm})
	if err != nil {
		return SupplyOrder{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox (kind, payload, created_at) VALUES ($1, $2, $3)`,
		OutboxSupplyOrder, payload, now); err != nil {
		return SupplyOrder{}, fmt.Errorf("store: supply order enqueue: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return SupplyOrder{}, fmt.Errorf("store: supply order commit: %w", err)
	}
	return SupplyOrder{ID: id, Status: SupplyOrderSent, CreatedAt: now.UTC(), Items: norm}, nil
}

// GetSupplyOrder возвращает заказ и его статус по подтверждению outbox.
func (d *DB) GetSupplyOrder(ctx context.Context, id string) (SupplyOrder, error) {
	return getSupplyOrder(ctx, d.Pool, id)
}

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func getSupplyOrder(ctx context.Context, q queryRower, id string) (SupplyOrder, error) {
	var payload []byte
	var delivered *time.Time
	var o SupplyOrder
	err := q.QueryRow(ctx, `
		SELECT payload, delivered_at, attempts, last_error FROM outbox
		WHERE kind = $1 AND payload->>'id' = $2 ORDER BY id LIMIT 1`, OutboxSupplyOrder, id).
		Scan(&payload, &delivered, &o.Attempts, &o.LastError)
	if errors.Is(err, pgx.ErrNoRows) {
		return SupplyOrder{}, ErrSupplyOrderNotFound
	}
	if err != nil {
		return SupplyOrder{}, fmt.Errorf("store: supply order: %w", err)
	}
	var p supplyOrderPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return SupplyOrder{}, fmt.Errorf("store: supply order payload: %w", err)
	}
	o.ID, o.CreatedAt, o.Items = p.ID, p.CreatedAt, p.Items
	switch {
	case delivered != nil:
		o.Status = SupplyOrderDelivered
	case o.Attempts > 0:
		o.Status = SupplyOrderError
	default:
		o.Status = SupplyOrderSent
	}
	return o, nil
}
