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

// RunStore хранит признак «генератор включён», чтобы он пережил рестарт API.
type RunStore interface {
	SimulatorRunning(ctx context.Context) (bool, error)
	SetSimulatorRunning(ctx context.Context, on bool) error
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
	// State — где хранится признак «включён»; nil — не сохраняется.
	State RunStore

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
// Явное значение сохраняется в State.
func (s *Simulator) SetRunning(running bool) {
	if running {
		s.start()
		s.persist(true)
	} else {
		s.Stop()
	}
}

// Restore после рестарта API включает генератор, если он был включён до остановки.
func (s *Simulator) Restore(ctx context.Context) error {
	if s.State == nil {
		return nil
	}
	on, err := s.State.SimulatorRunning(ctx)
	if err != nil {
		return err
	}
	if on {
		s.start()
	}
	return nil
}

func (s *Simulator) persist(on bool) {
	if s.State == nil {
		return
	}
	// Отмена base при остановке процесса не должна терять явную паузу.
	if err := s.State.SetSimulatorRunning(context.WithoutCancel(s.base), on); err != nil {
		s.Log.Error("simulator state save failed", slog.String("error", err.Error()))
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

// Stop — явная остановка (пауза, сброс стенда): генератор выключается и это сохраняется.
// Остановка процесса признак не меняет: горутина гаснет по отмене base.
func (s *Simulator) Stop() {
	s.halt()
	s.persist(false)
}

func (s *Simulator) halt() {
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
