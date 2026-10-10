package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/auth"
)

// CreateSession сохраняет сессию; в БД лежит только SHA-256 токена.
func (d *DB) CreateSession(ctx context.Context, tokenHash []byte, userID int64, expiresAt time.Time) error {
	if _, err := d.Pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		tokenHash, userID, expiresAt); err != nil {
		return fmt.Errorf("store: create session: %w", err)
	}
	return nil
}

// SessionUser возвращает владельца непросроченной сессии.
func (d *DB) SessionUser(ctx context.Context, tokenHash []byte, now time.Time) (auth.User, error) {
	var u auth.User
	var role string
	err := d.Pool.QueryRow(ctx,
		`SELECT u.id, u.login, u.role, u.created_at FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = $1 AND s.expires_at > $2`, tokenHash, now).
		Scan(&u.ID, &u.Login, &role, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, auth.ErrNotFound
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("store: session user: %w", err)
	}
	u.Role = auth.Role(role)
	return u, nil
}

// DeleteSession удаляет сессию; отсутствие строки — не ошибка.
func (d *DB) DeleteSession(ctx context.Context, tokenHash []byte) error {
	if _, err := d.Pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("store: delete session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions чистит просроченные сессии.
func (d *DB) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	if _, err := d.Pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, now); err != nil {
		return fmt.Errorf("store: delete expired sessions: %w", err)
	}
	return nil
}
