package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/domain"
)

// ExpiredBatch — партия, остаток которой списал проход «дворника».
type ExpiredBatch struct {
	BatchID   int64
	ProductID string
	Qty       int64
}

// ExpiringBatch — партия, о переходе которой в «истекает» проход «дворника» сообщает впервые.
type ExpiringBatch struct {
	BatchID   int64
	ProductID string
	ExpiresAt time.Time
}

// SweepResult — итог одного прохода «дворника».
type SweepResult struct {
	Expired  []ExpiredBatch
	Expiring []ExpiringBatch
}

// SweepExpired списывает остаток просроченных партий (expires_at <= now) движением
// writeoff/expired со временем = expires_at. Протокол блокировок (спецификация, 2.3):
// сначала блокируются затронутые продукты по отсортированным id, и только потом читаются
// часы (now) и партии. Повторный проход ничего не меняет.
func (d *DB) SweepExpired(ctx context.Context, now func() time.Time) ([]ExpiredBatch, error) {
	res, err := d.Sweep(ctx, now, nil)
	return res.Expired, err
}

// expiringCandidateSQL — партии, которые могли перейти в «истекает» к $1: осталось меньше верхней
// границы порога max(Share·срок, MinHours) (порог = min этой величины и MaxShare·срок). Точная
// проверка — domain.Status; этот отбор лишь не даёт блокировать продукты без кандидатов.
const expiringCandidateSQL = `remaining > 0 AND expires_at > $4 AND expiring_notified_at IS NULL
	AND expires_at - $4::timestamptz <
	    GREATEST($2::float8 * EXTRACT(EPOCH FROM expires_at - received_at), $3::float8 * 3600) * interval '1 second'`

// Sweep — проход «дворника»: списывает просроченные партии (см. SweepExpired) и, если задан cfg,
// в той же транзакции под тем же протоколом блокировок ставит expiring_notified_at партиям,
// перешедшим в «истекает», и возвращает их: сигнал batch.expiring уходит один раз на партию
// (спецификация, 2.4). Партия, ставшая просроченной минуя «истекает» (прыжок времени), в
// Expiring не попадает — только в Expired. Метка хранится в БД, поэтому рестарт сигнал не повторяет.
func (d *DB) Sweep(ctx context.Context, now func() time.Time, cfg *domain.ExpiryConfig) (SweepResult, error) {
	var res SweepResult
	err := d.InTx(ctx, func(tx pgx.Tx) error {
		res = SweepResult{}
		// Предварительный выбор продуктов — только чтобы знать, что блокировать; без блокировки
		// он может устареть, поэтому состав партий перечитывается ниже уже под блокировкой.
		pre := now().UTC()
		// Списание — не раньше expires_at + LateEvents (2.4а): до этого партию ещё может взять
		// продажа задним числом. «Истекает» считается на самом pre.
		q := `SELECT product_id FROM batches WHERE remaining > 0 AND expires_at <= $1`
		args := []any{pre.Add(-d.LateEvents)}
		if cfg != nil {
			q += ` UNION SELECT product_id FROM batches WHERE ` + expiringCandidateSQL
			args = append(args, cfg.Share, cfg.MinHours, pre)
		}
		rows, err := tx.Query(ctx, q, args...)
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
		sort.Strings(ids)
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
			RETURNING b.id, b.product_id, old.remaining, b.expires_at`, cur.Add(-d.LateEvents), ids)
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
			res.Expired = append(res.Expired, e)
			at = append(at, exp)
		}
		upd.Close()
		if err := upd.Err(); err != nil {
			return fmt.Errorf("store: sweep update: %w", err)
		}
		for i, e := range res.Expired {
			if _, err := tx.Exec(ctx,
				`INSERT INTO movements (batch_id, product_id, kind, reason, quantity, at) VALUES ($1, $2, 'writeoff', 'expired', $3, $4)`,
				e.BatchID, e.ProductID, e.Qty, at[i]); err != nil {
				return fmt.Errorf("store: sweep movement: %w", err)
			}
		}
		if cfg == nil {
			return nil
		}
		return markExpiring(ctx, tx, cur, ids, *cfg, &res)
	})
	if err != nil {
		return SweepResult{}, err
	}
	return res, nil
}

// markExpiring ставит expiring_notified_at партиям продуктов ids, уже перешедшим в «истекает» на cur.
// Вызывается под блокировкой продуктов, после списания просроченных: партия с expires_at <= cur
// сюда не попадает.
func markExpiring(ctx context.Context, tx pgx.Tx, cur time.Time, ids []string, cfg domain.ExpiryConfig, res *SweepResult) error {
	rows, err := tx.Query(ctx, `
		SELECT id, product_id, remaining, received_at, expires_at FROM batches
		WHERE remaining > 0 AND expires_at > $1 AND expiring_notified_at IS NULL AND product_id = ANY($2)
		ORDER BY id FOR UPDATE`, cur, ids)
	if err != nil {
		return fmt.Errorf("store: sweep expiring: %w", err)
	}
	var marked []int64
	for rows.Next() {
		var b domain.Batch
		var rem int64
		if err := rows.Scan(&b.ID, &b.ProductID, &rem, &b.ReceivedAt, &b.ExpiresAt); err != nil {
			rows.Close()
			return fmt.Errorf("store: sweep expiring: %w", err)
		}
		b.Remaining = domain.Qty(rem)
		if domain.ShouldNotifyExpiring(b, cur, cfg) {
			marked = append(marked, b.ID)
			res.Expiring = append(res.Expiring, ExpiringBatch{BatchID: b.ID, ProductID: b.ProductID, ExpiresAt: b.ExpiresAt})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: sweep expiring: %w", err)
	}
	if len(marked) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE batches SET expiring_notified_at = $1 WHERE id = ANY($2)`, cur, marked); err != nil {
		return fmt.Errorf("store: sweep expiring mark: %w", err)
	}
	return nil
}

// NextExpiringAt возвращает ближайший будущий момент перехода в «истекает» среди партий с остатком,
// о которых ещё не сообщали: expires_at − порог. Расчёт в SQL может отличаться от domain.ExpiringAt на
// доли секунды; «дворник» просыпается не раньше MinInterval и повторяет проход.
func (d *DB) NextExpiringAt(ctx context.Context, now time.Time, cfg domain.ExpiryConfig) (*time.Time, error) {
	var t *time.Time
	err := d.Pool.QueryRow(ctx, `
		SELECT MIN(expires_at - LEAST(
			GREATEST($2::float8 * EXTRACT(EPOCH FROM expires_at - received_at), $3::float8 * 3600),
			$4::float8 * EXTRACT(EPOCH FROM expires_at - received_at)) * interval '1 second')
		FROM batches
		WHERE remaining > 0 AND expires_at > $1 AND expiring_notified_at IS NULL`, now.UTC(), cfg.Share, cfg.MinHours, cfg.MaxShare).Scan(&t)
	if err != nil {
		return nil, fmt.Errorf("store: next expiring: %w", err)
	}
	return t, nil
}

// NextExpiry возвращает ближайший момент списания «дворником» среди партий с остатком: expires_at +
// LateEvents, строго после now (партия, истёкшая в пределах окна, будит на конец окна).
func (d *DB) NextExpiry(ctx context.Context, now time.Time) (*time.Time, error) {
	var t *time.Time
	err := d.Pool.QueryRow(ctx,
		`SELECT MIN(expires_at) + $2::float8 * interval '1 second' FROM batches
		 WHERE remaining > 0 AND expires_at + $2::float8 * interval '1 second' > $1`,
		now.UTC(), d.LateEvents.Seconds()).Scan(&t)
	if err != nil {
		return nil, fmt.Errorf("store: next expiry: %w", err)
	}
	return t, nil
}
