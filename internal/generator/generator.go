package generator

import (
	"fmt"
	"math"
	"sort"
	"time"

	"umirhack/internal/domain"
	seed "umirhack/internal/seed/catalog"
)

// Delivery — поставка (см. seed.Delivery).
type Delivery = seed.Delivery

// OrderItem — позиция заказа.
type OrderItem struct {
	DishID   string
	Portions int
}

// Order — заказ с временем.
type Order struct {
	ID    string
	At    time.Time
	Items []OrderItem
}

// Kind — вид события.
type Kind string

// Виды событий.
const (
	KindDelivery Kind = "delivery"
	KindOrder    Kind = "order"
)

// Event — событие генератора: ровно одно из Delivery и Order задано.
type Event struct {
	At       time.Time
	Kind     Kind
	Delivery *Delivery
	Order    *Order
}

const deliveryHour = 8 * time.Hour // поставки приходят в 08:00 по времени ресторана

// Generate выдаёт события с At в [from, to), отсортированные по времени.
// Результат зависит только от seed, from, to и справочников пакета seed.
// Каждый календарный день ресторана генерируется независимо от остальных,
// поэтому соседние интервалы склеиваются без расхождений.
func Generate(seedValue int64, from, to time.Time) ([]Event, error) {
	return GenerateWeighted(seedValue, from, to, nil)
}

// GenerateWeighted — Generate с множителями веса блюд при выборе (модель M2, 2.7): число заказов,
// позиций и порций от весов не зависит, меняется только то, какое блюдо выбрано. Поток случайных
// чисел расходуется так же, как без весов, поэтому при пустых весах результат совпадает с Generate.
// Блюда без записи в weights имеют множитель 1.
func GenerateWeighted(seedValue int64, from, to time.Time, weights map[string]float64) ([]Event, error) {
	hours := seed.WorkingHours()
	loc, err := time.LoadLocation(hours.TZ)
	if err != nil {
		return nil, fmt.Errorf("generator: часовой пояс %q: %w", hours.TZ, err)
	}
	if !from.Before(to) {
		return nil, nil
	}
	lf := from.In(loc)
	day := time.Date(lf.Year(), lf.Month(), lf.Day(), 0, 0, 0, 0, loc)
	var out []Event
	for ; day.Before(to); day = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc) {
		for _, e := range generateDay(seedValue, day, hours, weights) {
			if !e.At.Before(from) && e.At.Before(to) {
				out = append(out, e)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

func dayIndex(day time.Time) int64 {
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC).Unix() / 86400
}

// at возвращает момент суток day + off в часовом поясе day, округлённый до секунды.
func at(day time.Time, off time.Duration) time.Time {
	s := int(off / time.Second)
	return time.Date(day.Year(), day.Month(), day.Day(), s/3600, s/60%60, s%60, 0, day.Location())
}

func generateDay(seedValue int64, day time.Time, hours seed.Hours, weights map[string]float64) []Event {
	idx := dayIndex(day)
	r := newRNG(mix(seedValue, idx))
	var out []Event

	// Поставка: скоропортящиеся — каждый день, остальные — примерно через день.
	need := seed.DailyNeed()
	d := Delivery{
		ID:        fmt.Sprintf("D-%d-%d", seedValue, idx),
		Number:    fmt.Sprintf("%d", idx),
		CreatedAt: at(day, deliveryHour-time.Hour),
	}
	for _, p := range seed.Products() {
		if p.ShelfLifeHours > 100 && r.intn(2) == 0 {
			continue
		}
		cover := 1 + r.float()*2 // от 1 до 3 суток спроса
		qty := seed.RoundToPack(domain.Qty(math.Ceil(need[p.ID]*cover)), p.PackSize)
		if qty <= 0 {
			qty = p.PackSize
		}
		q, u := seed.FormatQty(qty, p.Unit)
		d.Items = append(d.Items, seed.DeliveryItem{ProductID: p.ID, Name: p.Name, Quantity: q, Unit: u, ShelfLifeHours: p.ShelfLifeHours})
	}
	if len(d.Items) > 0 {
		out = append(out, Event{At: at(day, deliveryHour), Kind: KindDelivery, Delivery: &d})
	}

	// Заказы: только в [OpenAt, CloseAt), пики на обед и ужин.
	dishes := seed.Dishes()
	total := 0.0
	for _, ds := range dishes {
		total += ds.PriorDaily
	}
	const avgPortions = 3.0 // среднее число порций в заказе
	n := int(math.Round(total / avgPortions * (0.8 + 0.4*r.float())))
	weighted := make([]float64, len(dishes)) // вес выбора блюда; сумма не влияет на число заказов
	wTotal := 0.0
	for i, ds := range dishes {
		w := ds.PriorDaily
		if m, ok := weights[ds.ID]; ok && m > 0 {
			w *= m
		}
		weighted[i] = w
		wTotal += w
	}
	for i := 0; i < n; i++ {
		o := Order{ID: uuid(r), At: at(day, orderOffset(r, hours))}
		used := map[string]bool{}
		for k, lines := 0, 1+r.intn(2); k < lines; k++ {
			ds := pickDish(r, dishes, weighted, wTotal)
			portions := 1 + r.intn(2) // всегда тратится, чтобы повтор блюда не сдвигал поток случайных чисел
			if used[ds.ID] {
				continue
			}
			used[ds.ID] = true
			o.Items = append(o.Items, OrderItem{DishID: ds.ID, Portions: portions})
		}
		out = append(out, Event{At: o.At, Kind: KindOrder, Order: &o})
	}
	return out
}

// orderOffset — смещение времени заказа от начала суток внутри часов работы.
func orderOffset(r *rng, h seed.Hours) time.Duration {
	var lo, hi time.Duration
	switch x := r.float(); {
	case x < 0.4:
		lo, hi = 12*time.Hour, 15*time.Hour
	case x < 0.8:
		lo, hi = 18*time.Hour, 21*time.Hour
	default:
		lo, hi = h.OpenAt, h.CloseAt
	}
	if lo < h.OpenAt {
		lo = h.OpenAt
	}
	if hi > h.CloseAt {
		hi = h.CloseAt
	}
	secs := int((hi - lo) / time.Second)
	if secs <= 0 {
		return h.OpenAt
	}
	return lo + time.Duration(r.intn(secs))*time.Second
}

func pickDish(r *rng, dishes []seed.Dish, weights []float64, total float64) seed.Dish {
	x := r.float() * total
	for i, d := range dishes {
		if x < weights[i] {
			return d
		}
		x -= weights[i]
	}
	return dishes[len(dishes)-1]
}

// uuid строит UUID v4-вида из генератора (не из crypto/rand: нужен детерминизм).
func uuid(r *rng) string {
	a, b := r.next(), r.next()
	a = a&^(0xf<<12) | 4<<12
	b = b&^(0x3<<62) | 2<<62
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", a>>32, (a>>16)&0xffff, a&0xffff, b>>48, b&0xffffffffffff)
}
