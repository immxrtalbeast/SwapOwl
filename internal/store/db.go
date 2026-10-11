package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"umirhack/internal/domain"
)

// acquireTimeout — сколько запрос ждёт свободное соединение пула (.ai/solution.md, 7.6).
const acquireTimeout = 5 * time.Second

// ErrPoolTimeout возвращается, когда свободного соединения не нашлось за acquireTimeout.
// API превращает её в 503 overloaded.
var ErrPoolTimeout = fmt.Errorf("store: no free database connection within %s", acquireTimeout)

// DB — пул соединений PostgreSQL.
type DB struct {
	Pool *pgxpool.Pool
	// LateEvents — окно событий задним числом (LATE_EVENTS_HOURS): occurred_at допустим в
	// [now − LateEvents, now], «дворник» списывает партию не раньше expires_at + LateEvents.
	// Ноль — окна нет (как до 2.4а).
	LateEvents time.Duration
	// Expiry — порог «истекает» для поля expiring в GET /api/products (.ai/solution.md, 2.4).
	Expiry domain.ExpiryConfig
}

// Open подключается к PostgreSQL и проверяет соединение.
func Open(ctx context.Context, url string, poolSize int) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		// pgx включает в текст ошибки часть строки подключения; пароль наружу не отдаём.
		return nil, fmt.Errorf("store: invalid DATABASE_URL")
	}
	cfg.MaxConns = int32(poolSize)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: open pool: %w", err)
	}
	db := &DB{Pool: pool}
	if err := db.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return db, nil
}

// Ping проверяет доступность БД; ожидание ограничено таймаутом из ctx и 5 секундами.
func (d *DB) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, acquireTimeout)
	defer cancel()
	if err := d.Pool.Ping(ctx); err != nil {
		return fmt.Errorf("store: ping: %w", err)
	}
	return nil
}

// Close закрывает пул.
func (d *DB) Close() { d.Pool.Close() }
