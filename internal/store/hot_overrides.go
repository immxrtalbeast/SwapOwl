package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Режимы ручной правки горячих предложений (FS-159).
const (
	HotPin  = "pin"  // менеджер добавил блюдо в предложения
	HotHide = "hide" // менеджер скрыл блюдо из предложений
)

// ErrHotOverrideDish — блюда нет в справочнике.
var ErrHotOverrideDish = errors.New("store: unknown dish in hot override")

// HotOverride — решение менеджера по блюду.
type HotOverride struct {
	DishID string    `json:"dish_id"`
	Mode   string    `json:"mode"`
	Note   string    `json:"note"`
	SetAt  time.Time `json:"set_at"`
	SetBy  string    `json:"set_by"`
}

// ListHotOverrides — все решения, новые первыми.
func (d *DB) ListHotOverrides(ctx context.Context) ([]HotOverride, error) {
	rows, err := d.Pool.Query(ctx, `SELECT dish_id, mode, note, set_at, set_by FROM hot_overrides ORDER BY set_at DESC, dish_id`)
	if err != nil {
		return nil, fmt.Errorf("store: list hot overrides: %w", err)
	}
	defer rows.Close()
	out := []HotOverride{}
	for rows.Next() {
		var o HotOverride
		if err := rows.Scan(&o.DishID, &o.Mode, &o.Note, &o.SetAt, &o.SetBy); err != nil {
			return nil, fmt.Errorf("store: list hot overrides: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SetHotOverride записывает решение по блюду (заменяет прежнее). Повтор того же решения идемпотентен.
func (d *DB) SetHotOverride(ctx context.Context, o HotOverride) (HotOverride, error) {
	tag, err := d.Pool.Exec(ctx, `
		INSERT INTO hot_overrides (dish_id, mode, note, set_at, set_by)
		SELECT $1, $2, $3, $4, $5 WHERE EXISTS (SELECT 1 FROM dishes WHERE id = $1)
		ON CONFLICT (dish_id) DO UPDATE SET mode = EXCLUDED.mode, note = EXCLUDED.note, set_at = EXCLUDED.set_at, set_by = EXCLUDED.set_by`,
		o.DishID, o.Mode, o.Note, o.SetAt, o.SetBy)
	if err != nil {
		return HotOverride{}, fmt.Errorf("store: set hot override: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return HotOverride{}, ErrHotOverrideDish
	}
	return o, nil
}

// DeleteHotOverride снимает решение; нет решения — не ошибка (повтор DELETE идемпотентен).
func (d *DB) DeleteHotOverride(ctx context.Context, dishID string) error {
	if _, err := d.Pool.Exec(ctx, `DELETE FROM hot_overrides WHERE dish_id = $1`, dishID); err != nil {
		return fmt.Errorf("store: delete hot override: %w", err)
	}
	return nil
}
