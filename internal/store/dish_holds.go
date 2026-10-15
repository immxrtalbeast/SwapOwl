package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrDishHoldDish — блюда нет в справочнике.
var ErrDishHoldDish = errors.New("store: unknown dish in hold")

// DishHold — ручная пауза блюда: заказать его нельзя, пока менеджер не снимет паузу.
type DishHold struct {
	DishID string    `json:"dish_id"`
	Note   string    `json:"note"`
	SetAt  time.Time `json:"set_at"`
	SetBy  string    `json:"set_by"`
}

// HeldJSON — блюдо на паузе в отклонённом заказе.
type HeldJSON struct {
	DishID string `json:"dish_id"`
	Note   string `json:"note"`
}

// ListDishHolds — все паузы, новые первыми.
func (d *DB) ListDishHolds(ctx context.Context) ([]DishHold, error) {
	rows, err := d.Pool.Query(ctx, `SELECT dish_id, note, set_at, set_by FROM dish_holds ORDER BY set_at DESC, dish_id`)
	if err != nil {
		return nil, fmt.Errorf("store: list dish holds: %w", err)
	}
	defer rows.Close()
	out := []DishHold{}
	for rows.Next() {
		var h DishHold
		if err := rows.Scan(&h.DishID, &h.Note, &h.SetAt, &h.SetBy); err != nil {
			return nil, fmt.Errorf("store: list dish holds: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// SetDishHold ставит блюдо на паузу (заменяет прежнюю заметку). Повтор идемпотентен.
func (d *DB) SetDishHold(ctx context.Context, h DishHold) (DishHold, error) {
	tag, err := d.Pool.Exec(ctx, `
		INSERT INTO dish_holds (dish_id, note, set_at, set_by)
		SELECT $1, $2, $3, $4 WHERE EXISTS (SELECT 1 FROM dishes WHERE id = $1)
		ON CONFLICT (dish_id) DO UPDATE SET note = EXCLUDED.note, set_at = EXCLUDED.set_at, set_by = EXCLUDED.set_by`,
		h.DishID, h.Note, h.SetAt, h.SetBy)
	if err != nil {
		return DishHold{}, fmt.Errorf("store: set dish hold: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return DishHold{}, ErrDishHoldDish
	}
	return h, nil
}

// DeleteDishHold снимает паузу; нет паузы — не ошибка (повтор DELETE идемпотентен).
func (d *DB) DeleteDishHold(ctx context.Context, dishID string) error {
	if _, err := d.Pool.Exec(ctx, `DELETE FROM dish_holds WHERE dish_id = $1`, dishID); err != nil {
		return fmt.Errorf("store: delete dish hold: %w", err)
	}
	return nil
}

// heldDishes — блюда из ids, стоящие на паузе, по возрастанию dish_id. Читается в транзакции заказа.
func heldDishes(ctx context.Context, tx pgx.Tx, ids []string) ([]HeldJSON, error) {
	rows, err := tx.Query(ctx, `SELECT dish_id, note FROM dish_holds WHERE dish_id = ANY($1) ORDER BY dish_id`, ids)
	if err != nil {
		return nil, fmt.Errorf("store: held dishes: %w", err)
	}
	defer rows.Close()
	out := []HeldJSON{}
	for rows.Next() {
		var h HeldJSON
		if err := rows.Scan(&h.DishID, &h.Note); err != nil {
			return nil, fmt.Errorf("store: held dishes: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
