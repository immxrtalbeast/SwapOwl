package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Виды исходящих сообщений.
const OutboxStoplist = "stoplist"

// ErrOutboxNotFound — строки outbox с таким id нет.
var ErrOutboxNotFound = errors.New("store: outbox entry not found")

// Границы limit в ListOutbox.
const (
	DefaultOutboxLimit = 100
	MaxOutboxLimit     = 1000
)

// OutboxEntry — недоставленное исходящее сообщение.
type OutboxEntry struct {
	ID        int64           `json:"id"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
	Attempts  int             `json:"attempts"`
	LastError *string         `json:"last_error"`
}

// EnqueueOutbox кладёт сообщение в очередь и возвращает его id.
func (d *DB) EnqueueOutbox(ctx context.Context, kind string, payload json.RawMessage, now time.Time) (int64, error) {
	var id int64
	err := d.Pool.QueryRow(ctx,
		`INSERT INTO outbox (kind, payload, created_at) VALUES ($1, $2, $3) RETURNING id`,
		kind, []byte(payload), now).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: enqueue outbox: %w", err)
	}
	return id, nil
}

// ListOutbox возвращает недоставленные сообщения с id > after по возрастанию id.
// limit <= 0 — DefaultOutboxLimit.
func (d *DB) ListOutbox(ctx context.Context, after int64, limit int) ([]OutboxEntry, error) {
	if limit <= 0 {
		limit = DefaultOutboxLimit
	}
	rows, err := d.Pool.Query(ctx, `
		SELECT id, kind, payload, created_at, attempts, last_error
		FROM outbox WHERE delivered_at IS NULL AND id > $1 ORDER BY id LIMIT $2`, after, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list outbox: %w", err)
	}
	defer rows.Close()
	out := []OutboxEntry{}
	for rows.Next() {
		var e OutboxEntry
		var payload []byte
		if err := rows.Scan(&e.ID, &e.Kind, &payload, &e.CreatedAt, &e.Attempts, &e.LastError); err != nil {
			return nil, fmt.Errorf("store: list outbox: %w", err)
		}
		e.Payload = payload
		out = append(out, e)
	}
	return out, rows.Err()
}

// AckOutbox подтверждает доставку (ok) или фиксирует неудачу: attempts+1 и last_error.
// Уже доставленное сообщение не меняется, повторный ack — не ошибка.
func (d *DB) AckOutbox(ctx context.Context, id int64, ok bool, errMsg string, now time.Time) error {
	var tag int64
	var err error
	if ok {
		err = d.Pool.QueryRow(ctx, `
			WITH t AS (SELECT id FROM outbox WHERE id = $1),
			u AS (UPDATE outbox SET delivered_at = $2, last_error = NULL
			      WHERE id = $1 AND delivered_at IS NULL RETURNING id)
			SELECT count(*) FROM t`, id, now).Scan(&tag)
	} else {
		err = d.Pool.QueryRow(ctx, `
			WITH t AS (SELECT id FROM outbox WHERE id = $1),
			u AS (UPDATE outbox SET attempts = attempts + 1, last_error = $2
			      WHERE id = $1 AND delivered_at IS NULL RETURNING id)
			SELECT count(*) FROM t`, id, errMsg).Scan(&tag)
	}
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && tag == 0) {
		return ErrOutboxNotFound
	}
	if err != nil {
		return fmt.Errorf("store: ack outbox: %w", err)
	}
	return nil
}
