package events

import (
	"sort"
	"sync"
)

// StopTracker помнит состав стоп-листа и публикует разность до и после (stoplist.changed).
type StopTracker struct {
	bus *Bus

	mu   sync.Mutex
	prev map[string]struct{}
	init bool
}

// NewStopTracker создаёт трекер; первый вызов Update только запоминает состояние.
func NewStopTracker(bus *Bus) *StopTracker { return &StopTracker{bus: bus} }

// Update принимает текущий состав стоп-листа (id блюд) и публикует событие, если он изменился.
func (t *StopTracker) Update(current []string) {
	cur := make(map[string]struct{}, len(current))
	for _, id := range current {
		cur[id] = struct{}{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.init {
		t.prev, t.init = cur, true
		return
	}
	added, removed := []string{}, []string{}
	for id := range cur {
		if _, ok := t.prev[id]; !ok {
			added = append(added, id)
		}
	}
	for id := range t.prev {
		if _, ok := cur[id]; !ok {
			removed = append(removed, id)
		}
	}
	t.prev = cur
	if len(added) == 0 && len(removed) == 0 {
		return
	}
	sort.Strings(added)
	sort.Strings(removed)
	t.bus.Publish(StoplistChanged, map[string]any{"added": added, "removed": removed})
}
