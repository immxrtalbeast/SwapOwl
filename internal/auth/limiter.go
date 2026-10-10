package auth

import (
	"sync"
	"time"
)

// Limiter считает неудачные входы по логину в скользящем окне. Живёт в памяти процесса
// (2.9: для одного экземпляра достаточно).
type Limiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu    sync.Mutex
	fails map[string][]time.Time
}

// NewLimiter: после max неудач за window вход по логину блокируется.
func NewLimiter(max int, window time.Duration, now func() time.Time) *Limiter {
	return &Limiter{max: max, window: window, now: now, fails: map[string][]time.Time{}}
}

// prune убирает устаревшие отметки; вызывать под mu.
func (l *Limiter) prune(login string, now time.Time) []time.Time {
	ts := l.fails[login]
	i := 0
	for i < len(ts) && now.Sub(ts[i]) >= l.window {
		i++
	}
	ts = ts[i:]
	if len(ts) == 0 {
		delete(l.fails, login)
		return nil
	}
	l.fails[login] = ts
	return ts
}

// Blocked — уже было max неудач за окно; следующая попытка получает 429.
func (l *Limiter) Blocked(login string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(login, l.now())) >= l.max
}

// Fail записывает неудачную попытку.
func (l *Limiter) Fail(login string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.fails) > 1024 { // чужие логины не копятся бесконечно
		for k := range l.fails {
			l.prune(k, now)
		}
	}
	l.fails[login] = append(l.prune(login, now), now)
}

// Reset сбрасывает счётчик после успешного входа.
func (l *Limiter) Reset(login string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, login)
}
