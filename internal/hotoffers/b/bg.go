package b

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/hotoffers"
)

// productState — состояние продукта в цикле жадного распределения.
type productState struct {
	id      string
	rate    float64
	stock   domain.Qty // остаток по всем доступным партиям, включая обречённые
	risk    domain.Qty // количество под риском по необречённым партиям
	batch   *domain.Batch
	batchAt domain.Qty // риск опорной партии e*
	expires time.Time  // e*: срок первой необречённой партии с риском
	hours   float64    // W(now, e*)
}

// norm — агрегированная норма продукта в блюде.
type norm struct {
	product string
	qty     domain.Qty
}

// Rank — алгоритм Bg: жадное распределение с предохранителем (solution.md, 2.7).
// Чистая функция: не читает часы, БД и сеть.
func Rank(in hotoffers.Input) ([]hotoffers.Offer, []hotoffers.Excluded) {
	states := buildStates(in)

	dishes := append([]domain.Dish(nil), in.Dishes...)
	sort.Slice(dishes, func(i, j int) bool { return dishes[i].ID < dishes[j].ID })

	type cand struct {
		id    string
		norms []norm
	}
	var cands []cand
	var excluded []hotoffers.Excluded
	for _, d := range dishes {
		if in.Stopped[d.ID] {
			continue
		}
		norms := aggregate(d.Recipe)
		if len(norms) == 0 {
			continue
		}
		if ex, bad := safeguard(d.ID, norms, states, in.Config.CoverHours); bad {
			excluded = append(excluded, ex)
			continue
		}
		cands = append(cands, cand{d.ID, norms})
	}

	counts := map[string]int64{}
	prio := map[string]float64{}
	reasons := map[string][]hotoffers.Reason{}
	var order []string

	for iter := 0; iter < in.Config.MaxIter; iter++ {
		best := -1
		var bestGain float64
		var bestExp time.Time
		for i, c := range cands {
			g, exp, ok := gain(c.norms, states, in)
			if !ok || g <= 0 {
				continue
			}
			if best < 0 || g > bestGain || (g == bestGain && exp.Before(bestExp)) {
				best, bestGain, bestExp = i, g, exp
			}
		}
		if best < 0 {
			break
		}
		c := cands[best]
		if counts[c.id] == 0 {
			order = append(order, c.id)
			prio[c.id] = bestGain
			reasons[c.id] = buildReasons(c.norms, states, in)
		}
		counts[c.id]++
		for _, n := range c.norms {
			s := states[n.product]
			s.stock -= n.qty
			if s.risk > n.qty {
				s.risk -= n.qty
			} else {
				s.risk = 0
			}
		}
	}

	var offers []hotoffers.Offer
	for _, id := range order {
		if len(offers) >= in.Config.TopN {
			break
		}
		offers = append(offers, hotoffers.Offer{
			DishID:         id,
			Priority:       prio[id],
			TargetPortions: counts[id],
			Reasons:        reasons[id],
		})
	}
	return offers, excluded
}

func aggregate(recipe []domain.RecipeItem) []norm {
	m := map[string]domain.Qty{}
	for _, r := range recipe {
		if r.Quantity > 0 {
			m[r.ProductID] += r.Quantity
		}
	}
	out := make([]norm, 0, len(m))
	for p, q := range m {
		out = append(out, norm{p, q})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].product < out[j].product })
	return out
}

func buildStates(in hotoffers.Input) map[string]*productState {
	byProduct := map[string][]domain.Batch{}
	for _, b := range in.Batches {
		if b.Remaining <= 0 || !in.Now.Before(b.ExpiresAt) {
			continue
		}
		byProduct[b.ProductID] = append(byProduct[b.ProductID], b)
	}
	states := map[string]*productState{}
	for p, bs := range byProduct {
		sort.Slice(bs, func(i, j int) bool {
			if !bs[i].ExpiresAt.Equal(bs[j].ExpiresAt) {
				return bs[i].ExpiresAt.Before(bs[j].ExpiresAt)
			}
			return bs[i].ID < bs[j].ID
		})
		s := &productState{id: p, rate: in.RateHour[p]}
		var used float64
		for i := range bs {
			b := bs[i]
			s.stock += b.Remaining
			w := workingHours(in.Now, b.ExpiresAt, in.Schedule)
			if w < in.Config.WMin {
				continue // обречённая: вне проекции и вне рекомендаций
			}
			cap := s.rate*w - used
			if cap < 0 {
				cap = 0
			}
			use := float64(b.Remaining)
			if cap < use {
				use = cap
			}
			risk := b.Remaining - domain.Qty(use)
			used += use
			if risk > 0 {
				s.risk += risk
				if s.batch == nil {
					s.batch = &bs[i]
					s.batchAt = risk
					s.expires = b.ExpiresAt
					s.hours = w
				}
			}
		}
		states[p] = s
	}
	return states
}

// safeguard исключает блюдо с дефицитным нерисковым ингредиентом.
func safeguard(dishID string, norms []norm, states map[string]*productState, cover float64) (hotoffers.Excluded, bool) {
	for _, n := range norms {
		s := states[n.product]
		if s == nil {
			continue // нет в наличии: блюдо и так недоступно в цикле
		}
		if s.risk == 0 && s.rate > 0 && float64(s.stock) < cover*s.rate {
			return hotoffers.Excluded{
				DishID:    dishID,
				ProductID: n.product,
				Why: fmt.Sprintf("дефицитный нерисковый ингредиент: остаток %d < %.0f ч × %.2f/ч",
					s.stock, cover, s.rate),
			}, true
		}
	}
	return hotoffers.Excluded{}, false
}

// gain — спасённая стоимость в рабочий час до срока; exp — min e*_p по рисковым ингредиентам.
func gain(norms []norm, states map[string]*productState, in hotoffers.Input) (float64, time.Time, bool) {
	var g float64
	var exp time.Time
	for _, n := range norms {
		s := states[n.product]
		if s == nil || s.stock < n.qty {
			return 0, time.Time{}, false
		}
		if s.risk <= 0 || s.hours <= 0 {
			continue
		}
		q := n.qty
		if s.risk < q {
			q = s.risk
		}
		g += float64(in.Price[n.product]) * float64(q) / s.hours
		if exp.IsZero() || s.expires.Before(exp) {
			exp = s.expires
		}
	}
	return round6(g), exp, true
}

func buildReasons(norms []norm, states map[string]*productState, in hotoffers.Input) []hotoffers.Reason {
	var rs []hotoffers.Reason
	for _, n := range norms {
		s := states[n.product]
		if s == nil || s.risk <= 0 || s.batch == nil {
			continue
		}
		rs = append(rs, hotoffers.Reason{
			ProductID:        n.product,
			BatchID:          s.batch.ID,
			AtRisk:           s.batchAt,
			ExpiresAt:        s.expires,
			WorkingHoursLeft: s.hours,
		})
	}
	return rs
}

func round6(x float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'e', 5, 64), 64)
	return v
}

// workingHours — W(from, to): рабочие часы расписания в интервале.
func workingHours(from, to time.Time, sch domain.Schedule) float64 {
	if !to.After(from) {
		return 0
	}
	loc := sch.Location
	if loc == nil {
		loc = time.UTC
	}
	f, t := from.In(loc), to.In(loc)
	y, m, d := f.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, loc)
	var total time.Duration
	for ; day.Before(t); day = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc) {
		open := day.Add(sch.OpenAt)
		closeT := day.Add(sch.CloseAt)
		lo, hi := open, closeT
		if f.After(lo) {
			lo = f
		}
		if t.Before(hi) {
			hi = t
		}
		if hi.After(lo) {
			total += hi.Sub(lo)
		}
	}
	return total.Hours()
}
