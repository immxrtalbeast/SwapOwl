package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/auth"
)

// CreateAPIKey сохраняет ключ интеграции; в БД лежит только SHA-256.
func (d *DB) CreateAPIKey(ctx context.Context, name string, keyHash []byte, role auth.Role) (auth.APIKey, error) {
	k := auth.APIKey{Name: name, Role: role}
	err := d.Pool.QueryRow(ctx,
		`INSERT INTO api_keys (name, key_hash, role) VALUES ($1, $2, $3) RETURNING id, created_at`,
		name, keyHash, string(role)).Scan(&k.ID, &k.CreatedAt)
	if isUnique(err) {
		return auth.APIKey{}, auth.ErrExists
	}
	if err != nil {
		return auth.APIKey{}, fmt.Errorf("store: create api key: %w", err)
	}
	return k, nil
}

// APIKeyByHash ищет действующий ключ.
func (d *DB) APIKeyByHash(ctx context.Context, keyHash []byte) (auth.APIKey, error) {
	var k auth.APIKey
	var role string
	err := d.Pool.QueryRow(ctx,
		`SELECT id, name, role, created_at FROM api_keys WHERE key_hash = $1`, keyHash).
		Scan(&k.ID, &k.Name, &role, &k.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.APIKey{}, auth.ErrNotFound
	}
	if err != nil {
		return auth.APIKey{}, fmt.Errorf("store: api key by hash: %w", err)
	}
	k.Role = auth.Role(role)
	return k, nil
}

// ListAPIKeys — ключи без секретов.
func (d *DB) ListAPIKeys(ctx context.Context) ([]auth.APIKey, error) {
	rows, err := d.Pool.Query(ctx, `SELECT id, name, role, created_at FROM api_keys ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list api keys: %w", err)
	}
	defer rows.Close()
	out := []auth.APIKey{}
	for rows.Next() {
		var k auth.APIKey
		var role string
		if err := rows.Scan(&k.ID, &k.Name, &role, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: list api keys: %w", err)
		}
		k.Role = auth.Role(role)
		out = append(out, k)
	}
	return out, rows.Err()
}

// DeleteAPIKey отзывает ключ удалением строки.
func (d *DB) DeleteAPIKey(ctx context.Context, id int64) error {
	tag, err := d.Pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: delete api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrNotFound
	}
	return nil
}
