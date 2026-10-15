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
// для шага 5 (docs/demo-script.md), и после шага 4 их остаётся на S = 2 порции крем-супа.
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

// HotSource отдаёт блюда текущего списка горячих предложений по порядку (позиция 1 — первая).
// Расчёт тот же, что у GET /api/hot-offers, но без HTTP.
type HotSource interface {
	HotDishes(ctx context.Context, now time.Time) ([]string, error)
}

// DefaultHotUplift — подъём веса блюда на первой позиции списка (основная точка bench, M2, 2.7).
// Величина выдумана, как и в bench: утверждается только направление эффекта.
const DefaultHotUplift = 1.5

// HotWeights — множители веса блюд по списку: u_i = 1 + (u−1)/i для позиции i = 1…k.
func HotWeights(dishes []string, uplift float64) map[string]float64 {
	w := make(map[string]float64, len(dishes))
	for i, id := range dishes {
		if _, dup := w[id]; !dup {
			w[id] = 1 + (uplift-1)/float64(i+1)
		}
	}
	return w
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
	// Hot и FollowHot — «слушаются» ли гости горячих предложений (модель M2): число гостей то же,
	// блюдо из списка выбирается чаще. FollowHot=false или Hot=nil — как без списка.
	// FEFO и инварианты склада от флага не зависят: заказы идут тем же путём.
	Hot       HotSource
	FollowHot bool
	Uplift    float64 // 0 — DefaultHotUplift
	// Events — источник событий вместо generator.GenerateWeighted (профиль ab); nil — демо-генератор.
	// Deliveries принимает поставки из этого источника; nil — поставки игнорируются (на демо их вносят вручную).
	Events     func(seed int64, from, to time.Time, weights map[string]float64) ([]generator.Event, error)
	Deliveries DeliverySink

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

// Following — слушаются ли гости горячих предложений.
func (s *Simulator) Following() bool { return s.FollowHot && s.Hot != nil }

// step отрабатывает интервал [from, to) времени стенда: события генератора идут в Sink.
func (s *Simulator) step(ctx context.Context, skip map[string]bool, from, to time.Time) error {
	var weights map[string]float64
	if s.Following() {
		dishes, err := s.Hot.HotDishes(ctx, to)
		if err != nil { // список недоступен — шаг идёт без подъёма, как при FollowHot=false
			s.Log.Warn("simulator hot offers failed", slog.String("error", err.Error()))
		} else {
			u := s.Uplift
			if u <= 0 {
				u = DefaultHotUplift
			}
			weights = HotWeights(dishes, u)
		}
	}
	gen := generator.GenerateWeighted
	if s.Events != nil {
		gen = s.Events
	}
	evs, err := gen(s.Seed, from, to, weights)
	if err != nil {
		return err
	}
	for _, e := range evs {
		if ctx.Err() != nil {
			return nil
		}
		if e.Kind == generator.KindDelivery && e.Delivery != nil && s.Deliveries != nil {
			if err := s.Deliveries.ReceiveDelivery(ctx, *e.Delivery); err != nil && ctx.Err() == nil {
				s.Log.Error("simulator delivery failed", slog.String("delivery_id", e.Delivery.ID), slog.String("error", err.Error()))
			}
			continue
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
	return nil
}

func (s *Simulator) run(ctx context.Context, done chan struct{}, from time.Time) {
	defer close(done)
	tick := s.Tick
	if tick <= 0 {
		tick = 250 * time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	var skip map[string]bool // сливки не трогаем только в демо-сценарии
	if s.Events == nil {
		skip = creamDishes()
	}
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
		start := from
		from = to
		if err := s.step(ctx, skip, start, to); err != nil {
			s.Log.Error("simulator generate failed", slog.String("error", err.Error()))
			return
		}
	}
}
