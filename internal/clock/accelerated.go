package clock

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

// MaxJumpHours — верхняя граница прыжка.
const MaxJumpHours = 720

var (
	ErrInvalidSpeed = errors.New("clock: speed must be finite and >= 0")
	ErrInvalidJump  = errors.New("clock: jump hours must satisfy 0 < hours <= 720")
)

// Accelerated — ускоренные часы: now = simBase + (прошедшее с wallBase) × speed.
// Прошедшее измеряется монотонным временем процесса.
type Accelerated struct {
	store Store

	ops sync.Mutex // сериализует смену скорости, паузу, прыжок и сохранение

	mu       sync.RWMutex // защищает точки отсчёта
	simBase  time.Time
	wallBase time.Time // содержит монотонную составляющую
	speed    float64
}

// NewAccelerated восстанавливает часы из хранилища. initial используется, если
// состояния ещё нет; speed по умолчанию — 1. sim_base = max(сохранённое время,
// initial при пустом хранилище, lastEvent): время не идёт назад после рестарта.
func NewAccelerated(ctx context.Context, store Store, initial, lastEvent time.Time) (*Accelerated, error) {
	st, ok, err := store.Load(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		st = State{SimTime: initial, Speed: 1}
	}
	if st.Speed < 0 || math.IsNaN(st.Speed) || math.IsInf(st.Speed, 0) {
		return nil, ErrInvalidSpeed
	}
	base := st.SimTime
	if lastEvent.After(base) {
		base = lastEvent
	}
	c := &Accelerated{store: store, simBase: base.UTC(), wallBase: time.Now(), speed: st.Speed}
	if err := store.Save(ctx, State{SimTime: c.simBase, Speed: c.speed}); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Accelerated) at(wall time.Time) time.Time {
	d := wall.Sub(c.wallBase)
	if d < 0 {
		d = 0
	}
	return c.simBase.Add(time.Duration(float64(d) * c.speed))
}

// Now возвращает текущее время стенда. Момент реального времени берётся под
// замком, поэтому читатель не может «обогнать» перебазирование.
func (c *Accelerated) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.at(time.Now())
}

// Speed возвращает текущую скорость.
func (c *Accelerated) Speed() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.speed
}

// rebase атомарно ставит новую скорость и сдвиг и возвращает прежнее и новое
// время. Реальное время читается под тем же замком, что и у читателей.
func (c *Accelerated) rebase(speed float64, shift time.Duration) (from, to time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	wall := time.Now()
	from = c.at(wall)
	to = from.Add(shift)
	c.simBase, c.wallBase, c.speed = to, wall, speed
	return from, to
}

// SetSpeed меняет скорость, перебазируя обе точки. Скорость 0 — пауза.
// Состояние в памяти меняется всегда; при ошибке хранилища она возвращается,
// а запись повторит Persist (время в памяти при этом не откатывается).
func (c *Accelerated) SetSpeed(ctx context.Context, speed float64) error {
	if speed < 0 || math.IsNaN(speed) || math.IsInf(speed, 0) {
		return ErrInvalidSpeed
	}
	c.ops.Lock()
	defer c.ops.Unlock()
	_, to := c.rebase(speed, 0)
	return c.store.Save(ctx, State{SimTime: to, Speed: speed})
}

// Pause ставит часы на паузу (скорость 0).
func (c *Accelerated) Pause(ctx context.Context) error { return c.SetSpeed(ctx, 0) }

// Jump переносит время вперёд на hours (0 < hours ≤ 720), сохраняя скорость.
// Возвращает новое время стенда. Семантика ошибки хранилища — как у SetSpeed.
func (c *Accelerated) Jump(ctx context.Context, hours float64) (time.Time, error) {
	if !(hours > 0 && hours <= MaxJumpHours) {
		return time.Time{}, ErrInvalidJump
	}
	c.ops.Lock()
	defer c.ops.Unlock()
	speed := c.Speed()
	from, to := c.rebase(speed, time.Duration(hours*float64(time.Hour)))
	err := c.store.RecordJump(ctx, Jump{From: from, To: to, Hours: hours}, State{SimTime: to, Speed: speed})
	return to, err
}

// Persist сохраняет текущее симулированное время (вызывать не реже раза в 5 с).
func (c *Accelerated) Persist(ctx context.Context) error {
	c.ops.Lock()
	defer c.ops.Unlock()
	c.mu.RLock()
	st := State{SimTime: c.at(time.Now()), Speed: c.speed}
	c.mu.RUnlock()
	return c.store.Save(ctx, st)
}

// finalPersistTimeout ограничивает последнее сохранение при остановке.
const finalPersistTimeout = 5 * time.Second

// Run периодически сохраняет время, пока ctx не отменён. При отмене делает последнее
// сохранение с отдельным контекстом и таймаутом (исходный уже отменён), чтобы после рестарта
// время стенда не откатывалось к последнему тику.
func (c *Accelerated) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			fctx, cancel := context.WithTimeout(context.Background(), finalPersistTimeout)
			_ = c.Persist(fctx)
			cancel()
			return
		case <-t.C:
			_ = c.Persist(ctx)
		}
	}
}

// Reset ставит часы на момент t со скоростью 1 (сброс стенда, 2.8). В отличие от Jump,
// допускает перенос назад: данные стенда в этот момент заменены сидом целиком.
func (c *Accelerated) Reset(ctx context.Context, t time.Time) error {
	c.ops.Lock()
	defer c.ops.Unlock()
	c.mu.Lock()
	c.simBase, c.wallBase, c.speed = t.UTC(), time.Now(), 1
	c.mu.Unlock()
	return c.store.Save(ctx, State{SimTime: t.UTC(), Speed: 1})
}
