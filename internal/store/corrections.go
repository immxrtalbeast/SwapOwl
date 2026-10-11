package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"umirhack/internal/domain"
)

// Ошибки корректировок.
var (
	ErrCorrectionConflict  = errors.New("store: correction id reused with different content")
	ErrUnknownBatch        = errors.New("store: unknown batch")
	ErrInsufficientBatch   = errors.New("store: batch has less than requested")
	ErrUnknownOrder        = errors.New("store: unknown order")
	ErrOrderNotCancellable = errors.New("store: only accepted orders can be cancelled")
	ErrAlreadyCancelled    = errors.New("store: order already cancelled")
	ErrInventoryNoShelf    = errors.New("store: product has no default shelf life for the surplus batch")
)

const (
	corrWriteoff  = "writeoff"
	corrInventory = "inventory"
	corrCancel    = "cancel"

	// InventoryDeliveryPrefix — префикс id поставки, под которой заводится излишек инвентаризации.
	InventoryDeliveryPrefix = "INV-"
)

// WriteoffInput — списание брака с партии.
type WriteoffInput struct {
	ID      string
	BatchID int64
	Qty     int64
	Reason  string // ReasonSpoilage | ReasonDamage
}

// WriteoffResult — ответ POST /api/writeoffs.
type WriteoffResult struct {
	ID        string    `json:"id"`
	BatchID   int64     `json:"batch_id"`
	ProductID string    `json:"product_id"`
	Qty       QtyJSON   `json:"qty"`
	Reason    string    `json:"reason"`
	Remaining QtyJSON   `json:"remaining"`
	At        time.Time `json:"at"`
	Replayed  bool      `json:"replayed"`
}

// InventoryInput — результат пересчёта продукта.
type InventoryInput struct {
	ID        string
	ProductID string
	Counted   int64
}

// AdjustmentJSON — списание недостачи с партии.
type AdjustmentJSON struct {
	BatchID   int64   `json:"batch_id"`
	ProductID string  `json:"product_id"`
	Qty       QtyJSON `json:"qty"`
}

// InventoryResult — ответ POST /api/inventory.
type InventoryResult struct {
	ID        string           `json:"id"`
	ProductID string           `json:"product_id"`
	Counted   QtyJSON          `json:"counted"`
	Shortage  []AdjustmentJSON `json:"shortage"`
	Surplus   *BatchJSON       `json:"surplus"`
	At        time.Time        `json:"at"`
	Replayed  bool             `json:"replayed"`
}

// CancelInput — отмена принятого заказа. ID — id операции, OrderID — отменяемый заказ.
type CancelInput struct {
	ID      string
	OrderID string
}

// CancelResult — ответ POST /api/orders/{id}/cancel. Returned — вернулось в непросроченные
// партии; Expired — проданное из партий, срок которых уже вышел: остаток им не возвращается.
type CancelResult struct {
	ID       string           `json:"id"`
	OrderID  string           `json:"order_id"`
	Returned []AdjustmentJSON `json:"returned"`
	Expired  []AdjustmentJSON `json:"expired"`
	At       time.Time        `json:"at"`
	Replayed bool             `json:"replayed"`
}

func corrHash(parts ...string) []byte {
	raw, _ := json.Marshal(parts)
	sum := sha256.Sum256(raw)
	return sum[:]
}

// claimCorrection — первая команда транзакции корректировки: вставка ключа идемпотентности.
// replay != nil — операция уже выполнена с тем же телом; другое тело — ErrCorrectionConflict.
func claimCorrection(ctx context.Context, tx pgx.Tx, kind, id, subject string, hash []byte) (replay []byte, err error) {
	tag, err := tx.Exec(ctx,
		`INSERT INTO corrections (kind, id, subject, body_hash, at, response) VALUES ($1, $2, $3, $4, now(), '{}')
		 ON CONFLICT (kind, id) DO NOTHING`, kind, id, subject, hash)
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" && kind == corrCancel {
			return nil, ErrAlreadyCancelled
		}
		return nil, fmt.Errorf("store: claim %s: %w", kind, err)
	}
	if tag.RowsAffected() == 1 {
		return nil, nil
	}
	var stored, raw []byte
	if err := tx.QueryRow(ctx, `SELECT body_hash, response FROM corrections WHERE kind = $1 AND id = $2`, kind, id).Scan(&stored, &raw); err != nil {
		return nil, fmt.Errorf("store: read %s: %w", kind, err)
	}
	if string(stored) != string(hash) {
		return nil, ErrCorrectionConflict
	}
	return raw, nil
}

func saveCorrection(ctx context.Context, tx pgx.Tx, kind, id string, at time.Time, resp any) error {
	raw, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("store: marshal %s: %w", kind, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE corrections SET at = $3, response = $4 WHERE kind = $1 AND id = $2`, kind, id, at, raw); err != nil {
		return fmt.Errorf("store: save %s: %w", kind, err)
	}
	return nil
}

// moveBatch — относительное изменение остатка партии под блокировкой продукта и движение.
// delta > 0 увеличивает остаток (receipt/return), delta < 0 — уменьшает.
func moveBatch(ctx context.Context, tx pgx.Tx, batchID int64, productID, kind, reason string, qty int64, orderID *string, at time.Time) error {
	delta := qty
	if kind == KindSale || kind == KindWriteoff || kind == KindAdjustment {
		delta = -qty
	}
	tag, err := tx.Exec(ctx,
		`UPDATE batches SET remaining = remaining + $2 WHERE id = $1 AND remaining + $2 >= 0 AND remaining + $2 <= quantity`, batchID, delta)
	if err != nil {
		return fmt.Errorf("store: update batch %d: %w", batchID, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("store: lock protocol violated: batch %d changed under lock", batchID)
	}
	return insertMovement(ctx, tx, batchID, productID, kind, reason, qty, orderID, at)
}

func insertMovement(ctx context.Context, tx pgx.Tx, batchID int64, productID, kind, reason string, qty int64, orderID *string, at time.Time) error {
	var r *string
	if reason != "" {
		r = &reason
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO movements (batch_id, product_id, kind, reason, quantity, order_id, at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		batchID, productID, kind, r, qty, orderID, at); err != nil {
		return fmt.Errorf("store: insert %s: %w", kind, err)
	}
	return nil
}

// BatchProduct возвращает продукт партии и его базовую единицу.
func (d *DB) BatchProduct(ctx context.Context, batchID int64) (string, domain.Unit, error) {
	var pid, u string
	err := d.Pool.QueryRow(ctx, `SELECT b.product_id, p.unit FROM batches b JOIN products p ON p.id = b.product_id WHERE b.id = $1`, batchID).Scan(&pid, &u)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrUnknownBatch
	}
	if err != nil {
		return "", "", fmt.Errorf("store: batch product: %w", err)
	}
	return pid, domain.Unit(u), nil
}

// WriteOffBatch списывает брак с партии: claim → блокировка продукта → now → UPDATE и движение
// writeoff с причиной. Количество больше остатка — ErrInsufficientBatch, ничего не записано.
func (d *DB) WriteOffBatch(ctx context.Context, in WriteoffInput, now func() time.Time) (WriteoffResult, error) {
	hash := corrHash(in.ID, strconv.FormatInt(in.BatchID, 10), strconv.FormatInt(in.Qty, 10), in.Reason)
	var res WriteoffResult
	err := d.InTx(ctx, func(tx pgx.Tx) error {
		replay, err := claimCorrection(ctx, tx, corrWriteoff, in.ID, strconv.FormatInt(in.BatchID, 10), hash)
		if err != nil {
			return err
		}
		if replay != nil {
			if err := json.Unmarshal(replay, &res); err != nil {
				return fmt.Errorf("store: stored writeoff: %w", err)
			}
			res.Replayed = true
			return nil
		}
		var pid, u string
		err = tx.QueryRow(ctx, `SELECT b.product_id, p.unit FROM batches b JOIN products p ON p.id = b.product_id WHERE b.id = $1`, in.BatchID).Scan(&pid, &u)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownBatch
		}
		if err != nil {
			return fmt.Errorf("store: read batch: %w", err)
		}
		if err := LockProducts(ctx, tx, []string{pid}); err != nil {
			return err
		}
		at := now().UTC()
		var remaining int64
		if err := tx.QueryRow(ctx, `SELECT remaining FROM batches WHERE id = $1`, in.BatchID).Scan(&remaining); err != nil {
			return fmt.Errorf("store: read remaining: %w", err)
		}
		if remaining < in.Qty {
			return ErrInsufficientBatch
		}
		if err := moveBatch(ctx, tx, in.BatchID, pid, KindWriteoff, in.Reason, in.Qty, nil, at); err != nil {
			return err
		}
		unit := domain.Unit(u)
		res = WriteoffResult{ID: in.ID, BatchID: in.BatchID, ProductID: pid, Qty: QtyJSON{in.Qty, unit}, Reason: in.Reason,
			Remaining: QtyJSON{remaining - in.Qty, unit}, At: at}
		return saveCorrection(ctx, tx, corrWriteoff, in.ID, at, res)
	})
	return res, err
}

// Inventory сверяет учёт с пересчётом. Недостача списывается по FEFO движениями adjustment
// (причина inventory_shortage); излишек заводится партией поставки INV-<id> со сроком по умолчанию
// продукта (shelf_life_hours) от now и движением receipt (причина inventory_surplus).
func (d *DB) Inventory(ctx context.Context, in InventoryInput, now func() time.Time) (InventoryResult, error) {
	hash := corrHash(in.ID, in.ProductID, strconv.FormatInt(in.Counted, 10))
	var res InventoryResult
	err := d.InTx(ctx, func(tx pgx.Tx) error {
		replay, err := claimCorrection(ctx, tx, corrInventory, in.ID, in.ProductID, hash)
		if err != nil {
			return err
		}
		if replay != nil {
			if err := json.Unmarshal(replay, &res); err != nil {
				return fmt.Errorf("store: stored inventory: %w", err)
			}
			res.Replayed = true
			return nil
		}
		if err := LockProducts(ctx, tx, []string{in.ProductID}); err != nil {
			return err
		}
		at := now().UTC()
		var u string
		var shelf *int
		if err := tx.QueryRow(ctx, `SELECT unit, shelf_life_hours FROM products WHERE id = $1`, in.ProductID).Scan(&u, &shelf); err != nil {
			return fmt.Errorf("store: read product: %w", err)
		}
		unit := domain.Unit(u)
		rows, err := tx.Query(ctx,
			`SELECT id, product_id, quantity, remaining, received_at, expires_at FROM batches
			 WHERE product_id = $1 AND remaining > 0 AND expires_at > $2 ORDER BY expires_at, id`, in.ProductID, at)
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

		plan := domain.PlanInventory(in.ProductID, batches, domain.Qty(in.Counted))
		res = InventoryResult{ID: in.ID, ProductID: in.ProductID, Counted: QtyJSON{in.Counted, unit}, Shortage: []AdjustmentJSON{}, At: at}
		for _, w := range plan.Shortage {
			if err := moveBatch(ctx, tx, w.BatchID, in.ProductID, KindAdjustment, ReasonInventoryShortage, int64(w.Qty), nil, at); err != nil {
				return err
			}
			res.Shortage = append(res.Shortage, AdjustmentJSON{BatchID: w.BatchID, ProductID: w.ProductID, Qty: QtyJSON{int64(w.Qty), unit}})
		}
		if plan.Surplus > 0 {
			if shelf == nil {
				return ErrInventoryNoShelf
			}
			expires := at.Add(time.Duration(*shelf) * time.Hour)
			delivery := InventoryDeliveryPrefix + in.ID
			if _, err := tx.Exec(ctx,
				`INSERT INTO deliveries (id, number, created_at, received_at, body_hash, response) VALUES ($1, $2, $3, $3, $4, '{}')`,
				delivery, delivery, at, hash); err != nil {
				return fmt.Errorf("store: insert inventory delivery: %w", err)
			}
			var bid int64
			if err := tx.QueryRow(ctx,
				`INSERT INTO batches (delivery_id, line_no, product_id, quantity, remaining, received_at, expires_at)
				 VALUES ($1, 1, $2, $3, $3, $4, $5) RETURNING id`, delivery, in.ProductID, int64(plan.Surplus), at, expires).Scan(&bid); err != nil {
				return fmt.Errorf("store: insert surplus batch: %w", err)
			}
			if err := insertMovement(ctx, tx, bid, in.ProductID, KindReceipt, ReasonInventorySurplus, int64(plan.Surplus), nil, at); err != nil {
				return err
			}
			res.Surplus = &BatchJSON{ID: bid, LineNo: 1, ProductID: in.ProductID, Quantity: QtyJSON{int64(plan.Surplus), unit},
				ReceivedAt: at, ExpiresAt: expires, Status: string(domain.StatusOK)}
		}
		return saveCorrection(ctx, tx, corrInventory, in.ID, at, res)
	})
	return res, err
}

// CancelOrder отменяет принятый заказ один раз. Проданное из непросроченных партий возвращается
// движением return; проданное из просроченных партий остаток не восстанавливает: пишутся return
// и adjustment/return_expired на то же количество (баланс партии не меняется, след в журнале есть).
func (d *DB) CancelOrder(ctx context.Context, in CancelInput, now func() time.Time) (CancelResult, error) {
	hash := corrHash(in.ID, in.OrderID)
	var res CancelResult
	err := d.InTx(ctx, func(tx pgx.Tx) error {
		replay, err := claimCorrection(ctx, tx, corrCancel, in.ID, in.OrderID, hash)
		if err != nil {
			return err
		}
		if replay != nil {
			if err := json.Unmarshal(replay, &res); err != nil {
				return fmt.Errorf("store: stored cancel: %w", err)
			}
			res.Replayed = true
			return nil
		}
		var status string
		err = tx.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1`, in.OrderID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownOrder
		}
		if err != nil {
			return fmt.Errorf("store: read order: %w", err)
		}
		if status != OrderAccepted {
			return ErrOrderNotCancellable
		}
		rows, err := tx.Query(ctx,
			`SELECT m.batch_id, m.product_id, SUM(m.quantity), b.expires_at FROM movements m JOIN batches b ON b.id = m.batch_id
			 WHERE m.order_id = $1 AND m.kind = $2 GROUP BY m.batch_id, m.product_id, b.expires_at ORDER BY m.batch_id`, in.OrderID, KindSale)
		if err != nil {
			return fmt.Errorf("store: read sold: %w", err)
		}
		var sold []domain.Sold
		var ids []string
		for rows.Next() {
			var s domain.Sold
			var q int64
			if err := rows.Scan(&s.BatchID, &s.ProductID, &q, &s.ExpiresAt); err != nil {
				rows.Close()
				return fmt.Errorf("store: read sold: %w", err)
			}
			s.Qty = domain.Qty(q)
			sold = append(sold, s)
			ids = append(ids, s.ProductID)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("store: read sold: %w", err)
		}
		if err := LockProducts(ctx, tx, ids); err != nil {
			return err
		}
		at := now().UTC()
		_, unitsOf, err := productInfo(ctx, tx, ids)
		if err != nil {
			return err
		}
		returns, expired := domain.PlanCancel(sold, at)
		res = CancelResult{ID: in.ID, OrderID: in.OrderID, Returned: []AdjustmentJSON{}, Expired: []AdjustmentJSON{}, At: at}
		for _, w := range returns {
			if err := moveBatch(ctx, tx, w.BatchID, w.ProductID, KindReturn, "", int64(w.Qty), &in.OrderID, at); err != nil {
				return err
			}
			res.Returned = append(res.Returned, AdjustmentJSON{BatchID: w.BatchID, ProductID: w.ProductID, Qty: QtyJSON{int64(w.Qty), unitsOf[w.ProductID]}})
		}
		for _, w := range expired {
			if err := insertMovement(ctx, tx, w.BatchID, w.ProductID, KindReturn, "", int64(w.Qty), &in.OrderID, at); err != nil {
				return err
			}
			if err := insertMovement(ctx, tx, w.BatchID, w.ProductID, KindAdjustment, ReasonReturnExpired, int64(w.Qty), &in.OrderID, at); err != nil {
				return err
			}
			res.Expired = append(res.Expired, AdjustmentJSON{BatchID: w.BatchID, ProductID: w.ProductID, Qty: QtyJSON{int64(w.Qty), unitsOf[w.ProductID]}})
		}
		return saveCorrection(ctx, tx, corrCancel, in.ID, at, res)
	})
	return res, err
}
