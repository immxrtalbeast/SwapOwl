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

// TakeLoginAttempt — счётчик неудачных входов в БД (login_attempts). Проверка и занятие попытки
// атомарны: транзакция берёт advisory-блокировку по ключу, поэтому параллельные запросы по одному
// логину не обходят лимит. Старые отметки (всех ключей) чистятся при каждом вызове.
func (d *DB) TakeLoginAttempt(ctx context.Context, key string, now time.Time, window time.Duration, max int) (bool, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("store: login attempt: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 7))`, key); err != nil {
		return false, fmt.Errorf("store: login attempt: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM login_attempts WHERE at <= $1`, now.Add(-window)); err != nil {
		return false, fmt.Errorf("store: login attempt: %w", err)
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM login_attempts WHERE login = $1`, key).Scan(&n); err != nil {
		return false, fmt.Errorf("store: login attempt: %w", err)
	}
	if n >= max {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO login_attempts (login, at) VALUES ($1, $2)`, key, now); err != nil {
		return false, fmt.Errorf("store: login attempt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("store: login attempt: %w", err)
	}
	return true, nil
}

// ClearLoginAttempts сбрасывает счётчик ключа.
func (d *DB) ClearLoginAttempts(ctx context.Context, key string) error {
	if _, err := d.Pool.Exec(ctx, `DELETE FROM login_attempts WHERE login = $1`, key); err != nil {
		return fmt.Errorf("store: clear login attempts: %w", err)
	}
	return nil
}
