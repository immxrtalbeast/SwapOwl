package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"umirhack/internal/clock"
)

// ClockStore — clock.Store на таблицах clock_state и clock_jumps (7.1).
// Время хранится в sim_saved; sim_base и wall_base нужны только для просмотра в БД:
// после рестарта простой процесса не засчитывается (2.4).
type ClockStore struct{ DB *DB }

// ClockStore возвращает хранилище состояния часов поверх этой БД.
func (d *DB) ClockStore() ClockStore { return ClockStore{DB: d} }

const saveClockSQL = `INSERT INTO clock_state (id, sim_base, wall_base, speed, sim_saved)
	VALUES (true, $1, now(), $2, $1)
	ON CONFLICT (id) DO UPDATE SET sim_base = EXCLUDED.sim_base, wall_base = EXCLUDED.wall_base,
		speed = EXCLUDED.speed, sim_saved = EXCLUDED.sim_saved`

// Load читает сохранённое состояние; ok=false, если строки ещё нет.
func (s ClockStore) Load(ctx context.Context) (clock.State, bool, error) {
	var st clock.State
	err := s.DB.Pool.QueryRow(ctx, `SELECT sim_saved, speed::float8 FROM clock_state WHERE id`).Scan(&st.SimTime, &st.Speed)
	if errors.Is(err, pgx.ErrNoRows) {
		return clock.State{}, false, nil
	}
	if err != nil {
		return clock.State{}, false, fmt.Errorf("store: load clock: %w", err)
	}
	st.SimTime = st.SimTime.UTC()
	return st, true, nil
}

// Save сохраняет состояние.
func (s ClockStore) Save(ctx context.Context, st clock.State) error {
	return SaveClock(ctx, s.DB.Pool, st)
}

// RecordJump сохраняет состояние и пишет прыжок в журнал одной транзакцией.
func (s ClockStore) RecordJump(ctx context.Context, j clock.Jump, st clock.State) error {
	return s.DB.InTx(ctx, func(tx pgx.Tx) error {
		if err := SaveClock(ctx, tx, st); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO clock_jumps (from_at, to_at) VALUES ($1, $2)`, j.From.UTC(), j.To.UTC()); err != nil {
			return fmt.Errorf("store: record jump: %w", err)
		}
		return nil
	})
}

// Execer — pgxpool.Pool и pgx.Tx.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// SaveClock пишет единственную строку clock_state через пул или транзакцию.
func SaveClock(ctx context.Context, q Execer, st clock.State) error {
	if _, err := q.Exec(ctx, saveClockSQL, st.SimTime.UTC(), st.Speed); err != nil {
		return fmt.Errorf("store: save clock: %w", err)
	}
	return nil
}

// LastEventTime — max(movements.at, orders.at, batches.received_at): нижняя граница времени
// стенда после рестарта (2.4). Нулевое время — событий ещё не было.
func (d *DB) LastEventTime(ctx context.Context) (time.Time, error) {
	var t *time.Time
	err := d.Pool.QueryRow(ctx, `SELECT GREATEST(
		(SELECT max(at) FROM movements), (SELECT max(at) FROM orders), (SELECT max(received_at) FROM batches))`).Scan(&t)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: last event time: %w", err)
	}
	if t == nil {
		return time.Time{}, nil
	}
	return t.UTC(), nil
}

// SimulatorRunning читает признак «генератор заказов включён»; false, если строки часов ещё нет.
func (s ClockStore) SimulatorRunning(ctx context.Context) (bool, error) {
	var on bool
	err := s.DB.Pool.QueryRow(ctx, `SELECT simulator_running FROM clock_state WHERE id`).Scan(&on)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: load simulator state: %w", err)
	}
	return on, nil
}

// SetSimulatorRunning сохраняет признак отдельным UPDATE, не трогая время и скорость.
// Строки часов ещё нет (в симулированном режиме она появляется при первом сохранении часов) — no-op.
func (s ClockStore) SetSimulatorRunning(ctx context.Context, on bool) error {
	if _, err := s.DB.Pool.Exec(ctx, `UPDATE clock_state SET simulator_running = $1 WHERE id`, on); err != nil {
		return fmt.Errorf("store: save simulator state: %w", err)
	}
	return nil
}
