package events

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// HotOffersChanged — список рекомендаций изменился (7.5, данные: algo).
const HotOffersChanged = "hotoffers.changed"

// Fingerprint — отпечаток значения: sha256 от его JSON. Нужен только для подавления лишних
// событий; на ответ чтения не влияет (2.7).
func Fingerprint(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// HotTracker помнит отпечаток списка по каждому алгоритму и публикует hotoffers.changed,
// только если отпечаток изменился. Состояние — в памяти процесса.
type HotTracker struct {
	bus *Bus

	mu   sync.Mutex
	prev map[string]string
}

// NewHotTracker создаёт трекер; первый Update по алгоритму только запоминает отпечаток.
func NewHotTracker(bus *Bus) *HotTracker { return &HotTracker{bus: bus, prev: map[string]string{}} }

// Update сообщает отпечаток списка алгоритма algo; true — событие опубликовано.
func (t *HotTracker) Update(algo, fingerprint string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	old, seen := t.prev[algo]
	t.prev[algo] = fingerprint
	if !seen || old == fingerprint {
		return false
	}
	t.bus.Publish(HotOffersChanged, map[string]any{"algo": algo})
	return true
}

// StandTick решает, пора ли пересчитать список по тику: прошло Step времени стенда.
// Опрашивать его можно хоть раз в секунду реального времени — быстрее раза в секунду
// пересчёт не идёт, как бы ни был ускорен стенд (2.7).
type StandTick struct {
	Step time.Duration

	last time.Time
	init bool
}

// Due — true, если с прошлого срабатывания прошло не меньше Step времени стенда либо время
// пошло назад (сброс стенда). Первый вызов только запоминает время и возвращает false.
func (s *StandTick) Due(now time.Time) bool {
	if !s.init {
		s.last, s.init = now, true
		return false
	}
	if now.Before(s.last) || now.Sub(s.last) >= s.Step {
		s.last = now
		return true
	}
	return false
}
