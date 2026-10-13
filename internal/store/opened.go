package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/domain"
)

const (
	corrOpen = "open"

	// OpenedDeliveryPrefix — префикс id поставки, под которой заводится дочерняя партия вскрытия.
	OpenedDeliveryPrefix = "OPEN-"
)

// ErrAlreadyOpened — партия уже выделена вскрытием (opened_at задан): вскрыть её повторно нельзя.
var ErrAlreadyOpened = errors.New("store: batch is already opened")

// OpenInput — вскрытие упаковки. Qty == nil — по умолчанию (упаковка продукта или вся партия).
type OpenInput struct {
	ID      string
	BatchID int64
	Qty     *int64
}

// OpenResult — ответ POST /api/batches/{id}/open.
type OpenResult struct {
	ID            string    `json:"id"`
	ParentBatchID int64     `json:"parent_batch_id"`
	ProductID     string    `json:"product_id"`
	Opened        QtyJSON   `json:"opened"`
	ParentLeft    QtyJSON   `json:"parent_remaining"`
	Child         BatchJSON `json:"child"`
	OpenedAt      time.Time `json:"opened_at"`
	ShelfHours    int       `json:"shelf_after_open_hours"`
	Replayed      bool      `json:"replayed"`
}

// OpenBatch вскрывает упаковку: из исходной партии уходит qty движением transfer_out/opened,
// создаётся дочерняя партия (parent_batch_id, opened_at = now) с остатком qty, сроком
// min(исходный срок, now + shelf_after_open_hours) и движением transfer_in/opened.
// claim → блокировка продукта → now → правила → запись, всё в одной транзакции.
func (d *DB) OpenBatch(ctx context.Context, in OpenInput, now func() time.Time) (OpenResult, error) {
	qtyArg := ""
	if in.Qty != nil {
		qtyArg = strconv.FormatInt(*in.Qty, 10)
	}
	hash := corrHash(in.ID, strconv.FormatInt(in.BatchID, 10), qtyArg)
	var res OpenResult
	err := d.InTx(ctx, func(tx pgx.Tx) error {
		replay, err := claimCorrection(ctx, tx, corrOpen, in.ID, strconv.FormatInt(in.BatchID, 10), hash)
		if err != nil {
			return err
		}
		if replay != nil {
			if err := json.Unmarshal(replay, &res); err != nil {
				return fmt.Errorf("store: stored open: %w", err)
			}
			res.Replayed = true
			return nil
		}
		var pid string
		err = tx.QueryRow(ctx, `SELECT product_id FROM batches WHERE id = $1`, in.BatchID).Scan(&pid)
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
		var (
			b         domain.Batch
			unit      string
			openedAt  *time.Time
			shelfOpen *int
			packSize  int64
		)
		err = tx.QueryRow(ctx,
			`SELECT b.id, b.product_id, b.quantity, b.remaining, b.received_at, b.expires_at,
			        p.unit, p.shelf_after_open_hours, p.pack_size, b.opened_at
			 FROM batches b JOIN products p ON p.id = b.product_id WHERE b.id = $1`, in.BatchID).
			Scan(&b.ID, &b.ProductID, &b.Quantity, &b.Remaining, &b.ReceivedAt, &b.ExpiresAt,
				&unit, &shelfOpen, &packSize, &openedAt)
		if err != nil {
			return fmt.Errorf("store: read batch: %w", err)
		}
		if openedAt != nil {
			return ErrAlreadyOpened
		}
		rules := domain.OpenRules{PackSize: domain.Qty(packSize)}
		if shelfOpen != nil {
			rules.AfterOpenHours = *shelfOpen
		}
		var qty *domain.Qty
		if in.Qty != nil {
			q := domain.Qty(*in.Qty)
			qty = &q
		}
		plan, err := domain.PlanOpen(b, rules, qty, at)
		if err != nil {
			return err
		}
		if err := moveBatch(ctx, tx, b.ID, pid, KindTransferOut, ReasonOpened, int64(plan.Qty), nil, at); err != nil {
			return err
		}
		delivery := OpenedDeliveryPrefix + in.ID
		if _, err := tx.Exec(ctx,
			`INSERT INTO deliveries (id, number, created_at, received_at, body_hash, response) VALUES ($1, $2, $3, $3, $4, '{}')`,
			delivery, delivery, at, hash); err != nil {
			return fmt.Errorf("store: insert open delivery: %w", err)
		}
		// Срок дочерней партии вычислен здесь, а не пришёл от поставщика: confirmed не требует подтверждения при приёмке.
		var child int64
		if err := tx.QueryRow(ctx,
			`INSERT INTO batches (delivery_id, line_no, product_id, quantity, remaining, received_at, expires_at, expiry_source, parent_batch_id, opened_at)
			 VALUES ($1, 1, $2, $3, $3, $4, $5, 'confirmed', $6, $4) RETURNING id`,
			delivery, pid, int64(plan.Qty), at, plan.ChildExpiresAt, b.ID).Scan(&child); err != nil {
			return fmt.Errorf("store: insert child batch: %w", err)
		}
		if err := insertMovement(ctx, tx, child, pid, KindTransferIn, ReasonOpened, int64(plan.Qty), nil, at); err != nil {
			return err
		}
		u := domain.Unit(unit)
		res = OpenResult{ID: in.ID, ParentBatchID: b.ID, ProductID: pid,
			Opened:     QtyJSON{int64(plan.Qty), u},
			ParentLeft: QtyJSON{int64(b.Remaining - plan.Qty), u},
			Child: BatchJSON{ID: child, LineNo: 1, ProductID: pid, Quantity: QtyJSON{int64(plan.Qty), u},
				ReceivedAt: at, ExpiresAt: plan.ChildExpiresAt, Status: string(domain.StatusOK)},
			OpenedAt: at, ShelfHours: rules.AfterOpenHours}
		return saveCorrection(ctx, tx, corrOpen, in.ID, at, res)
	})
	return res, err
}
