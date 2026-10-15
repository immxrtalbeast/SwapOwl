// Package audit — журнал действий в БД (спецификация, 2.9, 8.3 п. 6). Вызов Record общий:
// его используют и другие тикеты (корректировки, подтверждение сроков). Пакет не знает о HTTP и БД.
package audit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Entry — запись журнала. Details не должны содержать паролей, токенов и ключей.
type Entry struct {
	ID      int64          `json:"id"`
	At      time.Time      `json:"at"`
	Actor   string         `json:"actor"`
	Action  string         `json:"action"`
	Details map[string]any `json:"details"`
}

// Sink — хранилище записей; реализует internal/store.
type Sink interface {
	InsertAudit(ctx context.Context, e Entry) error
	// ListAudit возвращает последние записи, новые сверху.
	ListAudit(ctx context.Context, limit int) ([]Entry, error)
}

// Record пишет запись в журнал. Нулевое At — текущее время; nil Details — пустой объект.
// nil sink — журнал не настроен, запись пропускается.
func Record(ctx context.Context, sink Sink, actor, action string, details map[string]any) error {
	if sink == nil {
		return nil
	}
	if actor == "" || action == "" {
		return fmt.Errorf("audit: actor and action are required")
	}
	if details == nil {
		details = map[string]any{}
	}
	e := Entry{At: time.Now().UTC(), Actor: actor, Action: action, Details: details}
	// Запись не должна пропасть из-за отмены запроса (разрыв соединения клиентом).
	return sink.InsertAudit(context.WithoutCancel(ctx), e)
}

// Mem — журнал в памяти для тестов.
type Mem struct {
	mu      sync.Mutex
	entries []Entry
}

var _ Sink = (*Mem)(nil)

// InsertAudit добавляет запись.
func (m *Mem) InsertAudit(_ context.Context, e Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e.ID = int64(len(m.entries) + 1)
	m.entries = append(m.entries, e)
	return nil
}

// ListAudit возвращает до limit последних записей, новые сверху.
func (m *Mem) ListAudit(_ context.Context, limit int) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Entry{}
	for i := len(m.entries) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, m.entries[i])
	}
	return out, nil
}
