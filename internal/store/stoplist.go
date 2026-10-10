package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/domain"
)

// MenuSnapshot — согласованный срез для расчёта стоп-листа: блюда с рецептами, доступные
// остатки на момент now и справочник продуктов.
type MenuSnapshot struct {
	Dishes    []domain.Dish
	Available map[string]domain.Qty
	Products  map[string]ProductRef
}

// ProductRef — название и единица продукта для текста причины.
type ProductRef struct {
	Name string
	Unit domain.Unit
}

// MenuSnapshot читает меню и остатки одной транзакцией. Доступно = remaining > 0 AND now < expires_at:
// просрочка исключается предикатом времени, а не событием «дворника».
func (d *DB) MenuSnapshot(ctx context.Context, now time.Time) (MenuSnapshot, error) {
	tx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return MenuSnapshot{}, fmt.Errorf("store: menu snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	snap := MenuSnapshot{Available: map[string]domain.Qty{}, Products: map[string]ProductRef{}}

	rows, err := tx.Query(ctx, `SELECT id, name, unit FROM products`)
	if err != nil {
		return snap, fmt.Errorf("store: menu snapshot: %w", err)
	}
	for rows.Next() {
		var id, name, u string
		if err := rows.Scan(&id, &name, &u); err != nil {
			rows.Close()
			return snap, fmt.Errorf("store: menu snapshot: %w", err)
		}
		snap.Products[id] = ProductRef{Name: name, Unit: domain.Unit(u)}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return snap, fmt.Errorf("store: menu snapshot: %w", err)
	}

	rows, err = tx.Query(ctx, `
		SELECT product_id, SUM(remaining)::bigint FROM batches
		WHERE remaining > 0 AND expires_at > $1 GROUP BY product_id`, now)
	if err != nil {
		return snap, fmt.Errorf("store: menu snapshot: %w", err)
	}
	for rows.Next() {
		var id string
		var q int64
		if err := rows.Scan(&id, &q); err != nil {
			rows.Close()
			return snap, fmt.Errorf("store: menu snapshot: %w", err)
		}
		snap.Available[id] = domain.Qty(q)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return snap, fmt.Errorf("store: menu snapshot: %w", err)
	}

	rows, err = tx.Query(ctx, `
		SELECT d.id, d.name, r.product_id, r.quantity
		FROM dishes d JOIN recipe_items r ON r.dish_id = d.id
		ORDER BY d.name, d.id, r.product_id`)
	if err != nil {
		return snap, fmt.Errorf("store: menu snapshot: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, name, pid string
		var q int64
		if err := rows.Scan(&id, &name, &pid, &q); err != nil {
			return snap, fmt.Errorf("store: menu snapshot: %w", err)
		}
		if n := len(snap.Dishes); n == 0 || snap.Dishes[n-1].ID != id {
			snap.Dishes = append(snap.Dishes, domain.Dish{ID: id, Name: name})
		}
		last := &snap.Dishes[len(snap.Dishes)-1]
		last.Recipe = append(last.Recipe, domain.RecipeItem{ProductID: pid, Quantity: domain.Qty(q)})
	}
	return snap, rows.Err()
}
