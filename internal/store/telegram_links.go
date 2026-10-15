package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"umirhack/internal/auth"
)

// TelegramLogin возвращает логин пользователя, к которому привязан Telegram-аккаунт,
// или auth.ErrNotFound.
func (d *DB) TelegramLogin(ctx context.Context, telegramUserID int64) (string, error) {
	var login string
	err := d.Pool.QueryRow(ctx,
		`SELECT login FROM telegram_links WHERE telegram_user_id = $1`, telegramUserID).Scan(&login)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", auth.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("store: telegram login: %w", err)
	}
	return login, nil
}

// LinkTelegram привязывает аккаунт к пользователю, заменяя прежнюю привязку этого пользователя.
// auth.ErrNotFound — нет такого пользователя; auth.ErrExists — аккаунт уже привязан к другому.
func (d *DB) LinkTelegram(ctx context.Context, login string, telegramUserID int64) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: link telegram: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var one int
	switch err := tx.QueryRow(ctx, `SELECT 1 FROM users WHERE login = $1 FOR SHARE`, login).Scan(&one); {
	case errors.Is(err, pgx.ErrNoRows):
		return auth.ErrNotFound
	case err != nil:
		return fmt.Errorf("store: link telegram: %w", err)
	}
	var owner string
	switch err := tx.QueryRow(ctx,
		`SELECT login FROM telegram_links WHERE telegram_user_id = $1 FOR UPDATE`, telegramUserID).Scan(&owner); {
	case err == nil && owner != login:
		return auth.ErrExists
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("store: link telegram: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM telegram_links WHERE login = $1`, login); err != nil {
		return fmt.Errorf("store: link telegram: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO telegram_links (telegram_user_id, login) VALUES ($1, $2)`, telegramUserID, login); err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" { // параллельная привязка того же аккаунта
			return auth.ErrExists
		}
		return fmt.Errorf("store: link telegram: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: link telegram: %w", err)
	}
	return nil
}

// TelegramLinked — привязан ли пользователь к какому-либо Telegram-аккаунту.
func (d *DB) TelegramLinked(ctx context.Context, login string) (bool, error) {
	var one int
	err := d.Pool.QueryRow(ctx, `SELECT 1 FROM telegram_links WHERE login = $1`, login).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: telegram linked: %w", err)
	}
	return true, nil
}

// UnlinkTelegram снимает привязку пользователя; auth.ErrNotFound — привязки не было.
func (d *DB) UnlinkTelegram(ctx context.Context, login string) error {
	tag, err := d.Pool.Exec(ctx, `DELETE FROM telegram_links WHERE login = $1`, login)
	if err != nil {
		return fmt.Errorf("store: unlink telegram: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrNotFound
	}
	return nil
}
