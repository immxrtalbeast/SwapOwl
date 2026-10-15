package store

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

// ErrUnknownProduct — продукта нет в справочнике.
var ErrUnknownProduct = errors.New("store: unknown product")

// InTx выполняет fn в транзакции: коммит при nil, откат при ошибке.
func (d *DB) InTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// LockProducts — протокол блокировок (спецификация, 2.3): продукты блокируются
// SELECT … FOR UPDATE по одному, циклом по отсортированному списку уникальных id.
// Все писатели партий (поставка, заказ, «дворник») обязаны вызывать его первой операцией
// над складом, до чтения часов и партий. Неизвестный продукт — ErrUnknownProduct.
func LockProducts(ctx context.Context, tx pgx.Tx, ids []string) error {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	prev := ""
	for i, id := range sorted {
		if i > 0 && id == prev {
			continue
		}
		prev = id
		var got string
		err := tx.QueryRow(ctx, `SELECT id FROM products WHERE id = $1 FOR UPDATE`, id).Scan(&got)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrUnknownProduct, id)
		}
		if err != nil {
			return fmt.Errorf("store: lock product %s: %w", id, err)
		}
	}
	return nil
}
