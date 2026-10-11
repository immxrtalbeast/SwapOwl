package store

import (
	"context"
	"encoding/json"
	"fmt"

	"umirhack/internal/audit"
)

var _ audit.Sink = (*DB)(nil)

// InsertAudit добавляет запись журнала действий.
func (d *DB) InsertAudit(ctx context.Context, e audit.Entry) error {
	details, err := json.Marshal(e.Details)
	if err != nil {
		return fmt.Errorf("store: audit details: %w", err)
	}
	if _, err := d.Pool.Exec(ctx,
		`INSERT INTO audit_log (at, actor, action, details) VALUES ($1, $2, $3, $4)`,
		e.At, e.Actor, e.Action, details); err != nil {
		return fmt.Errorf("store: insert audit: %w", err)
	}
	return nil
}

// ListAudit возвращает до limit последних записей, новые сверху.
func (d *DB) ListAudit(ctx context.Context, limit int) ([]audit.Entry, error) {
	rows, err := d.Pool.Query(ctx,
		`SELECT id, at, actor, action, details FROM audit_log ORDER BY at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list audit: %w", err)
	}
	defer rows.Close()
	out := []audit.Entry{}
	for rows.Next() {
		var e audit.Entry
		var details []byte
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.Action, &details); err != nil {
			return nil, fmt.Errorf("store: list audit: %w", err)
		}
		if err := json.Unmarshal(details, &e.Details); err != nil {
			return nil, fmt.Errorf("store: audit details: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
