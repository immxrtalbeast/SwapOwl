package a

import (
	"sort"
	"strconv"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/hotoffers"
)

const defaultTopN = 3

// Rank — алгоритм R («требуемый темп», solution.md 2.7). Чистая функция:
// не читает часы, БД и RateHour; порядок входных срезов на результат не влияет.
func Rank(in hotoffers.Input) ([]hotoffers.Offer, []hotoffers.Excluded) {
	needs := productNeeds(in)

	// Суммарные нормы по продукту среди всех блюд меню (включая стоп-лист).
	totals := map[string]float64{}
	recipes := make(map[string]map[string]float64, len(in.Dishes))
	for _, d := range in.Dishes {
		r := map[string]float64{}
		for _, it := range d.Recipe {
			if it.Quantity > 0 {
				r[it.ProductID] += float64(it.Quantity)
			}
		}
		recipes[d.ID] = r
		for p, q := range r {
			totals[p] += q
		}
	}

	type cand struct {
		offer  hotoffers.Offer
		expire time.Time
	}
	var cands []cand
	var excluded []hotoffers.Excluded
	seen := map[string]bool{}
	for _, d := range in.Dishes {
		if seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		r := recipes[d.ID]
		pids := make([]string, 0, len(r))
		for p := range r {
			pids = append(pids, p)
		}
		sort.Strings(pids)

		var prio float64
		type contrib struct {
			pid string
			val float64
		}
		var cs []contrib
		for _, p := range pids {
			n, ok := needs[p]
			if !ok || totals[p] <= 0 {
				continue
			}
			v := n.need * r[p] / totals[p]
			if v <= 0 {
				continue
			}
			prio += v
			cs = append(cs, contrib{p, v})
		}
		prio = round6(prio)
		if prio <= 0 {
			continue
		}
		if in.Stopped[d.ID] {
			excluded = append(excluded, hotoffers.Excluded{DishID: d.ID, ProductID: cs[0].pid, Why: "блюдо в стоп-листе"})
			continue
		}
		sort.SliceStable(cs, func(i, j int) bool { return cs[i].val > cs[j].val })
		reasons := make([]hotoffers.Reason, 0, len(cs))
		for _, c := range cs {
			n := needs[c.pid]
			reasons = append(reasons, hotoffers.Reason{
				ProductID:        c.pid,
				BatchID:          n.batchID,
				AtRisk:           n.cum,
				ExpiresAt:        n.expires,
				WorkingHoursLeft: n.w,
			})
		}
		cands = append(cands, cand{hotoffers.Offer{DishID: d.ID, Priority: prio, Reasons: reasons}, minExpiry(reasons)})
	}

	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.offer.Priority != b.offer.Priority {
			return a.offer.Priority > b.offer.Priority
		}
		if !a.expire.Equal(b.expire) {
			return a.expire.Before(b.expire)
		}
		return a.offer.DishID < b.offer.DishID
	})
	sort.Slice(excluded, func(i, j int) bool { return excluded[i].DishID < excluded[j].DishID })

	topN := in.Config.TopN
	if topN <= 0 {
		topN = defaultTopN
	}
	if len(cands) > topN {
		cands = cands[:topN]
	}
	offers := make([]hotoffers.Offer, len(cands))
	for i, c := range cands {
		offers[i] = c.offer
	}
	return offers, excluded
}

// productNeed — требуемый темп по продукту и его опорная партия.
type productNeed struct {
	need    float64 // копеек в рабочий час
	batchID int64
	cum     domain.Qty
	expires time.Time
	w       float64
}

func productNeeds(in hotoffers.Input) map[string]productNeed {
	byProduct := map[string][]domain.Batch{}
	for _, b := range in.Batches {
		if b.Remaining <= 0 || !in.Now.Before(b.ExpiresAt) {
			continue
		}
		byProduct[b.ProductID] = append(byProduct[b.ProductID], b)
	}
	res := map[string]productNeed{}
	for p, bs := range byProduct {
		price := float64(in.Price[p]) / 1000
		if price <= 0 {
			continue
		}
		sort.Slice(bs, func(i, j int) bool {
			if !bs[i].ExpiresAt.Equal(bs[j].ExpiresAt) {
				return bs[i].ExpiresAt.Before(bs[j].ExpiresAt)
			}
			return bs[i].ID < bs[j].ID
		})
		var cum domain.Qty
		var best productNeed
		found := false
		for _, b := range bs {
			w := workingHours(in.Now, b.ExpiresAt, in.Schedule)
			if w <= 0 || w < in.Config.WMin {
				continue // обречённая партия
			}
			cum += b.Remaining
			rate := price * float64(cum) / w
			if !found || rate > best.need {
				best = productNeed{need: rate, batchID: b.ID, cum: cum, expires: b.ExpiresAt, w: w}
				found = true
			}
		}
		if found {
			res[p] = best
		}
	}
	return res
}

func minExpiry(rs []hotoffers.Reason) time.Time {
	m := rs[0].ExpiresAt
	for _, r := range rs[1:] {
		if r.ExpiresAt.Before(m) {
			m = r.ExpiresAt
		}
	}
	return m
}

func round6(x float64) float64 {
	v, err := strconv.ParseFloat(strconv.FormatFloat(x, 'g', 6, 64), 64)
	if err != nil {
		return x
	}
	return v
}

// workingHours — W(from, to): рабочие часы ресторана в интервале. Локальная
// чистая копия, пока domain.WorkingHours — заглушка контракта.
func workingHours(from, to time.Time, sch domain.Schedule) float64 {
	if !to.After(from) {
		return 0
	}
	loc := sch.Location
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := from.In(loc).Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, loc)
	var total time.Duration
	for ; day.Before(to); day = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc) {
		open, closeAt := day.Add(sch.OpenAt), day.Add(sch.CloseAt)
		if open.Before(from) {
			open = from
		}
		if closeAt.After(to) {
			closeAt = to
		}
		if closeAt.After(open) {
			total += closeAt.Sub(open)
		}
	}
	return total.Hours()
}
