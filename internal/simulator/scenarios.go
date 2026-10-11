package simulator

import (
	"fmt"
	"math"
	"sort"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/generator"
	"umirhack/internal/seed"
)

// World — справочники и источник событий одного прогона.
type World struct {
	Name     string
	Products []seed.Product
	Dishes   []seed.Dish
	// Start даёт стартовые поставки; принимаются за секунду до t0.
	Start func(t0 time.Time) []seed.Delivery
	// Events строит поток событий на days суток от t0.
	Events func(seedValue int64, t0 time.Time, days int) ([]generator.Event, error)
}

func seedWorld() World {
	return World{
		Name: "seed", Products: seed.Products(), Dishes: seed.Dishes(), Start: seed.StartingDeliveries,
		Events: func(s int64, t0 time.Time, days int) ([]generator.Event, error) {
			return generator.Generate(s, t0, t0.Add(time.Duration(days)*24*time.Hour))
		},
	}
}

// Scenario — один из пяти сценариев solution.md, 2.7. В каждом блюд с рисковыми
// ингредиентами (продукт с поставкой больше спроса за срок хранения) больше k = 3.
type Scenario struct {
	Name  string
	Title string
	World World
}

type prodSpec struct {
	id     string
	price  int64 // копеек за 1000 г
	shelfH int
	cover  float64 // размер поставки в сутках ожидаемой потребности
	every  int     // поставка раз в every суток
	pack   domain.Qty
}

type dishSpec struct {
	id     string
	prior  float64
	recipe []domain.RecipeItem
}

func ri(p string, q domain.Qty) domain.RecipeItem {
	return domain.RecipeItem{ProductID: p, Quantity: q}
}

var rice = prodSpec{"rice", 8000, 4320, 1.5, 3, 1000}

// Scenarios — пять сценариев в порядке из solution.md, 2.7.
func Scenarios() []Scenario {
	return []Scenario{
		{"perishable3", "скоропорт в трёх блюдах", build("perishable3",
			[]prodSpec{
				{"fish", 200000, 48, 1.7, 1, 500}, {"cream", 25000, 72, 1.3, 1, 1000},
				{"chicken", 32000, 72, 1.0, 1, 1000}, rice},
			[]dishSpec{
				{"fish-soup", 14, []domain.RecipeItem{ri("fish", 120), ri("cream", 40)}},
				{"fish-grill", 12, []domain.RecipeItem{ri("fish", 150), ri("rice", 100)}},
				{"fish-bowl", 10, []domain.RecipeItem{ri("fish", 90), ri("rice", 150)}},
				{"creamy-chicken", 16, []domain.RecipeItem{ri("chicken", 150), ri("cream", 80)}},
				{"chicken-rice", 22, []domain.RecipeItem{ri("chicken", 130), ri("rice", 120)}},
				{"rice-plain", 14, []domain.RecipeItem{ri("rice", 250)}},
			})},
		{"oversupply", "избыточная поставка", build("oversupply",
			[]prodSpec{
				{"chicken", 32000, 72, 1.9, 1, 1000}, {"tomato", 20000, 72, 1.9, 1, 1000},
				{"lettuce", 40000, 48, 1.9, 1, 500}, {"milk", 9000, 72, 1.8, 1, 1000}, rice},
			[]dishSpec{
				{"caesar", 20, []domain.RecipeItem{ri("chicken", 100), ri("lettuce", 80)}},
				{"tomato-salad", 15, []domain.RecipeItem{ri("tomato", 150), ri("lettuce", 40)}},
				{"chicken-tomato", 18, []domain.RecipeItem{ri("chicken", 140), ri("tomato", 80)}},
				{"milk-porridge", 14, []domain.RecipeItem{ri("milk", 250), ri("rice", 60)}},
				{"wrap", 12, []domain.RecipeItem{ri("chicken", 120), ri("tomato", 40), ri("lettuce", 20)}},
				{"rice-plain", 14, []domain.RecipeItem{ri("rice", 250)}},
			})},
		{"two-risky", "блюдо с двумя рисковыми ингредиентами", build("two-risky",
			[]prodSpec{
				{"salmon", 180000, 48, 1.5, 1, 500}, {"cream", 25000, 48, 1.5, 1, 1000},
				{"tomato", 20000, 48, 1.2, 1, 1000}, rice},
			[]dishSpec{
				{"salmon-cream-pasta", 18, []domain.RecipeItem{ri("salmon", 120), ri("cream", 100), ri("rice", 80)}},
				{"salmon-steak", 8, []domain.RecipeItem{ri("salmon", 180), ri("rice", 100)}},
				{"cream-soup", 12, []domain.RecipeItem{ri("cream", 150), ri("tomato", 100)}},
				{"tomato-rice", 14, []domain.RecipeItem{ri("tomato", 120), ri("rice", 120)}},
				{"salmon-tartare", 6, []domain.RecipeItem{ri("salmon", 100), ri("tomato", 50)}},
				{"rice-plain", 14, []domain.RecipeItem{ri("rice", 250)}},
			})},
		{"scarce-companion", "рисковый продукт с дефицитным попутчиком", build("scarce-companion",
			[]prodSpec{
				{"steak", 210000, 48, 1.7, 1, 1000}, {"truffle-sauce", 150000, 720, 0.8, 1, 200}, rice},
			[]dishSpec{
				{"steak-truffle", 14, []domain.RecipeItem{ri("steak", 200), ri("truffle-sauce", 40)}},
				{"steak-plain", 8, []domain.RecipeItem{ri("steak", 200), ri("rice", 100)}},
				{"steak-pepper", 8, []domain.RecipeItem{ri("steak", 180), ri("truffle-sauce", 20)}},
				{"steak-burger", 12, []domain.RecipeItem{ri("steak", 120), ri("rice", 60)}},
				{"truffle-pasta", 10, []domain.RecipeItem{ri("truffle-sauce", 30), ri("rice", 120)}},
				{"rice-plain", 14, []domain.RecipeItem{ri("rice", 250)}},
			})},
		{"cheap-vs-dear", "дешёвая зелень против дорогого продукта", build("cheap-vs-dear",
			[]prodSpec{
				{"herbs", 30000, 24, 1.8, 1, 500}, {"wagyu", 900000, 48, 1.4, 1, 500}, rice},
			[]dishSpec{
				{"herb-salad", 18, []domain.RecipeItem{ri("herbs", 100), ri("rice", 50)}},
				{"herb-soup", 12, []domain.RecipeItem{ri("herbs", 80), ri("rice", 100)}},
				{"herb-wrap", 10, []domain.RecipeItem{ri("herbs", 60), ri("rice", 120)}},
				{"wagyu-steak", 5, []domain.RecipeItem{ri("wagyu", 200), ri("rice", 100)}},
				{"wagyu-burger", 8, []domain.RecipeItem{ri("wagyu", 120), ri("rice", 80)}},
				{"wagyu-tartare", 4, []domain.RecipeItem{ri("wagyu", 100), ri("herbs", 20)}},
				{"rice-plain", 14, []domain.RecipeItem{ri("rice", 250)}},
			})},
	}
}

// hourProfile — доля дневного спроса по часам работы с 10:00; пики на обед и ужин.
var hourProfile = []float64{1, 2, 4, 4, 3, 1.5, 1, 1.5, 3, 4, 4, 3, 1}

func build(name string, ps []prodSpec, ds []dishSpec) World {
	need := map[string]float64{}
	var total float64
	w := World{Name: name}
	for _, d := range ds {
		total += d.prior
		for _, it := range d.recipe {
			need[it.ProductID] += d.prior * float64(it.Quantity)
		}
		w.Dishes = append(w.Dishes, seed.Dish{ID: d.id, Name: d.id, PriorDaily: d.prior, Recipe: d.recipe})
	}
	for _, p := range ps {
		w.Products = append(w.Products, seed.Product{
			ID: p.id, Name: p.id, Unit: domain.UnitG, PriceMinorPer1000: p.price, PackSize: p.pack, ShelfLifeHours: p.shelfH,
		})
	}
	delivery := func(id string, at time.Time, pick func(prodSpec) (domain.Qty, bool)) seed.Delivery {
		d := seed.Delivery{ID: id, Number: id, CreatedAt: at}
		for _, p := range ps {
			qty, ok := pick(p)
			if !ok {
				continue
			}
			q, u := seed.FormatQty(seed.RoundToPack(qty, p.pack), domain.UnitG)
			d.Items = append(d.Items, seed.DeliveryItem{ProductID: p.id, Name: p.id, Quantity: q, Unit: u, ShelfLifeHours: p.shelfH})
		}
		return d
	}
	w.Start = func(t0 time.Time) []seed.Delivery {
		return []seed.Delivery{delivery("D-START-"+name, t0.Add(-time.Hour), func(p prodSpec) (domain.Qty, bool) {
			return domain.Qty(math.Ceil(need[p.id] * math.Min(p.cover, 1.5))), true
		})}
	}
	w.Events = func(s int64, t0 time.Time, days int) ([]generator.Event, error) {
		hours := seed.WorkingHours()
		var out []generator.Event
		var profile float64
		for _, v := range hourProfile {
			profile += v
		}
		for day := 0; day < days; day++ {
			r := &rng{s: uint64(s)*0x9e3779b97f4a7c15 ^ uint64(day+1)*0xd1b54a32d192ed03}
			r.next()
			d0 := t0.Add(time.Duration(day) * 24 * time.Hour)
			if at := d0.Add(-hours.OpenAt + 8*time.Hour); day > 0 {
				id := fmt.Sprintf("D-%s-%d-%d", name, s, day)
				d := delivery(id, at, func(p prodSpec) (domain.Qty, bool) {
					if (day-1)%p.every != 0 {
						return 0, false
					}
					return domain.Qty(math.Ceil(need[p.id] * p.cover * float64(p.every) * (0.75 + 0.5*r.float()))), true
				})
				if len(d.Items) > 0 {
					out = append(out, generator.Event{At: at, Kind: generator.KindDelivery, Delivery: &d})
				}
			}
			orders := total / 3 * (0.8 + 0.4*r.float())
			for h, v := range hourProfile {
				n := r.poisson(orders * v / profile)
				for i := 0; i < n; i++ {
					at := d0.Add(time.Duration(h)*time.Hour + time.Duration(r.intn(3600))*time.Second)
					o := generator.Order{ID: fmt.Sprintf("O-%s-%d-%d-%d-%d", name, s, day, h, i), At: at}
					used := map[string]bool{}
					for k, lines := 0, 1+r.intn(2); k < lines; k++ {
						x := r.float() * total
						pick := ds[len(ds)-1].id
						for _, d := range ds {
							if x < d.prior {
								pick = d.id
								break
							}
							x -= d.prior
						}
						if used[pick] {
							continue
						}
						used[pick] = true
						o.Items = append(o.Items, generator.OrderItem{DishID: pick, Portions: 1 + r.intn(2)})
					}
					out = append(out, generator.Event{At: at, Kind: generator.KindOrder, Order: &o})
				}
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
		return out, nil
	}
	return w
}

// rng — splitmix64: детерминированный источник от одного seed.
type rng struct{ s uint64 }

func (r *rng) next() uint64 {
	r.s += 0x9e3779b97f4a7c15
	z := r.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func (r *rng) intn(n int) int { return int(r.next() % uint64(n)) }
func (r *rng) float() float64 { return unit(r.next()) }
func (r *rng) poisson(l float64) int {
	limit, p, k := math.Exp(-l), 1.0, 0
	for {
		p *= r.float()
		if p <= limit {
			return k
		}
		k++
	}
}
