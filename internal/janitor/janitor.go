// Package janitor — фоновый «дворник»: списывает просроченные партии (.ai/solution.md, 2.4).
// Корректность остатков от него не зависит: доступность считается по now < expires_at.
package janitor

import (
	"context"
	"log/slog"
	"time"

	"umirhack/internal/store"
)

// Store — то, что нужно «дворнику» от хранилища.
type Store interface {
	SweepExpired(ctx context.Context, now func() time.Time) ([]store.ExpiredBatch, error)
	NextExpiry(ctx context.Context, now time.Time) (*time.Time, error)
}

// Janitor периодически вызывает SweepExpired.
type Janitor struct {
	Store       Store
	Now         func() time.Time
	MinInterval time.Duration // минимальный реальный интервал между проходами
	MaxWait     time.Duration // верхняя граница ожидания, если истекающих партий нет
	OnSweep     func([]store.ExpiredBatch)

	kick chan struct{}
}

// New создаёт «дворника» с интервалом 200 мс и потолком ожидания 5 с (часы могут идти быстрее реальных).
func New(s Store, now func() time.Time, onSweep func([]store.ExpiredBatch)) *Janitor {
	return &Janitor{Store: s, Now: now, MinInterval: 200 * time.Millisecond, MaxWait: 5 * time.Second,
		OnSweep: onSweep, kick: make(chan struct{}, 1)}
}

// Kick будит «дворника» (смена часов, прыжок времени).
func (j *Janitor) Kick() {
	select {
	case j.kick <- struct{}{}:
	default:
	}
}

// Sweep делает один проход и сообщает о результате через OnSweep.
func (j *Janitor) Sweep(ctx context.Context) ([]store.ExpiredBatch, error) {
	got, err := j.Store.SweepExpired(ctx, j.Now)
	if err == nil && len(got) > 0 && j.OnSweep != nil {
		j.OnSweep(got)
	}
	return got, err
}

// Run работает до отмены ctx: проход, затем ожидание до ближайшего истечения или Kick.
func (j *Janitor) Run(ctx context.Context) {
	for {
		if _, err := j.Sweep(ctx); err != nil && ctx.Err() == nil {
			slog.Error("janitor sweep", "err", err)
		}
		wait := j.MaxWait
		if next, err := j.Store.NextExpiry(ctx, j.Now()); err == nil && next != nil {
			if d := next.Sub(j.Now()); d < wait {
				wait = d
			}
		}
		if wait < j.MinInterval {
			wait = j.MinInterval
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-j.kick:
			timer.Stop()
			// Минимальный интервал между проходами.
			select {
			case <-ctx.Done():
				return
			case <-time.After(j.MinInterval):
			}
		case <-timer.C:
		}
	}
}
