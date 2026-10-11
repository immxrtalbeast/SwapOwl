package main

import (
	"context"
	"log/slog"
	"time"

	"umirhack/internal/api"
	"umirhack/internal/events"
	"umirhack/internal/hotoffers"
)

// Тик пересчёта списка: 60 с времени стенда, но опрос не чаще раза в секунду реального (2.7).
const (
	hotTickStep = 60 * time.Second
	hotPollReal = time.Second
)

// hotWatcher шлёт hotoffers.changed: по событиям склада и по тику, только если изменился
// отпечаток списка. Следит за оба алгоритма, чтобы переключатель на экране тоже обновлялся.
type hotWatcher struct {
	bus  *events.Bus
	src  api.HotOffersSource
	cfg  hotoffers.Config
	now  func() time.Time
	log  *slog.Logger
	trk  *events.HotTracker
	tick events.StandTick
}

func newHotWatcher(bus *events.Bus, src api.HotOffersSource, cfg hotoffers.Config, now func() time.Time, log *slog.Logger) *hotWatcher {
	return &hotWatcher{bus: bus, src: src, cfg: cfg, now: now, log: log,
		trk: events.NewHotTracker(bus), tick: events.StandTick{Step: hotTickStep}}
}

// check пересчитывает списки обоих алгоритмов и сверяет отпечатки.
func (w *hotWatcher) check(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	snap, err := w.src.Snapshot(ctx, w.now().UTC())
	if err != nil {
		w.log.Warn("events: hot-offers snapshot", slog.String("error", err.Error()))
		return
	}
	for _, algo := range []string{"a", "b"} {
		fp, err := events.Fingerprint(api.BuildHotOffers(snap, algo, w.cfg))
		if err != nil {
			w.log.Warn("events: hot-offers fingerprint", slog.String("error", err.Error()))
			continue
		}
		w.trk.Update(algo, fp)
	}
}

// triggers — события, после которых список мог измениться.
func hotTrigger(typ string) bool {
	switch typ {
	case events.StockChanged, events.StoplistChanged, events.ClockChanged, events.StandReset:
		return true
	}
	return false
}

// run работает до остановки ctx. Шина закрывает медленного подписчика — подписываемся заново
// и сверяем список, так как события могли потеряться.
func (w *hotWatcher) run(ctx context.Context) {
	w.check(ctx) // запоминаем отпечатки на старте
	poll := time.NewTicker(hotPollReal)
	defer poll.Stop()
	for ctx.Err() == nil {
		sub := w.bus.Subscribe()
		w.loop(ctx, sub, poll)
		sub.Unsubscribe()
		select {
		case <-ctx.Done():
			return
		case <-time.After(hotPollReal):
		}
		w.check(ctx)
	}
}

func (w *hotWatcher) loop(ctx context.Context, sub *events.Subscription, poll *time.Ticker) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			if hotTrigger(ev.Type) {
				w.check(ctx)
			}
		case <-poll.C:
			if w.tick.Due(w.now()) {
				w.check(ctx)
			}
		}
	}
}
