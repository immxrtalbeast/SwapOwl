package simulator

import (
	"fmt"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/domain/units"
	"umirhack/internal/generator"
	"umirhack/internal/hotoffers"
	hotr "umirhack/internal/hotoffers/a"
	hotbg "umirhack/internal/hotoffers/b"
	"umirhack/internal/seed"
)

// Algo — вариант прогона: без рекомендаций (C0), алгоритм (R, Bg) или контроль (RND, POP, C1).
type Algo string

// Варианты прогона.
const (
	AlgoC0  Algo = "C0"  // без рекомендаций
	AlgoR   Algo = "R"   // hotoffers/a
	AlgoBg  Algo = "Bg"  // hotoffers/b
	AlgoRND Algo = "RND" // случайные блюда не из стоп-листа
	AlgoPOP Algo = "POP" // самые популярные по оценке спроса
	AlgoC1  Algo = "C1"  // блюда с ближайшим сроком партии
)

// Algos — порядок вывода.
var Algos = []Algo{AlgoC0, AlgoR, AlgoBg, AlgoRND, AlgoPOP, AlgoC1}

// Model — модель влияния рекомендации на спрос (solution.md, 2.7).
type Model string

// Модели влияния.
const (
	ModelM1 Model = "M1" // множитель u на спрос блюда
	ModelM2 Model = "M2" // число гостей фиксировано, меняется вес выбора
)

// Параметры оценки спроса (solution.md, 2.6).
const (
	priorDays  = 2.0 // n0
	windowDays = 7.0 // N
)

// DefaultConfig — параметры алгоритмов по умолчанию (solution.md, 2.7).
func DefaultConfig() hotoffers.Config {
	return hotoffers.Config{TopN: 3, WMin: domain.DoomedMinHours, CoverHours: 12, MaxIter: 200}
}

// Params — параметры одного прогона.
type Params struct {
	Seed int64
	Days int
	U    float64 // множитель подъёма; U <= 1 — без влияния
	// Model — модель влияния; пусто — M2.
	Model Model
	Cfg   hotoffers.Config
}

// Result — итог прогона.
type Result struct {
	Algo         Algo
	Orders       int
	Rejected     int
	HotLines     int   // строки заказов, замещённые горячим блюдом
	ReceivedCost int64 // стоимость поступившего, копейки
	WriteoffQty  int64 // списано по просрочке, базовые единицы (сумма разных единиц)
	WriteoffCost int64 // списано по просрочке, копейки — основная метрика
}

// WriteoffShare — доля списанного по просрочке от поступившего.
func (r Result) WriteoffShare() float64 {
	if r.ReceivedCost == 0 {
		return 0
	}
	return float64(r.WriteoffCost) / float64(r.ReceivedCost)
}

func cost(q domain.Qty, pricePer1000 int64) int64 { return int64(q) * pricePer1000 / 1000 }

// Start — момент начала прогона: открытие ресторана первого дня.
func Start() (time.Time, *time.Location, error) {
	h := seed.WorkingHours()
	loc, err := time.LoadLocation(h.TZ)
	if err != nil {
		return time.Time{}, nil, err
	}
	return time.Date(2026, 1, 5, 0, 0, 0, 0, loc).Add(h.OpenAt), loc, nil
}

// Events строит поток событий генератора для прогона; общий для всех Algo.
func Events(p Params) ([]generator.Event, error) {
	t0, _, err := Start()
	if err != nil {
		return nil, err
	}
	return generator.Generate(p.Seed, t0, t0.Add(time.Duration(p.Days)*24*time.Hour))
}

// Run гонит поток событий базового сида через модель склада с выбранным алгоритмом.
// Один seed и параметры — один результат.
func Run(p Params, algo Algo, events []generator.Event) (Result, error) {
	return RunWorld(seedWorld(), p, algo, events)
}

// RunWorld — Run для произвольного мира (справочники и стартовые поставки сценария).
func RunWorld(w0 World, p Params, algo Algo, events []generator.Event) (Result, error) {
	t0, loc, err := Start()
	if err != nil {
		return Result{}, err
	}
	end := t0.Add(time.Duration(p.Days) * 24 * time.Hour)
	hours := seed.WorkingHours()
	sch := domain.Schedule{Location: loc, OpenAt: hours.OpenAt, CloseAt: hours.CloseAt}

	products := map[string]seed.Product{}
	price := map[string]int64{}
	for _, pr := range w0.Products {
		products[pr.ID] = pr
		price[pr.ID] = pr.PriceMinorPer1000
	}
	var dishes []domain.Dish
	recipes := map[string]map[string]domain.Qty{}
	priors := map[string]float64{}
	var total float64
	for _, d := range w0.Dishes {
		total += d.PriorDaily
		dishes = append(dishes, domain.Dish{ID: d.ID, Name: d.Name, Recipe: d.Recipe})
		priors[d.ID] = d.PriorDaily
		r := map[string]domain.Qty{}
		for _, it := range d.Recipe {
			r[it.ProductID] += it.Quantity
		}
		recipes[d.ID] = r
	}

	res := Result{Algo: algo}
	w := &warehouse{}
	account := func(b domain.Batch) {
		res.WriteoffQty += int64(b.Remaining)
		res.WriteoffCost += cost(b.Remaining, price[b.ProductID])
	}
	receive := func(d seed.Delivery, at time.Time) error {
		for _, it := range d.Items {
			pr, ok := products[it.ProductID]
			if !ok {
				return fmt.Errorf("simulator: неизвестный продукт %q", it.ProductID)
			}
			uq, err := units.Parse(it.Quantity, it.Unit, units.Unit(pr.Unit))
			if err != nil {
				return fmt.Errorf("simulator: поставка %s: %w", d.ID, err)
			}
			q := domain.Qty(uq)
			w.receive(pr.ID, q, at, time.Duration(it.ShelfLifeHours)*time.Hour)
			res.ReceivedCost += cost(q, pr.PriceMinorPer1000)
		}
		return nil
	}

	for _, d := range w0.Start(t0) {
		if err := receive(d, t0.Add(-time.Second)); err != nil {
			return Result{}, err
		}
	}

	var history []domain.OrderedDish
	orderIdx := 0
	for _, ev := range events {
		if !ev.At.Before(end) {
			break
		}
		w.expire(ev.At, account)
		switch ev.Kind {
		case generator.KindDelivery:
			if err := receive(*ev.Delivery, ev.At); err != nil {
				return Result{}, err
			}
		case generator.KindOrder:
			var offers []hotoffers.Offer
			if algo != AlgoC0 && p.U > 1 {
				offers, err = rankOffers(algo, p.Seed, orderIdx, ev.At, w, dishes, recipes, priors, price, history, sch, p.Cfg)
				if err != nil {
					return Result{}, err
				}
			}
			needs := map[string]domain.Qty{}
			add := func(id string, portions int) {
				history = append(history, domain.OrderedDish{At: ev.At, DishID: id, Portions: int64(portions)})
				for pid, q := range recipes[id] {
					needs[pid] += q * domain.Qty(portions)
				}
			}
			for li, it := range ev.Order.Items {
				id := it.DishID
				if p.Model == ModelM1 {
					add(id, it.Portions)
					for _, extra := range raise(p, orderIdx, li, offers, priors, total) {
						add(extra, 1)
						res.HotLines++
					}
					continue
				}
				if sub, ok := substitute(p, orderIdx, li, offers, priors, total); ok {
					id = sub
					res.HotLines++
				}
				add(id, it.Portions)
			}
			res.Orders++
			if !w.consume(needs, ev.At) {
				res.Rejected++
			}
			orderIdx++
		}
	}
	w.expire(end, account)
	return res, nil
}

// rankOffers считает горячие предложения по текущему снимку склада и истории заказов.
func rankOffers(algo Algo, seedValue int64, orderIdx int, now time.Time, w *warehouse, dishes []domain.Dish, recipes map[string]map[string]domain.Qty,
	priors map[string]float64, price map[string]int64, history []domain.OrderedDish, sch domain.Schedule, cfg hotoffers.Config) ([]hotoffers.Offer, error) {
	stock := w.stock(now)
	stopped := map[string]bool{}
	for id, r := range recipes {
		for pid, q := range r {
			if stock[pid] < q {
				stopped[id] = true
				break
			}
		}
	}
	switch algo {
	case AlgoRND:
		return randomOffers(seedValue, orderIdx, dishes, stopped, cfg.TopN), nil
	case AlgoC1:
		return nearestOffers(now, w.available(now), dishes, recipes, stopped, cfg.TopN), nil
	}
	lambda, err := domain.DishDemand(domain.DemandInput{
		Now: now, WindowDays: windowDays, PriorDays: priorDays, Priors: priors, History: history,
	})
	if err != nil {
		return nil, err
	}
	rate := map[string]float64{}
	for pid, perDay := range domain.ProductDemandPerDay(dishes, lambda) {
		rate[pid] = domain.RatePerHour(perDay, sch)
	}
	if algo == AlgoPOP {
		return popularOffers(lambda, dishes, stopped, cfg.TopN), nil
	}
	in := hotoffers.Input{
		Now: now, Batches: w.available(now), Dishes: dishes, Stopped: stopped,
		Price: price, RateHour: rate, Schedule: sch, Config: cfg,
	}
	var rank hotoffers.Ranker = hotr.Rank
	if algo == AlgoBg {
		rank = hotbg.Rank
	}
	offers, _ := rank(in)
	if len(offers) > cfg.TopN {
		offers = offers[:cfg.TopN]
	}
	return offers, nil
}

// substitute — модель M2: число гостей фиксировано, рекомендация меняет только
// вес блюда при выборе. Позиция i получает вес prior·u_i, u_i = 1 + (u−1)/i.
// Случайные числа зависят только от (seed, заказ, строка) — не от алгоритма,
// поэтому прогоны парные.
func substitute(p Params, orderIdx, line int, offers []hotoffers.Offer, priors map[string]float64, total float64) (string, bool) {
	if len(offers) == 0 || p.U <= 1 {
		return "", false
	}
	var extra float64
	extras := make([]float64, len(offers))
	for i, o := range offers {
		ui := 1 + (p.U-1)/float64(i+1)
		extras[i] = priors[o.DishID] * (ui - 1)
		extra += extras[i]
	}
	if extra <= 0 {
		return "", false
	}
	h := hash(p.Seed, int64(orderIdx), int64(line))
	if unit(h) >= extra/(total+extra) {
		return "", false
	}
	x := unit(hash(int64(h), 1, 2)) * extra
	for i, e := range extras {
		if x < e {
			return offers[i].DishID, true
		}
		x -= e
	}
	return offers[len(offers)-1].DishID, true
}

// raise — модель M1: множитель u_i на спрос блюда в позиции i. К строке заказа с
// вероятностью (u_i−1)·prior_i/Σprior добавляется ещё одна порция горячего блюда.
// Случайные числа зависят только от (seed, заказ, строка, позиция).
func raise(p Params, orderIdx, line int, offers []hotoffers.Offer, priors map[string]float64, total float64) []string {
	if p.U <= 1 || total <= 0 {
		return nil
	}
	var out []string
	for i, o := range offers {
		ui := 1 + (p.U-1)/float64(i+1)
		if unit(hash(p.Seed, int64(orderIdx), int64(line*16+i+100))) < (ui-1)*priors[o.DishID]/total {
			out = append(out, o.DishID)
		}
	}
	return out
}

func hash(a, b, c int64) uint64 {
	z := uint64(a)*0x9e3779b97f4a7c15 ^ uint64(b)*0xbf58476d1ce4e5b9 ^ uint64(c)*0x94d049bb133111eb
	z ^= z >> 30
	z *= 0xbf58476d1ce4e5b9
	z ^= z >> 27
	z *= 0x94d049bb133111eb
	return z ^ z>>31
}

func unit(h uint64) float64 { return float64(h>>11) / (1 << 53) }
