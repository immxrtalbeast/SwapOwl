package clock

import (
	"sync"
	"time"
)

// Clock — источник времени стенда.
type Clock interface {
	Now() time.Time
}

// Real — реальные часы.
type Real struct{}

// Now возвращает текущее время в UTC.
func (Real) Now() time.Time { return time.Now().UTC() }

// Fixed — фиксированные часы для тестов с ручным сдвигом.
type Fixed struct {
	mu sync.RWMutex
	t  time.Time
}

// NewFixed создаёт часы, стоящие на моменте t.
func NewFixed(t time.Time) *Fixed { return &Fixed{t: t.UTC()} }

// Now возвращает текущий момент часов.
func (f *Fixed) Now() time.Time {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.t
}

// Set ставит часы на момент t.
func (f *Fixed) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = t.UTC()
}

// Advance сдвигает часы на d.
func (f *Fixed) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}
