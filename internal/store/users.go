package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"umirhack/internal/auth"
)

var _ auth.Repo = (*DB)(nil)

const pgUniqueViolation = "23505"

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == pgUniqueViolation
}

// CountUsers — число пользователей.
func (d *DB) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := d.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count users: %w", err)
	}
	return n, nil
}

// CreateUser добавляет пользователя; занятый login — auth.ErrExists.
func (d *DB) CreateUser(ctx context.Context, login, passwordHash string, role auth.Role) (auth.User, error) {
	u := auth.User{Login: login, Role: role, PasswordHash: passwordHash}
	err := d.Pool.QueryRow(ctx,
		`INSERT INTO users (login, password_hash, role) VALUES ($1, $2, $3) RETURNING id, created_at`,
		login, passwordHash, string(role)).Scan(&u.ID, &u.CreatedAt)
	if isUnique(err) {
		return auth.User{}, auth.ErrExists
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("store: create user: %w", err)
	}
	return u, nil
}

// UserByLogin ищет пользователя по логину.
func (d *DB) UserByLogin(ctx context.Context, login string) (auth.User, error) {
	var u auth.User
	var role string
	err := d.Pool.QueryRow(ctx,
		`SELECT id, login, password_hash, role, created_at FROM users WHERE login = $1`, login).
		Scan(&u.ID, &u.Login, &u.PasswordHash, &role, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, auth.ErrNotFound
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("store: user by login: %w", err)
	}
	u.Role = auth.Role(role)
	return u, nil
}

// ListUsers — все пользователи по возрастанию id.
func (d *DB) ListUsers(ctx context.Context) ([]auth.User, error) {
	rows, err := d.Pool.Query(ctx, `SELECT id, login, role, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	defer rows.Close()
	out := []auth.User{}
	for rows.Next() {
		var u auth.User
		var role string
		if err := rows.Scan(&u.ID, &u.Login, &role, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: list users: %w", err)
		}
		u.Role = auth.Role(role)
		out = append(out, u)
	}
	return out, rows.Err()
}

// DeleteUser удаляет пользователя (сессии уходят каскадом). Последнего admin удалить нельзя:
// строки admin блокируются на время проверки, поэтому два параллельных удаления не оставят систему без admin.
func (d *DB) DeleteUser(ctx context.Context, id int64) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `SELECT id FROM users WHERE role = 'admin' ORDER BY id FOR UPDATE`)
	if err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	admins := map[int64]bool{}
	for rows.Next() {
		var aid int64
		if err := rows.Scan(&aid); err != nil {
			rows.Close()
			return fmt.Errorf("store: delete user: %w", err)
		}
		admins[aid] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	if admins[id] && len(admins) <= 1 {
		return auth.ErrLastAdmin
	}
	tag, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	return nil
}
