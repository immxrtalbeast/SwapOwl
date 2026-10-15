package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrSupplierCodeProduct — продукта нет в справочнике.
var ErrSupplierCodeProduct = errors.New("store: unknown product in supplier code")

// SupplierCode — сопоставление кода поставщика (GTIN или артикул) с продуктом.
type SupplierCode struct {
	Code      string    `json:"code"`
	ProductID string    `json:"product_id"`
	SetAt     time.Time `json:"set_at"`
	SetBy     string    `json:"set_by"`
}

// ListSupplierCodes — все сопоставления, новые первыми.
func (d *DB) ListSupplierCodes(ctx context.Context) ([]SupplierCode, error) {
	rows, err := d.Pool.Query(ctx, `SELECT code, product_id, set_at, set_by FROM supplier_codes ORDER BY set_at DESC, code`)
	if err != nil {
		return nil, fmt.Errorf("store: list supplier codes: %w", err)
	}
	defer rows.Close()
	out := []SupplierCode{}
	for rows.Next() {
		var c SupplierCode
		if err := rows.Scan(&c.Code, &c.ProductID, &c.SetAt, &c.SetBy); err != nil {
			return nil, fmt.Errorf("store: list supplier codes: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetSupplierCode записывает сопоставление (заменяет прежнее у этого кода). Повтор идемпотентен.
func (d *DB) SetSupplierCode(ctx context.Context, c SupplierCode) (SupplierCode, error) {
	tag, err := d.Pool.Exec(ctx, `
		INSERT INTO supplier_codes (code, product_id, set_at, set_by)
		SELECT $1, $2, $3, $4 WHERE EXISTS (SELECT 1 FROM products WHERE id = $2)
		ON CONFLICT (code) DO UPDATE SET product_id = EXCLUDED.product_id, set_at = EXCLUDED.set_at, set_by = EXCLUDED.set_by`,
		c.Code, c.ProductID, c.SetAt, c.SetBy)
	if err != nil {
		return SupplierCode{}, fmt.Errorf("store: set supplier code: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return SupplierCode{}, ErrSupplierCodeProduct
	}
	return c, nil
}

// DeleteSupplierCode снимает сопоставление; нет записи — не ошибка (повтор DELETE идемпотентен).
func (d *DB) DeleteSupplierCode(ctx context.Context, code string) error {
	if _, err := d.Pool.Exec(ctx, `DELETE FROM supplier_codes WHERE code = $1`, code); err != nil {
		return fmt.Errorf("store: delete supplier code: %w", err)
	}
	return nil
}
