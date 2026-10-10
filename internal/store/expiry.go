package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ExpiredBatch — партия, остаток которой списал проход «дворника».
type ExpiredBatch struct {
	BatchID   int64
	ProductID string
	Qty       int64
}

// SweepExpired списывает остаток просроченных партий (expires_at <= now) движением
// writeoff/expired со временем = expires_at. Протокол блокировок (.ai/solution.md, 2.3):
// сначала блокируются затронутые продукты по отсортированным id, и только потом читаются
// часы (now) и партии. Повторный проход ничего не меняет.
func (d *DB) SweepExpired(ctx context.Context, now func() time.Time) ([]ExpiredBatch, error) {
	var out []ExpiredBatch
	err := d.InTx(ctx, func(tx pgx.Tx) error {
		out = nil
		// Предварительный выбор продуктов — только чтобы знать, что блокировать; без блокировки
		// он может устареть, поэтому состав партий перечитывается ниже уже под блокировкой.
		rows, err := tx.Query(ctx,
			`SELECT DISTINCT product_id FROM batches WHERE remaining > 0 AND expires_at <= $1`, now().UTC())
		if err != nil {
			return fmt.Errorf("store: sweep candidates: %w", err)
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return fmt.Errorf("store: sweep candidates: %w", err)
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("store: sweep candidates: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		if err := LockProducts(ctx, tx, ids); err != nil {
			return err
		}
		cur := now().UTC()
		upd, err := tx.Query(ctx, `
			WITH old AS (
				SELECT id, remaining FROM batches
				WHERE remaining > 0 AND expires_at <= $1 AND product_id = ANY($2)
				ORDER BY id FOR UPDATE
			)
			UPDATE batches b SET remaining = b.remaining - old.remaining, written_off_at = b.expires_at
			FROM old WHERE b.id = old.id
			RETURNING b.id, b.product_id, old.remaining, b.expires_at`, cur, ids)
		if err != nil {
			return fmt.Errorf("store: sweep update: %w", err)
		}
		var at []time.Time
		for upd.Next() {
			var e ExpiredBatch
			var exp time.Time
			if err := upd.Scan(&e.BatchID, &e.ProductID, &e.Qty, &exp); err != nil {
				upd.Close()
				return fmt.Errorf("store: sweep update: %w", err)
			}
			out = append(out, e)
			at = append(at, exp)
		}
		upd.Close()
		if err := upd.Err(); err != nil {
			return fmt.Errorf("store: sweep update: %w", err)
		}
		for i, e := range out {
			if _, err := tx.Exec(ctx,
				`INSERT INTO movements (batch_id, product_id, kind, reason, quantity, at) VALUES ($1, $2, 'writeoff', 'expired', $3, $4)`,
				e.BatchID, e.ProductID, e.Qty, at[i]); err != nil {
				return fmt.Errorf("store: sweep movement: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// NextExpiry возвращает ближайший expires_at среди партий с остатком, ещё не истёкших на now.
func (d *DB) NextExpiry(ctx context.Context, now time.Time) (*time.Time, error) {
	var t *time.Time
	err := d.Pool.QueryRow(ctx,
		`SELECT MIN(expires_at) FROM batches WHERE remaining > 0 AND expires_at > $1`, now.UTC()).Scan(&t)
	if err != nil {
		return nil, fmt.Errorf("store: next expiry: %w", err)
	}
	return t, nil
}
