package stand

import (
	"context"
	"testing"
	"time"

	"umirhack/internal/api"
	"umirhack/internal/domain"
	"umirhack/internal/domain/units"
	"umirhack/internal/generator"
	"umirhack/internal/hotoffers"
	"umirhack/internal/hotoffers/b"
	"umirhack/internal/seed"
	"umirhack/internal/store"
)

func TestHotWeights(t *testing.T) {
	w := HotWeights([]string{"x", "y", "z", "x"}, 1.5)
	if len(w) != 3 || w["x"] != 1.5 || w["y"] != 1.25 || w["z"] != 1+0.5/3 {
		t.Fatalf("u_i = 1 + (u-1)/i: %v", w)
	}
	if len(HotWeights(nil, 1.5)) != 0 {
		t.Fatal("пустой список — без весов")
	}
}

type snapHot struct{ s *Stand }

// HotDishes — тот же расчёт, что у GET /api/hot-offers (b — основной алгоритм), без HTTP.
func (h snapHot) HotDishes(ctx context.Context, now time.Time) ([]string, error) {
	snap, err := h.s.DB.Snapshot(ctx, now, store.SnapshotParams{WindowDays: 14, PriorDays: 3,
		Schedule: domain.Schedule{Location: h.s.Loc, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour}})
	if err != nil {
		return nil, err
	}
	offers, _ := b.Rank(hotoffers.Input{Now: snap.Now, Batches: snap.Batches, Dishes: snap.Dishes,
		Stopped: snap.Stopped, Price: snap.Price, RateHour: snap.RateHour, Schedule: snap.Schedule,
		Config: hotoffers.Config{TopN: 3, WMin: 0.5, CoverHours: 12, MaxIter: 200, HorizonHours: 24}})
	out := make([]string, 0, len(offers))
	for _, o := range offers {
		out = append(out, o.DishID)
	}
	return out, nil
}

type placerSink struct {
	p     api.OrderPlacer
	count int
}

func (k *placerSink) PlaceOrder(ctx context.Context, o generator.Order) error {
	items := make([]store.OrderItem, 0, len(o.Items))
	for _, it := range o.Items {
		items = append(items, store.OrderItem{DishID: it.DishID, Portions: it.Portions})
	}
	k.count++
	_, err := k.p.Place(ctx, o.ID, store.SourceAPI, items)
	return err
}

func receive(t *testing.T, s *Stand, d *generator.Delivery) {
	t.Helper()
	base := map[string]units.Unit{}
	for _, p := range seed.Products() {
		base[p.ID] = units.Unit(p.Unit)
	}
	in := store.DeliveryInput{ID: d.ID, Number: d.Number, CreatedAt: d.CreatedAt}
	for _, it := range d.Items {
		q, err := units.Parse(it.Quantity, it.Unit, base[it.ProductID])
		if err != nil {
			t.Fatal(err)
		}
		in.Items = append(in.Items, store.DeliveryItem{ProductID: it.ProductID, Name: it.Name,
			Unit: domain.Unit(base[it.ProductID]), Qty: int64(q), ShelfLifeHours: it.ShelfLifeHours})
	}
	if _, err := s.DB.ReceiveDelivery(context.Background(), in, s.Now, s.Loc); err != nil {
		t.Fatal(err)
	}
}

// runWeek гонит стенд 7 суток (168 часовых шагов ≈ ×600 за 17 минут): поставки генератора вносятся
// как «вручную», заказы идут через Simulator.step, «дворник» — через Stand.Jump.
// Возвращает списания за неделю в копейках и число заказов.
func runWeek(t *testing.T, follow bool, seedValue int64) (int64, int) {
	t.Helper()
	s, db := newStand(t)
	ctx := context.Background()
	sink := &placerSink{p: api.OrderPlacer{Orders: db, Now: s.Now}}
	sim := NewSimulator(ctx, sink, s.Now, seedValue, nil)
	sim.Hot, sim.FollowHot = snapHot{s}, follow
	skip := creamDishes()
	start := s.Now()
	for i := 0; i < 7*24; i++ {
		from := s.Now()
		if _, err := s.Jump(ctx, 1); err != nil {
			t.Fatal(err)
		}
		to := s.Now()
		evs, err := generator.Generate(seedValue, from, to)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range evs {
			if e.Kind == generator.KindDelivery {
				receive(t, s, e.Delivery)
			}
		}
		if err := sim.step(ctx, skip, from, to); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := db.WriteoffsReport(ctx, start, s.Now().Add(time.Hour), s.Loc)
	if err != nil {
		t.Fatal(err)
	}
	return rep.TotalMinor, sink.count
}

// Демо «со SwapOwl и без»: один seed и один генератор, гости слушаются списка (true) или нет (false).
// FEFO и инварианты те же; у true списания в ₽ меньше, при одинаковом флаге — совпадают до копейки.
func TestFollowHotReducesWriteoffsSameSeed(t *testing.T) {
	if testing.Short() {
		t.Skip("неделя времени стенда")
	}
	const seedValue = 2026
	withHot, ordersHot := runWeek(t, true, seedValue)
	withoutHot, ordersOff := runWeek(t, false, seedValue)
	t.Logf("списания за 7 суток: слушаются %d коп., не слушаются %d коп.; заказов %d и %d",
		withHot, withoutHot, ordersHot, ordersOff)
	if ordersHot == 0 || withoutHot == 0 {
		t.Fatalf("прогон пуст: заказов %d, списаний без списка %d", ordersHot, withoutHot)
	}
	if withHot >= withoutHot {
		t.Fatalf("списания при follow_hot=true (%d) должны быть меньше, чем при false (%d)", withHot, withoutHot)
	}
	again, _ := runWeek(t, true, seedValue)
	if again != withHot {
		t.Fatalf("тот же seed и флаг дали разные списания: %d и %d", withHot, again)
	}
}
