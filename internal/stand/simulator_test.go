package stand

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"umirhack/internal/generator"
)

type recSink struct {
	mu  sync.Mutex
	ids []string
}

func (r *recSink) PlaceOrder(_ context.Context, o generator.Order) error {
	r.mu.Lock()
	r.ids = append(r.ids, o.ID)
	r.mu.Unlock()
	return nil
}
func (r *recSink) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.ids) }

func newSim(sink OrderSink, now func() time.Time) *Simulator {
	s := NewSimulator(context.Background(), sink, now, 1, nil)
	s.Tick = 2 * time.Millisecond
	return s
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if cond() {
			return
		}
		time.Sleep(4 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

// Время стенда сдвигается на сутки вперёд шагами по 30 минут: заказы дня доходят до приёмника.
func TestSimulatorPlacesOrdersAsTimeAdvances(t *testing.T) {
	base := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC) // 11:00 по Москве
	var off atomic.Int64
	now := func() time.Time { return base.Add(time.Duration(off.Load())) }
	sink := &recSink{}
	s := newSim(sink, now)
	s.SetRunning(true)
	defer s.Stop()
	if !s.Running() {
		t.Fatal("must run")
	}
	for i := 0; i < 48; i++ {
		off.Add(int64(30 * time.Minute))
		time.Sleep(8 * time.Millisecond)
	}
	waitFor(t, func() bool { return sink.count() > 0 })
}

func TestSimulatorStopHaltsOrders(t *testing.T) {
	base := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	var off atomic.Int64
	sink := &recSink{}
	s := newSim(sink, func() time.Time { return base.Add(time.Duration(off.Load())) })
	s.SetRunning(true)
	s.SetRunning(true) // повторный запуск — без второй горутины
	s.SetRunning(false)
	if s.Running() {
		t.Fatal("must stop")
	}
	off.Add(int64(20 * time.Hour))
	time.Sleep(30 * time.Millisecond)
	if sink.count() != 0 {
		t.Fatalf("stopped simulator placed %d orders", sink.count())
	}
}

func TestStandSimulatorRunningFlag(t *testing.T) {
	st := &Stand{}
	if st.SimulatorRunning() {
		t.Fatal("no simulator")
	}
	st.Sim = newSim(&recSink{}, time.Now)
	st.Sim.SetRunning(true)
	defer st.Sim.Stop()
	if !st.SimulatorRunning() {
		t.Fatal("running expected")
	}
}

// Демо-профиль не заказывает блюда со сливками, остальные позиции заказа сохраняются.
func TestDemoProfileDropsCreamDishes(t *testing.T) {
	skip := creamDishes()
	if !skip["salmon-pasta"] || !skip["cream-soup"] || skip["salmon-steak"] || skip["caesar"] {
		t.Fatalf("блюда со сливками: %v", skip)
	}
	o, ok := demoProfile(generator.Order{ID: "x", Items: []generator.OrderItem{{DishID: "salmon-pasta", Portions: 1}, {DishID: "caesar", Portions: 2}}}, skip)
	if !ok || len(o.Items) != 1 || o.Items[0].DishID != "caesar" {
		t.Fatalf("заказ после профиля: %+v ok=%v", o, ok)
	}
	if _, ok := demoProfile(generator.Order{Items: []generator.OrderItem{{DishID: "cream-soup", Portions: 1}}}, skip); ok {
		t.Fatal("заказ только со сливками должен отбрасываться")
	}
}

// Симулятор за сутки времени стенда не отправляет в приёмник ни одного блюда со сливками.
func TestSimulatorNeverOrdersCreamDishes(t *testing.T) {
	base := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	var off atomic.Int64
	now := func() time.Time { return base.Add(time.Duration(off.Load())) }
	sink := &dishSink{}
	s := newSim(sink, now)
	s.SetRunning(true)
	defer s.Stop()
	for i := 0; i < 48; i++ {
		off.Add(int64(30 * time.Minute))
		time.Sleep(8 * time.Millisecond)
	}
	waitFor(t, func() bool { return sink.count() > 0 })
	s.Stop()
	for _, d := range sink.dishes() {
		if d == "salmon-pasta" || d == "cream-soup" {
			t.Fatalf("симулятор заказал блюдо со сливками: %s", d)
		}
	}
}

type dishSink struct {
	mu sync.Mutex
	ds []string
}

func (r *dishSink) PlaceOrder(_ context.Context, o generator.Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, it := range o.Items {
		r.ds = append(r.ds, it.DishID)
	}
	return nil
}
func (r *dishSink) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.ds) }
func (r *dishSink) dishes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ds...)
}
