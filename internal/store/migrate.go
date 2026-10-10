package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrateLockID — ключ advisory-блокировки: два экземпляра не мигрируют одновременно.
const migrateLockID int64 = 0x756d6972 // "umir"

// Migrate применяет ещё не применённые миграции из migrations/ по порядку имён.
// Каждая миграция идёт в своей транзакции вместе с записью в schema_migrations.
func (d *DB) Migrate(ctx context.Context) error {
	return migrate(ctx, d, migrationsFS, "migrations")
}

func migrate(ctx context.Context, d *DB, fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("store: read migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	conn, err := d.Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("store: migrate: acquire: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrateLockID); err != nil {
		return fmt.Errorf("store: migrate: lock: %w", err)
	}
	defer func() {
		// ctx мог быть отменён; разблокировку делаем независимо от него.
		_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrateLockID)
	}()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name       text PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("store: migrate: create schema_migrations: %w", err)
	}

	for _, name := range names {
		var applied bool
		if err := conn.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)", name).Scan(&applied); err != nil {
			return fmt.Errorf("store: migrate %s: %w", name, err)
		}
		if applied {
			continue
		}
		body, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return fmt.Errorf("store: migrate %s: %w", name, err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("store: migrate %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("store: migrate %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (name) VALUES ($1)", name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("store: migrate %s: record: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("store: migrate %s: commit: %w", name, err)
		}
	}
	return nil
}
