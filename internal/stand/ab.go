package stand

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/domain"
	"umirhack/internal/domain/units"
	"umirhack/internal/generator"
	"umirhack/internal/seed"
	"umirhack/internal/simulator"
	"umirhack/internal/store"
)

// Профили сида (SEED_PROFILE).
const (
	ProfileDemo = "demo" // сценарий показа (Л-1, крем-суп и т. д.), по умолчанию
	ProfileAB   = "ab"   // bench «скоропорт в трёх блюдах» для живого A/B
)

// ABScenario — сценарий bench, на котором построен профиль ab (internal/simulator/scenarios.go).
const ABScenario = "perishable3"

// abNames — человеческие подписи вместо id сценария (в bench имя равно id).
var abNames = map[string]string{
	"fish": "Рыба", "cream": "Сливки", "chicken": "Курица", "rice": "Рис",
	"fish-soup": "Рыбный суп", "fish-grill": "Рыба на гриле", "fish-bowl": "Боул с рыбой",
	"creamy-chicken": "Курица в сливках", "chicken-rice": "Курица с рисом", "rice-plain": "Рис",
}

// ABWorld — склад и спрос сценария bench perishable3 с русскими названиями и ценой блюда
// (себестоимость ×3, до рубля). Данные берутся из simulator.Scenarios, не копируются.
func ABWorld() simulator.World {
	var w simulator.World
	for _, sc := range simulator.Scenarios() {
		if sc.Name == ABScenario {
			w = sc.World
		}
	}
	price := map[string]int64{}
	for i, p := range w.Products {
		if n, ok := abNames[p.ID]; ok {
			w.Products[i].Name = n
		}
		price[p.ID] = p.PriceMinorPer1000
	}
	for i, d := range w.Dishes {
		if n, ok := abNames[d.ID]; ok {
			w.Dishes[i].Name = n
		}
		var cost int64
		for _, it := range d.Recipe {
			cost += price[it.ProductID] * int64(it.Quantity) / 1000
		}
		w.Dishes[i].PriceMinor = (cost*3 + 50) / 100 * 100
	}
	return w
}

// loadAB пишет справочники профиля ab в tx (без истории); стартовая поставка — loadABStart после коммита.
func loadAB(ctx context.Context, tx pgx.Tx) error {
	w := ABWorld()
	return seed.LoadCatalog(ctx, tx, w.Products, w.Dishes)
}

// loadABStart принимает стартовую поставку профиля за секунду до t0.
func loadABStart(ctx context.Context, db *store.DB, t0 time.Time, loc *time.Location) error {
	w := ABWorld()
	at := t0.Add(-time.Second)
	for _, d := range w.Start(t0) {
		if err := receiveWorld(ctx, db, w, d, func() time.Time { return at }, loc); err != nil {
			return err
		}
	}
	return nil
}

func receiveWorld(ctx context.Context, db *store.DB, w simulator.World, d seed.Delivery, now func() time.Time, loc *time.Location) error {
	base := map[string]units.Unit{}
	for _, p := range w.Products {
		base[p.ID] = units.Unit(p.Unit)
	}
	in := store.DeliveryInput{ID: d.ID, Number: d.Number, CreatedAt: d.CreatedAt}
	for _, it := range d.Items {
		q, err := units.Parse(it.Quantity, it.Unit, base[it.ProductID])
		if err != nil {
			return err
		}
		in.Items = append(in.Items, store.DeliveryItem{ProductID: it.ProductID, Name: it.Name,
			Unit: domain.Unit(base[it.ProductID]), Qty: int64(q), ShelfLifeHours: it.ShelfLifeHours})
	}
	_, err := db.ReceiveDelivery(ctx, in, now, loc)
	return err
}

// DeliverySink принимает поставку генератора профиля ab (на демо поставки вносят вручную).
type DeliverySink interface {
	ReceiveDelivery(ctx context.Context, d generator.Delivery) error
}

// ReceiveDelivery реализует DeliverySink: поставка профиля ab принимается под затвором на чтение.
func (s *Stand) ReceiveDelivery(ctx context.Context, d generator.Delivery) error {
	s.Gate.RLock()
	err := receiveWorld(ctx, s.DB, ABWorld(), d, s.Now, s.Loc)
	s.Gate.RUnlock()
	if err == nil && s.AfterChange != nil {
		s.AfterChange(ctx)
	}
	return err
}

// SeedProfile реализует api.ProfileReporter.
func (s *Stand) SeedProfile() string {
	if s.Profile == ProfileAB {
		return ProfileAB
	}
	return ProfileDemo
}

// UseProfile настраивает генератор под профиль стенда; для demo ничего не меняет.
func (s *Stand) UseProfile(profile string) {
	s.Profile = profile
	if profile != ProfileAB || s.Sim == nil {
		return
	}
	src := &abSource{world: ABWorld(), anchor: func() time.Time { return s.abAnchor() }}
	s.Sim.Events, s.Sim.Deliveries = src.events, s
}

func (s *Stand) abAnchor() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.anchor.IsZero() {
		s.anchor = s.restoreABAnchor()
	}
	return s.anchor
}

// restoreABAnchor — T0 сброса после перезапуска процесса. Стартовые поставки профиля ab принимаются
// за секунду до T0 (loadABStart), поэтому T0 = самая ранняя приёмка партии + 1 с. Без партий (БД пуста
// или недоступна) — T0 текущих суток стенда, как при сбросе. Иначе генератор заново проиграл бы прошедшие
// дни с теми же id заказов и поставок, и точка осталась бы без продаж и подвоза.
func (s *Stand) restoreABAnchor() time.Time {
	if s.DB != nil {
		var first *time.Time
		err := s.DB.Pool.QueryRow(context.Background(), `SELECT min(received_at) FROM batches`).Scan(&first)
		if err == nil && first != nil {
			return first.Add(time.Second).UTC()
		}
	}
	return T0(s.Now(), s.Loc)
}

// abSource — поток событий сценария bench, привязанный к T0 сброса. Спрос подъёмом веса не
// накручивается: при weights заказные позиции переразыгрываются с весами p_i·w_i (модель M2).
type abSource struct {
	world  simulator.World
	anchor func() time.Time

	mu     sync.Mutex
	t0     time.Time
	seed   int64
	days   int
	cached []generator.Event
}

func (a *abSource) events(seedValue int64, from, to time.Time, weights map[string]float64) ([]generator.Event, error) {
	t0 := a.anchor()
	need := int(to.Sub(t0)/(24*time.Hour)) + 2
	a.mu.Lock()
	if !a.t0.Equal(t0) || a.seed != seedValue || a.days < need {
		days := 30
		for days < need*2 {
			days *= 2
		}
		evs, err := a.world.Events(seedValue, t0, days)
		if err != nil {
			a.mu.Unlock()
			return nil, err
		}
		a.t0, a.seed, a.days, a.cached = t0, seedValue, days, evs
	}
	cached := a.cached
	a.mu.Unlock()

	lo := sort.Search(len(cached), func(i int) bool { return !cached[i].At.Before(from) })
	var out []generator.Event
	for _, e := range cached[lo:] {
		if !e.At.Before(to) {
			break
		}
		if e.Kind == generator.KindOrder && e.Order != nil {
			o := *e.Order
			if len(weights) > 0 {
				o = reweigh(o, a.world.Dishes, weights)
			}
			o.ID = orderUUID(o.ID) // POST /api/orders ждёт UUID, id сценария — строка
			e.Order = &o
		}
		out = append(out, e)
	}
	return out, nil
}

// reweigh переразыгрывает блюда позиций заказа с весами prior·множитель; от ID заказа
// результат детерминирован, число гостей и порций не меняется.
func reweigh(o generator.Order, dishes []seed.Dish, weights map[string]float64) generator.Order {
	h := fnv.New64a()
	_, _ = h.Write([]byte(o.ID))
	x := h.Sum64()
	next := func() float64 { // splitmix64
		x += 0x9e3779b97f4a7c15
		z := x
		z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
		z = (z ^ (z >> 27)) * 0x94d049bb133111eb
		return float64((z^(z>>31))>>11) / (1 << 53)
	}
	w := make([]float64, len(dishes))
	var total float64
	for i, d := range dishes {
		w[i] = d.PriorDaily
		if m, ok := weights[d.ID]; ok && m > 0 {
			w[i] *= m
		}
		total += w[i]
	}
	items := make([]generator.OrderItem, 0, len(o.Items))
	idx := map[string]int{}
	for _, it := range o.Items {
		v := next() * total
		pick := dishes[len(dishes)-1].ID
		for i, d := range dishes {
			if v < w[i] {
				pick = d.ID
				break
			}
			v -= w[i]
		}
		if j, dup := idx[pick]; dup {
			items[j].Portions += it.Portions
			continue
		}
		idx[pick] = len(items)
		items = append(items, generator.OrderItem{DishID: pick, Portions: it.Portions})
	}
	o.Items = items
	return o
}

// orderUUID — детерминированный UUID вида v4 от id заказа сценария.
func orderUUID(id string) string {
	h := sha256.Sum256([]byte(id))
	h[6] = h[6]&0x0f | 0x40
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
