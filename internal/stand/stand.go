// Package stand — управление демо-стендом: ускоренные часы, скорость, прыжок времени и сброс
// (.ai/solution.md, 2.4 и 2.8). Ядро (domain, остальной store) о пакете не знает; его подключает
// только cmd/api.
package stand

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/clock"
	"umirhack/internal/events"
	"umirhack/internal/seed"
	"umirhack/internal/store"
)

// ErrNotSimulated — часы не ускоренные (CLOCK_MODE=real): скорость и прыжок недоступны.
var ErrNotSimulated = errors.New("stand: clock is not simulated")

// SeedHour — час T0 по времени ресторана в календарный день сброса (2.8).
const SeedHour = 11

// Gate — затвор процесса (2.8): изменяющие запросы и «дворник» берут его на чтение,
// сброс — на запись. Пока затвор взят на запись, других писателей нет.
// Затвор не реентерабелен: держатель RLock не должен брать его повторно.
type Gate struct{ mu sync.RWMutex }

func (g *Gate) RLock()   { g.mu.RLock() }
func (g *Gate) RUnlock() { g.mu.RUnlock() }
func (g *Gate) Lock()    { g.mu.Lock() }
func (g *Gate) Unlock()  { g.mu.Unlock() }

// Sweeper — проход «дворника» (janitor.Janitor).
type Sweeper interface {
	Sweep(ctx context.Context) ([]store.ExpiredBatch, error)
	Kick()
}

// Stand — управление стендом.
type Stand struct {
	DB    *store.DB
	Clock *clock.Accelerated // nil — реальные часы
	Gate  *Gate
	Sweep Sweeper
	Bus   *events.Bus
	Loc   *time.Location

	// AfterChange вызывается после прыжка и сброса, уже без затвора: сверка стоп-листа
	// (stoplist.changed) делается издателем событий, который живёт в cmd/api.
	AfterChange func(ctx context.Context)
	// Wall — реальное время (для T0 сброса); nil — time.Now. Нужен тестам.
	Wall func() time.Time
}

// T0 — 11:00 по времени ресторана в календарный день момента now.
func T0(now time.Time, loc *time.Location) time.Time {
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d, SeedHour, 0, 0, 0, loc).UTC()
}

func (s *Stand) wall() time.Time {
	if s.Wall != nil {
		return s.Wall()
	}
	return time.Now()
}

// Now — время стенда.
func (s *Stand) Now() time.Time {
	if s.Clock == nil {
		return time.Now().UTC()
	}
	return s.Clock.Now()
}

// Speed, Simulated и SimulatorRunning реализуют api.ClockInfo.
func (s *Stand) Speed() float64 {
	if s.Clock == nil {
		return 1
	}
	return s.Clock.Speed()
}
func (s *Stand) Simulated() bool        { return s.Clock != nil }
func (s *Stand) SimulatorRunning() bool { return false } // симулятор управляется отдельным тикетом

func (s *Stand) clockChanged() {
	s.Bus.Publish(events.ClockChanged, map[string]any{
		"now": s.Now().UTC().Format(time.RFC3339), "speed": s.Speed()})
}

// SetSpeed меняет скорость часов (0 — пауза), будит «дворника» и шлёт clock.changed.
func (s *Stand) SetSpeed(ctx context.Context, speed float64) error {
	if s.Clock == nil {
		return ErrNotSimulated
	}
	err := s.Clock.SetSpeed(ctx, speed)
	if errors.Is(err, clock.ErrInvalidSpeed) {
		return err
	}
	s.Sweep.Kick()
	s.clockChanged()
	return err
}

// Jump переносит время вперёд, синхронно делает проход «дворника» и шлёт clock.changed.
// Проход берёт затвор на чтение сам (через обёртку хранилища «дворника»), поэтому здесь он не держится.
func (s *Stand) Jump(ctx context.Context, hours float64) (time.Time, error) {
	if s.Clock == nil {
		return time.Time{}, ErrNotSimulated
	}
	to, err := s.Clock.Jump(ctx, hours)
	if errors.Is(err, clock.ErrInvalidJump) {
		return to, err
	}
	if _, serr := s.Sweep.Sweep(ctx); serr != nil && err == nil {
		err = serr
	}
	s.Sweep.Kick()
	s.clockChanged()
	if s.AfterChange != nil {
		s.AfterChange(ctx)
	}
	return to, err
}

// Reset возвращает стенд в исходное состояние: под затвором на запись DELETE в порядке 7.1,
// загрузка сида на T0, часы на T0 со скоростью ×1. Часы в БД сбрасываются в той же транзакции.
func (s *Stand) Reset(ctx context.Context) error {
	if err := s.reset(ctx); err != nil {
		return err
	}
	s.Sweep.Kick()
	s.clockChanged()
	s.Bus.Publish(events.StandReset, nil)
	if s.AfterChange != nil {
		s.AfterChange(ctx)
	}
	return nil
}

func (s *Stand) reset(ctx context.Context) error {
	s.Gate.Lock()
	defer s.Gate.Unlock()
	t0 := T0(s.wall(), s.Loc)
	err := s.DB.InTx(ctx, func(tx pgx.Tx) error {
		if err := store.DeleteStandData(ctx, tx); err != nil {
			return err
		}
		if err := seed.Load(ctx, tx, t0); err != nil {
			return err
		}
		if s.Clock != nil {
			return store.SaveClock(ctx, tx, clock.State{SimTime: t0, Speed: 1})
		}
		return nil
	})
	if err != nil {
		return err
	}
	if s.Clock != nil {
		return s.Clock.Reset(ctx, t0)
	}
	return nil
}

// Load загружает сид на пустую БД (SEED_ON_EMPTY=true) и возвращает T0 и признак загрузки.
func Load(ctx context.Context, db *store.DB, t0 time.Time) (bool, error) {
	loaded := false
	err := db.InTx(ctx, func(tx pgx.Tx) error {
		empty, err := seed.IsEmpty(ctx, tx)
		if err != nil || !empty {
			return err
		}
		loaded = true
		return seed.Load(ctx, tx, t0)
	})
	return loaded && err == nil, err
}

// GatedJanitorStore оборачивает хранилище «дворника»: проход берёт затвор на чтение.
type GatedJanitorStore struct {
	Inner interface {
		SweepExpired(ctx context.Context, now func() time.Time) ([]store.ExpiredBatch, error)
		NextExpiry(ctx context.Context, now time.Time) (*time.Time, error)
	}
	Gate *Gate
}

func (g GatedJanitorStore) SweepExpired(ctx context.Context, now func() time.Time) ([]store.ExpiredBatch, error) {
	g.Gate.RLock()
	defer g.Gate.RUnlock()
	return g.Inner.SweepExpired(ctx, now)
}

func (g GatedJanitorStore) NextExpiry(ctx context.Context, now time.Time) (*time.Time, error) {
	return g.Inner.NextExpiry(ctx, now)
}
