package clock

import (
	"context"
	"sync"
	"time"
)

// State — сохраняемое состояние ускоренных часов. Wall-точка в него не входит:
// после рестарта простой процесса не засчитывается.
type State struct {
	SimTime time.Time // симулированное время на момент сохранения
	Speed   float64   // множитель скорости; 0 — пауза
}

// Jump — запись журнала прыжков.
type Jump struct {
	From  time.Time
	To    time.Time
	Hours float64
}

// Store — хранилище состояния часов. Реализация на PostgreSQL — вне пакета.
type Store interface {
	// Load возвращает сохранённое состояние; ok=false, если его ещё нет.
	Load(ctx context.Context) (st State, ok bool, err error)
	// Save сохраняет состояние.
	Save(ctx context.Context, st State) error
	// RecordJump атомарно сохраняет состояние после прыжка и пишет его в журнал.
	RecordJump(ctx context.Context, j Jump, st State) error
}

// MemStore — хранилище в памяти (для тестов).
type MemStore struct {
	mu    sync.Mutex
	st    State
	has   bool
	jumps []Jump
}

func (m *MemStore) Load(context.Context) (State, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st, m.has, nil
}

func (m *MemStore) Save(_ context.Context, st State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.st, m.has = st, true
	return nil
}

func (m *MemStore) RecordJump(_ context.Context, j Jump, st State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.st, m.has = st, true
	m.jumps = append(m.jumps, j)
	return nil
}

// Jumps возвращает копию журнала прыжков.
func (m *MemStore) Jumps() []Jump {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Jump(nil), m.jumps...)
}
