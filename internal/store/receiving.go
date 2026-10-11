package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/domain"
)

// Источники срока годности партии (2.1а).
const (
	ExpirySourceDelivery       = "delivery"
	ExpirySourceProductDefault = "product_default"
	ExpirySourceConfirmed      = "confirmed"
)

// ReceivingWindow — за какое время стенда партии с неподтверждённым сроком показываются на приёмке.
const ReceivingWindow = 48 * time.Hour

// Ошибки подтверждения срока.
var (
	// ErrBatchClosed — партия просрочена или списана в ноль; срок менять нельзя.
	ErrBatchClosed = errors.New("store: batch is expired or written off")
	// ErrExpiryNotFuture — новый срок не позже now.
	ErrExpiryNotFuture = errors.New("store: new expiry is not in the future")
)

// ExpiryInput — подтверждение срока: ровно одно из ExpiresOn (YYYY-MM-DD, конец дня по поясу
// ресторана) и ShelfLifeHours (> 0, от received_at партии).
type ExpiryInput struct {
	BatchID        int64
	ExpiresOn      string
	ShelfLifeHours int
}

// ExpiryResult — ответ PATCH /api/batches/{id}/expiry.
type ExpiryResult struct {
	BatchID         int64     `json:"batch_id"`
	ProductID       string    `json:"product_id"`
	ExpiresAt       time.Time `json:"expires_at"`
	OldExpiresAt    time.Time `json:"-"`
	OldSource       string    `json:"-"`
	ExpirySource    string    `json:"expiry_source"`
	ReceivedAt      time.Time `json:"received_at"`
	TimeLeftSeconds int64     `json:"time_left_seconds"`
}

// ConfirmExpiry ставит партии подтверждённый срок: блокировка продукта → now → проверки → UPDATE.
// Движение не пишется. Метка «истекает» сбрасывается, чтобы «дворник» оценил новый срок заново.
func (d *DB) ConfirmExpiry(ctx context.Context, in ExpiryInput, now func() time.Time, loc *time.Location) (ExpiryResult, error) {
	pid, _, err := d.BatchProduct(ctx, in.BatchID)
	if err != nil {
		return ExpiryResult{}, err
	}
	var res ExpiryResult
	err = d.InTx(ctx, func(tx pgx.Tx) error {
		if err := LockProducts(ctx, tx, []string{pid}); err != nil {
			return err
		}
		cur := now().UTC()
		var remaining int64
		var received, expires time.Time
		var writtenOff *time.Time
		if err := tx.QueryRow(ctx,
			`SELECT remaining, received_at, expires_at, written_off_at, expiry_source FROM batches WHERE id = $1`,
			in.BatchID).Scan(&remaining, &received, &expires, &writtenOff, &res.OldSource); err != nil {
			return fmt.Errorf("store: read batch: %w", err)
		}
		if remaining == 0 || writtenOff != nil || !expires.After(cur) {
			return ErrBatchClosed
		}
		next := received.Add(time.Duration(in.ShelfLifeHours) * time.Hour)
		if in.ExpiresOn != "" {
			day, err := time.ParseInLocation("2006-01-02", in.ExpiresOn, loc)
			if err != nil {
				return fmt.Errorf("store: expires_on %q: %w", in.ExpiresOn, err)
			}
			next = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc).UTC()
		}
		if !next.After(cur) {
			return ErrExpiryNotFuture
		}
		if _, err := tx.Exec(ctx,
			`UPDATE batches SET expires_at = $2, expiry_source = 'confirmed', expiring_notified_at = NULL WHERE id = $1`,
			in.BatchID, next); err != nil {
			return fmt.Errorf("store: confirm expiry: %w", err)
		}
		res.BatchID, res.ProductID, res.ExpiresAt, res.OldExpiresAt = in.BatchID, pid, next, expires
		res.ExpirySource, res.ReceivedAt = ExpirySourceConfirmed, received
		res.TimeLeftSeconds = int64(next.Sub(cur) / time.Second)
		return nil
	})
	return res, err
}

// ReceivingBatch — строка GET /api/receiving.
type ReceivingBatch struct {
	ID              int64     `json:"id"`
	ProductID       string    `json:"product_id"`
	ProductName     string    `json:"product_name"`
	DeliveryID      string    `json:"delivery_id"`
	DeliveryNumber  string    `json:"delivery_number"`
	Quantity        QtyJSON   `json:"quantity"`
	Remaining       QtyJSON   `json:"remaining"`
	ReceivedAt      time.Time `json:"received_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	TimeLeftSeconds int64     `json:"time_left_seconds"`
	ExpirySource    string    `json:"expiry_source"`
}

// ListReceiving — действующие партии со сроком из продукта, принятые не раньше now − 48 ч;
// новые сверху. Просроченные и списанные в ноль не показываются: срок им уже не поменять.
func (d *DB) ListReceiving(ctx context.Context, now time.Time) ([]ReceivingBatch, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT b.id, b.product_id, p.name, b.delivery_id, dl.number, b.quantity, b.remaining,
		       b.received_at, b.expires_at, p.unit
		FROM batches b
		JOIN deliveries dl ON dl.id = b.delivery_id
		JOIN products p ON p.id = b.product_id
		WHERE b.expiry_source = 'product_default'
		  AND b.remaining > 0 AND b.written_off_at IS NULL AND b.expires_at > $1
		  AND b.received_at >= $2
		ORDER BY b.received_at DESC, b.id DESC`, now, now.Add(-ReceivingWindow))
	if err != nil {
		return nil, fmt.Errorf("store: list receiving: %w", err)
	}
	defer rows.Close()
	out := []ReceivingBatch{}
	for rows.Next() {
		var b ReceivingBatch
		var u string
		if err := rows.Scan(&b.ID, &b.ProductID, &b.ProductName, &b.DeliveryID, &b.DeliveryNumber, &b.Quantity.Qty,
			&b.Remaining.Qty, &b.ReceivedAt, &b.ExpiresAt, &u); err != nil {
			return nil, fmt.Errorf("store: list receiving: %w", err)
		}
		b.Quantity.Unit, b.Remaining.Unit = domain.Unit(u), domain.Unit(u)
		b.TimeLeftSeconds = int64(b.ExpiresAt.Sub(now) / time.Second)
		b.ExpirySource = ExpirySourceProductDefault
		out = append(out, b)
	}
	return out, rows.Err()
}
