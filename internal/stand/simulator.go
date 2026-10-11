package stand

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"umirhack/internal/generator"
	"umirhack/internal/seed"
)

// creamDishes — блюда со сливками. Демо-профиль их не заказывает: сливки С-1 нужны сценарию
// для шага 5 (.ai/demo.md), и после шага 4 их остаётся на S = 2 порции крем-супа.
func creamDishes() map[string]bool {
	out := map[string]bool{}
	for _, d := range seed.Dishes() {
		for _, it := range d.Recipe {
			if it.ProductID == "cream" {
				out[d.ID] = true
			}
		}
	}
	return out
}

// demoProfile убирает из заказа позиции со сливками; пустой заказ отбрасывается.
func demoProfile(o generator.Order, skip map[string]bool) (generator.Order, bool) {
	items := make([]generator.OrderItem, 0, len(o.Items))
	for _, it := range o.Items {
		if !skip[it.DishID] {
			items = append(items, it)
		}
	}
	o.Items = items
	return o, len(items) > 0
}

// OrderSink принимает заказ генератора — теми же сервисами, что и POST /api/orders.
type OrderSink interface {
	PlaceOrder(ctx context.Context, o generator.Order) error
}

// MaxCatchUp — сколько времени стенда генератор отрабатывает за один шаг: после прыжка
// вперёд он не заливает заказами весь пропущенный интервал, а берёт только хвост.
const MaxCatchUp = 2 * time.Hour

// Simulator гонит события пакета generator в процессе API по времени стенда (2.8).
// Поставки генератор не подаёт: их на стенде вносят вручную.
type Simulator struct {
	Sink OrderSink
	Now  func() time.Time
	Seed int64
	// Tick — период шагов в реальном времени; 0 — 250 мс.
	Tick time.Duration
	Log  *slog.Logger

	base context.Context
	mu   sync.Mutex
	stop context.CancelFunc
	done chan struct{}
}

// NewSimulator создаёт остановленный симулятор; base ограничивает жизнь его горутины.
func NewSimulator(base context.Context, sink OrderSink, now func() time.Time, seed int64, log *slog.Logger) *Simulator {
	if log == nil {
		log = slog.Default()
	}
	return &Simulator{Sink: sink, Now: now, Seed: seed, Log: log, base: base}
}

// Running — идёт ли генератор.
func (s *Simulator) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stop != nil
}

// SetRunning запускает или останавливает генератор; повторный вызов с тем же значением — без эффекта.
func (s *Simulator) SetRunning(running bool) {
	if running {
		s.start()
	} else {
		s.Stop()
	}
}

func (s *Simulator) start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop != nil {
		return
	}
	ctx, cancel := context.WithCancel(s.base)
	s.stop, s.done = cancel, make(chan struct{})
	go s.run(ctx, s.done, s.Now())
}

// Stop останавливает генератор и ждёт завершения текущего шага.
func (s *Simulator) Stop() {
	s.mu.Lock()
	cancel, done := s.stop, s.done
	s.stop, s.done = nil, nil
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

func (s *Simulator) run(ctx context.Context, done chan struct{}, from time.Time) {
	defer close(done)
	tick := s.Tick
	if tick <= 0 {
		tick = 250 * time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	skip := creamDishes()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		to := s.Now()
		if !to.After(from) { // пауза часов или время ушло назад (сброс)
			from = to
			continue
		}
		if to.Sub(from) > MaxCatchUp {
			from = to.Add(-MaxCatchUp)
		}
		evs, err := generator.Generate(s.Seed, from, to)
		if err != nil {
			s.Log.Error("simulator generate failed", slog.String("error", err.Error()))
			return
		}
		from = to
		for _, e := range evs {
			if ctx.Err() != nil {
				return
			}
			if e.Kind != generator.KindOrder || e.Order == nil {
				continue
			}
			o, ok := demoProfile(*e.Order, skip)
			if !ok {
				continue
			}
			if err := s.Sink.PlaceOrder(ctx, o); err != nil && ctx.Err() == nil {
				s.Log.Error("simulator order failed", slog.String("order_id", o.ID), slog.String("error", err.Error()))
			}
		}
	}
}
