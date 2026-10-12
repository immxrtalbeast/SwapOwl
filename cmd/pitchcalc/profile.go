package main

import (
	"math"
	"math/rand"
	"sort"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/seed"
)

// profile — допущения «кафе на N чеков в день»: спрос и правило закупки.
// Значения по умолчанию описаны в docs/pitch-numbers.md.
type profile struct {
	Checks      int     // чеков (заказов) в сутки в среднем
	CoverDays   float64 // сверх интервала между поставками: на сколько суток вперёд заказываем
	Reserve     float64 // запас сверх прогноза, доля (0,10 = +10 %)
	ForecastErr float64 // ошибка прогноза спроса на поставку, ±доля
	ShortShare  int     // % количества поставки, пришедший с коротким сроком (остатки поставщика)
	ShortFactor float64 // короткий срок = срок × ShortFactor
}

func defaultProfile() profile {
	return profile{Checks: 120, CoverDays: 0.25, Reserve: 0.05, ForecastErr: 0.10, ShortShare: 20, ShortFactor: 0.5}
}

// portionsPerCheck — среднее число порций в чеке: 1–2 позиции по 1–2 порции (как у генератора демо-сида).
const portionsPerCheck = 2.2

// perishable — продукты со сроком до 5 суток привозят ежедневно, остальные — раз в 3 суток.
func perishable(p seed.Product) bool { return p.ShelfLifeHours <= 120 }

// dishPerDay — ожидаемое число порций блюда в сутки в профиле: доли блюд берутся из PriorDaily демо-каталога.
func (pf profile) dishPerDay() map[string]float64 {
	total := 0.0
	for _, d := range seed.Dishes() {
		total += d.PriorDaily
	}
	out := map[string]float64{}
	for _, d := range seed.Dishes() {
		out[d.ID] = float64(pf.Checks) * portionsPerCheck * d.PriorDaily / total
	}
	return out
}

// productNeed — ожидаемый суточный расход продуктов в базовых единицах.
func (pf profile) productNeed() map[string]float64 {
	per := pf.dishPerDay()
	need := map[string]float64{}
	for _, d := range seed.Dishes() {
		for _, it := range d.Recipe {
			need[it.ProductID] += per[d.ID] * float64(it.Quantity)
		}
	}
	return need
}

// check — чек: время и позиции.
type check struct {
	At    time.Time
	Items []checkItem
}

type checkItem struct {
	DishID   string
	Portions int
}

// genChecks строит чеки на days суток от t0 (t0 — открытие). Результат зависит только от сида.
func genChecks(seedValue int64, days int, pf profile, t0 time.Time, loc *time.Location) []check {
	hours := seed.WorkingHours()
	dishes := seed.Dishes()
	total := 0.0
	for _, d := range dishes {
		total += d.PriorDaily
	}
	var out []check
	for d := 0; d < days; d++ {
		day := t0.In(loc).AddDate(0, 0, d)
		midnight := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
		r := rand.New(rand.NewSource(int64(hash(seedValue, int64(d), 11) >> 1)))
		n := int(math.Round(float64(pf.Checks) * (0.8 + 0.4*r.Float64())))
		var dayChecks []check
		for i := 0; i < n; i++ {
			var lo, hi time.Duration
			switch x := r.Float64(); {
			case x < 0.4:
				lo, hi = 12*time.Hour, 15*time.Hour
			case x < 0.8:
				lo, hi = 18*time.Hour, 21*time.Hour
			default:
				lo, hi = hours.OpenAt, hours.CloseAt
			}
			at := midnight.Add(lo + time.Duration(r.Int63n(int64((hi-lo)/time.Second)))*time.Second)
			c := check{At: at}
			used := map[string]bool{}
			for k, lines := 0, 1+r.Intn(2); k < lines; k++ {
				x := r.Float64() * total
				pick := dishes[len(dishes)-1]
				for _, ds := range dishes {
					if x < ds.PriorDaily {
						pick = ds
						break
					}
					x -= ds.PriorDaily
				}
				if used[pick.ID] {
					continue
				}
				used[pick.ID] = true
				c.Items = append(c.Items, checkItem{DishID: pick.ID, Portions: 1 + r.Intn(2)})
			}
			dayChecks = append(dayChecks, c)
		}
		sort.SliceStable(dayChecks, func(i, j int) bool { return dayChecks[i].At.Before(dayChecks[j].At) })
		out = append(out, dayChecks...)
	}
	return out
}

const deliveryAt = 8 * time.Hour // поставка приходит в 08:00, за 2 часа до открытия

// procure — правило закупки на утро дня dayIdx: заказ «до уровня» — прогноз спроса на
// (интервал между поставками + CoverDays) суток с запасом Reserve, минус текущий остаток,
// вверх до упаковки. Прогноз ошибается на ±ForecastErr (по хэшу сида, дня и продукта).
func (pf profile) procure(w *warehouse, now time.Time, dayIdx int, seedValue int64, need map[string]float64, price map[string]int64) (spent int64) {
	stock := w.stock(now)
	for pi, p := range seed.Products() {
		cycle := 3
		if perishable(p) {
			cycle = 1
		}
		if (dayIdx+pi)%cycle != 0 {
			continue
		}
		errF := 1 + pf.ForecastErr*(2*unit(hash(seedValue, int64(dayIdx), int64(100+pi)))-1)
		target := need[p.ID] * (float64(cycle) + pf.CoverDays) * (1 + pf.Reserve) * errF
		qty := domain.Qty(math.Ceil(target)) - stock[p.ID]
		if qty <= 0 {
			continue
		}
		qty = seed.RoundToPack(qty, p.PackSize)
		short := qty * domain.Qty(pf.ShortShare) / 100
		shelf := time.Duration(p.ShelfLifeHours) * time.Hour
		w.receive(p.ID, qty-short, now, shelf)
		w.receive(p.ID, short, now, time.Duration(float64(shelf)*pf.ShortFactor))
		spent += cost(qty, price[p.ID])
	}
	return spent
}
