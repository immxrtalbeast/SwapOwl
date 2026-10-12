// Команда pitchcalc считает цифры для питча: рубли списаний за месяц на точку
// при трёх режимах работы склада на одном и том же потоке событий генератора.
// Без БД и HTTP; склад в памяти, время приходит из событий. Описание — docs/pitch-numbers.md.
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/domain/units"
	"umirhack/internal/generator"
	"umirhack/internal/hotoffers"
	hotbg "umirhack/internal/hotoffers/b"
	"umirhack/internal/seed"
)

func main() {
	days := flag.Int("days", 30, "длина прогона, суток")
	runs := flag.Int("runs", 20, "число прогонов (сидов)")
	seed0 := flag.Int64("seed0", 1, "первый сид; прогоны идут с сидами seed0 … seed0+runs−1")
	accept := flag.String("accept", "0,30,60", "доля горячих предложений, которые гость берёт, %, через запятую")
	flag.Parse()

	acc, err := parseAccept(*accept)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pitchcalc:", err)
		os.Exit(2)
	}
	if *days < 1 || *runs < 1 {
		fmt.Fprintln(os.Stderr, "pitchcalc: -days и -runs должны быть ≥ 1")
		os.Exit(2)
	}
	rep, err := compute(*days, *runs, *seed0, acc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pitchcalc:", err)
		os.Exit(1)
	}
	rep.write(os.Stdout)
}

func parseAccept(s string) ([]int, error) {
	var out []int
	for _, f := range strings.Split(s, ",") {
		v, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || v < 0 || v > 100 {
			return nil, fmt.Errorf("-accept: %q — нужно целое 0…100", f)
		}
		out = append(out, v)
	}
	return out, nil
}

// order — порядок, в котором режим берёт партии со склада.
type order int

const (
	orderLIFO order = iota // «без системы»: берут то, что лежит сверху — последнюю поставку
	orderFIFO              // по порядку прихода
	orderFEFO              // по сроку годности
)

// variant — режим работы точки.
type variant struct {
	Name   string
	Order  order
	Hot    bool // горячие предложения (HOTOFFERS_ALGO=b)
	Accept int  // % гостей, берущих предложенное блюдо
}

// result — итог одного прогона.
type result struct {
	Orders, Rejected int
	HotTaken         int
	Writeoff         int64   // копейки по закупочной цене
	Received         int64   // копейки
	StopDishHours    float64 // блюдо-часов в стоп-листе в часы работы
	StockOuts        int     // переходов «продукт был → продукта нет»
}

const (
	tickStep = 15 * time.Minute
	topN     = 3
)

func hotConfig() hotoffers.Config {
	return hotoffers.Config{TopN: topN, WMin: domain.DoomedMinHours, CoverHours: 12, MaxIter: 200}
}

func variants(acc []int) []variant {
	vs := []variant{
		{Name: "без системы (LIFO)", Order: orderLIFO},
		{Name: "FIFO", Order: orderFIFO},
	}
	for _, a := range acc {
		vs = append(vs, variant{Name: fmt.Sprintf("SwapOwl (FEFO + горячие, берут %d %%)", a), Order: orderFEFO, Hot: true, Accept: a})
	}
	return vs
}

type warehouse struct {
	batches []domain.Batch
	nextID  int64
}

func (w *warehouse) receive(product string, qty domain.Qty, at time.Time, shelf time.Duration) {
	if qty <= 0 {
		return
	}
	w.nextID++
	w.batches = append(w.batches, domain.Batch{ID: w.nextID, ProductID: product, Quantity: qty, Remaining: qty,
		ReceivedAt: at, ExpiresAt: at.Add(shelf)})
}

func (w *warehouse) expire(now time.Time, visit func(domain.Batch)) {
	kept := w.batches[:0]
	for _, b := range w.batches {
		if b.Remaining <= 0 {
			continue
		}
		if !now.Before(b.ExpiresAt) {
			visit(b)
			continue
		}
		kept = append(kept, b)
	}
	w.batches = kept
}

func (w *warehouse) available(now time.Time) []domain.Batch {
	var out []domain.Batch
	for _, b := range w.batches {
		if b.Remaining > 0 && now.Before(b.ExpiresAt) {
			out = append(out, b)
		}
	}
	return out
}

func (w *warehouse) stock(now time.Time) map[string]domain.Qty {
	s := map[string]domain.Qty{}
	for _, b := range w.available(now) {
		s[b.ProductID] += b.Remaining
	}
	return s
}

// before — true, если партия x в режиме o берётся раньше y.
func before(o order, x, y domain.Batch) bool {
	switch o {
	case orderLIFO:
		if !x.ReceivedAt.Equal(y.ReceivedAt) {
			return x.ReceivedAt.After(y.ReceivedAt)
		}
		return x.ID > y.ID
	case orderFIFO:
		if !x.ReceivedAt.Equal(y.ReceivedAt) {
			return x.ReceivedAt.Before(y.ReceivedAt)
		}
		return x.ID < y.ID
	}
	if !x.ExpiresAt.Equal(y.ExpiresAt) {
		return x.ExpiresAt.Before(y.ExpiresAt)
	}
	return x.ID < y.ID
}

// consume атомарно списывает потребность в порядке режима; при нехватке склад не меняется.
func (w *warehouse) consume(o order, needs map[string]domain.Qty, now time.Time) bool {
	stock := w.stock(now)
	for p, q := range needs {
		if stock[p] < q {
			return false
		}
	}
	var idx []int
	for i, b := range w.batches {
		if b.Remaining > 0 && now.Before(b.ExpiresAt) {
			idx = append(idx, i)
		}
	}
	// вставками: партий на складе десятки
	for i := 1; i < len(idx); i++ {
		for j := i; j > 0 && before(o, w.batches[idx[j]], w.batches[idx[j-1]]); j-- {
			idx[j], idx[j-1] = idx[j-1], idx[j]
		}
	}
	left := map[string]domain.Qty{}
	for p, q := range needs {
		left[p] = q
	}
	for _, i := range idx {
		b := &w.batches[i]
		take := left[b.ProductID]
		if take <= 0 {
			continue
		}
		if b.Remaining < take {
			take = b.Remaining
		}
		b.Remaining -= take
		left[b.ProductID] -= take
	}
	return true
}

func cost(q domain.Qty, pricePer1000 int64) int64 { return int64(q) * pricePer1000 / 1000 }

func hash(a, b, c int64) uint64 {
	z := uint64(a)*0x9e3779b97f4a7c15 ^ uint64(b)*0xbf58476d1ce4e5b9 ^ uint64(c)*0x94d049bb133111eb
	z ^= z >> 30
	z *= 0xbf58476d1ce4e5b9
	z ^= z >> 27
	z *= 0x94d049bb133111eb
	return z ^ z>>31
}

func unit(h uint64) float64 { return float64(h>>11) / (1 << 53) }

// run гонит поток событий через склад в режиме v. Результат зависит только от сида и режима.
func run(v variant, seedValue int64, days int, events []generator.Event, t0 time.Time, loc *time.Location) (result, error) {
	end := t0.Add(time.Duration(days) * 24 * time.Hour)
	hours := seed.WorkingHours()
	sch := domain.Schedule{Location: loc, OpenAt: hours.OpenAt, CloseAt: hours.CloseAt}

	products := map[string]seed.Product{}
	price := map[string]int64{}
	for _, pr := range seed.Products() {
		products[pr.ID] = pr
		price[pr.ID] = pr.PriceMinorPer1000
	}
	var dishes []domain.Dish
	recipes := map[string]map[string]domain.Qty{}
	priors := map[string]float64{}
	for _, d := range seed.Dishes() {
		dishes = append(dishes, domain.Dish{ID: d.ID, Name: d.Name, Recipe: d.Recipe})
		priors[d.ID] = d.PriorDaily
		r := map[string]domain.Qty{}
		for _, it := range d.Recipe {
			r[it.ProductID] += it.Quantity
		}
		recipes[d.ID] = r
	}

	var res result
	w := &warehouse{}
	account := func(b domain.Batch) { res.Writeoff += cost(b.Remaining, price[b.ProductID]) }
	receive := func(d seed.Delivery, at time.Time) error {
		for _, it := range d.Items {
			pr, ok := products[it.ProductID]
			if !ok {
				return fmt.Errorf("неизвестный продукт %q", it.ProductID)
			}
			uq, err := units.Parse(it.Quantity, it.Unit, units.Unit(pr.Unit))
			if err != nil {
				return fmt.Errorf("поставка %s: %w", d.ID, err)
			}
			q := domain.Qty(uq)
			w.receive(pr.ID, q, at, time.Duration(it.ShelfLifeHours)*time.Hour)
			res.Received += cost(q, pr.PriceMinorPer1000)
		}
		return nil
	}
	for _, d := range seed.StartingDeliveries(t0) {
		if err := receive(d, t0.Add(-time.Second)); err != nil {
			return res, err
		}
	}

	hadStock := map[string]bool{}
	for p := range products {
		hadStock[p] = w.stock(t0)[p] > 0
	}
	stopped := func(now time.Time) map[string]bool {
		st := w.stock(now)
		out := map[string]bool{}
		for _, d := range dishes {
			if domain.Servings(d.Recipe, st) == 0 {
				out[d.ID] = true
			}
		}
		return out
	}
	// observe фиксирует «продукт закончился» после любого изменения склада.
	observe := func(now time.Time) {
		st := w.stock(now)
		for p := range products {
			if hadStock[p] && st[p] <= 0 {
				res.StockOuts++
			}
			hadStock[p] = st[p] > 0
		}
	}
	inHours := func(t time.Time) bool {
		l := t.In(loc)
		off := time.Duration(l.Hour())*time.Hour + time.Duration(l.Minute())*time.Minute + time.Duration(l.Second())*time.Second
		return off >= hours.OpenAt && off < hours.CloseAt
	}
	nextTick := t0
	tick := func(until time.Time) {
		for ; nextTick.Before(until); nextTick = nextTick.Add(tickStep) {
			w.expire(nextTick, account)
			observe(nextTick)
			if inHours(nextTick) {
				res.StopDishHours += float64(len(stopped(nextTick))) * tickStep.Hours()
			}
		}
	}

	var history []domain.OrderedDish
	orderIdx := 0
	for _, ev := range events {
		if !ev.At.Before(end) {
			break
		}
		tick(ev.At)
		w.expire(ev.At, account)
		switch ev.Kind {
		case generator.KindDelivery:
			if err := receive(*ev.Delivery, ev.At); err != nil {
				return res, err
			}
		case generator.KindOrder:
			var offers []hotoffers.Offer
			if v.Hot && v.Accept > 0 {
				var err error
				offers, err = rankOffers(ev.At, w, dishes, stopped(ev.At), recipes, priors, price, history, sch)
				if err != nil {
					return res, err
				}
			}
			needs := map[string]domain.Qty{}
			for li, it := range ev.Order.Items {
				id := it.DishID
				// Гостю предлагают одно горячее блюдо на заказ (на первой позиции);
				// предложение из тройки выбирается по хэшу, берёт гость с вероятностью Accept %.
				if li == 0 && len(offers) > 0 {
					h := hash(seedValue, int64(orderIdx), 1)
					if unit(h) < float64(v.Accept)/100 {
						id = offers[int(hash(int64(h), 3, 4)%uint64(len(offers)))].DishID
						res.HotTaken++
					}
				}
				history = append(history, domain.OrderedDish{At: ev.At, DishID: id, Portions: int64(it.Portions)})
				for pid, q := range recipes[id] {
					needs[pid] += q * domain.Qty(it.Portions)
				}
			}
			res.Orders++
			if !w.consume(v.Order, needs, ev.At) {
				res.Rejected++
			}
			orderIdx++
		}
		observe(ev.At)
	}
	tick(end)
	w.expire(end, account)
	return res, nil
}

// rankOffers — горячие предложения алгоритма Bg (HOTOFFERS_ALGO=b) по снимку склада и истории.
func rankOffers(now time.Time, w *warehouse, dishes []domain.Dish, stopped map[string]bool, recipes map[string]map[string]domain.Qty,
	priors map[string]float64, price map[string]int64, history []domain.OrderedDish, sch domain.Schedule) ([]hotoffers.Offer, error) {
	lambda, err := domain.DishDemand(domain.DemandInput{
		Now: now, WindowDays: 7, PriorDays: 2, Priors: priors, History: history,
	})
	if err != nil {
		return nil, err
	}
	rate := map[string]float64{}
	for pid, perDay := range domain.ProductDemandPerDay(dishes, lambda) {
		rate[pid] = domain.RatePerHour(perDay, sch)
	}
	cfg := hotConfig()
	offers, _ := hotbg.Rank(hotoffers.Input{
		Now: now, Batches: w.available(now), Dishes: dishes, Stopped: stopped,
		Price: price, RateHour: rate, Schedule: sch, Config: cfg,
	})
	if len(offers) > cfg.TopN {
		offers = offers[:cfg.TopN]
	}
	return offers, nil
}

func start() (time.Time, *time.Location, error) {
	h := seed.WorkingHours()
	loc, err := time.LoadLocation(h.TZ)
	if err != nil {
		return time.Time{}, nil, err
	}
	return time.Date(2026, 1, 5, 0, 0, 0, 0, loc).Add(h.OpenAt), loc, nil
}
