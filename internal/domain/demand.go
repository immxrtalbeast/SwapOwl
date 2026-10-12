package domain

import (
	"fmt"
	"sort"
	"time"
)

const day = 24 * time.Hour

// TimeJump — интервал, пропущенный прыжком времени стенда: заказов в нём быть не могло.
type TimeJump struct {
	From, To time.Time
}

// OrderedDish — запрошенные порции блюда в принятом или отклонённом заказе.
type OrderedDish struct {
	At       time.Time
	DishID   string
	Portions int64
	Rejected bool // заказ отклонён: в спрос входит не больше RejectedCap порций
}

// DefaultRejectedCap — сколько порций блюда из одного отклонённого заказа входит в оценку спроса (2.6).
// Отклонённый заказ — сигнал неудовлетворённого спроса, но не мерило его величины: столик редко
// заказывает больше пяти одинаковых блюд, а заказ на 9999 порций иначе раздувал λ и предложение
// поставок до сотен килограммов (FS-153). Принятые заказы не ограничиваются.
const DefaultRejectedCap = 5

// DemandInput — вход DishDemand. Все параметры приходят аргументами.
type DemandInput struct {
	Now        time.Time
	Since      time.Time // начало истории стенда; нулевое — не ограничивать
	WindowDays float64   // N: длина окна в активных сутках
	PriorDays  float64   // n0 > 0
	Priors     map[string]float64
	History    []OrderedDish
	Jumps      []TimeJump
	// RejectedCap — вклад одного отклонённого заказа по блюду, порций; 0 — DefaultRejectedCap.
	RejectedCap int64
}

// activeWindow возвращает начало окна и число активных суток в нём. Окно идёт назад
// от now, пока не наберёт windowDays суток без пропущенных прыжками интервалов,
// поэтому прыжок без заказов не меняет оценку. Окно не заходит раньше since.
func activeWindow(now, since time.Time, windowDays float64, jumps []TimeJump) (time.Time, float64) {
	js := make([]TimeJump, 0, len(jumps))
	for _, j := range jumps {
		if j.To.After(j.From) {
			js = append(js, j)
		}
	}
	sort.Slice(js, func(a, b int) bool { return js[a].To.After(js[b].To) })

	remaining := time.Duration(windowDays * float64(day))
	var active time.Duration
	pos := now
	// take забирает активный отрезок [lo, pos]; true — окно закончилось.
	take := func(lo time.Time) (time.Time, bool) {
		floored := false
		if !since.IsZero() && lo.Before(since) {
			lo, floored = since, true
		}
		seg := pos.Sub(lo)
		if seg < 0 {
			seg = 0
		}
		if seg >= remaining {
			active += remaining
			return pos.Add(-remaining), true
		}
		active += seg
		remaining -= seg
		pos = lo
		return lo, floored
	}
	for _, j := range js {
		to := j.To
		if to.After(pos) {
			to = pos
		}
		if !to.After(j.From) {
			continue
		}
		if start, done := take(to); done {
			return start, active.Hours() / 24
		}
		pos = j.From
	}
	start, _ := take(pos.Add(-remaining))
	return start, active.Hours() / 24
}

// DishDemand — λ_d, порций в день: (prior_d·n0 + запрошено_d) / (n0 + активные_дни).
// Отклонённый заказ входит в запрошено_d не больше чем RejectedCap порциями блюда.
// Возвращает оценку для каждого блюда из Priors. История вне окна и блюда без
// априора игнорируются.
func DishDemand(in DemandInput) (map[string]float64, error) {
	if in.PriorDays <= 0 {
		return nil, fmt.Errorf("%w: n0 must be > 0", ErrValidation)
	}
	if in.WindowDays <= 0 {
		return nil, fmt.Errorf("%w: window must be > 0", ErrValidation)
	}
	start, activeDays := activeWindow(in.Now, in.Since, in.WindowDays, in.Jumps)
	rejectedCap := in.RejectedCap
	if rejectedCap <= 0 {
		rejectedCap = DefaultRejectedCap
	}
	requested := make(map[string]int64, len(in.Priors))
	for _, o := range in.History {
		if o.At.After(start) && !o.At.After(in.Now) && o.Portions > 0 {
			p := o.Portions
			if o.Rejected && p > rejectedCap {
				p = rejectedCap
			}
			requested[o.DishID] += p
		}
	}
	out := make(map[string]float64, len(in.Priors))
	for id, prior := range in.Priors {
		out[id] = (prior*in.PriorDays + float64(requested[id])) / (in.PriorDays + activeDays)
	}
	return out, nil
}

// ProductDemandPerDay — rate_p_день = Σ_d λ_d · норма_dp, базовых единиц в день.
func ProductDemandPerDay(dishes []Dish, lambda map[string]float64) map[string]float64 {
	out := make(map[string]float64)
	for _, d := range dishes {
		l := lambda[d.ID]
		for _, it := range d.Recipe {
			out[it.ProductID] += l * float64(it.Quantity)
		}
	}
	return out
}

// RatePerHour — rate_p_час = rate_p_день / W_день, где W_день = CloseAt − OpenAt в часах;
// при заданном sch.Days W_день — средний рабочий день недели (сумма длин интервалов / 7).
func RatePerHour(perDay float64, sch Schedule) float64 {
	h := (sch.CloseAt - sch.OpenAt).Hours()
	if len(sch.Days) > 0 {
		var total time.Duration
		for _, ivs := range sch.Days {
			for _, iv := range ivs {
				total += iv.Close - iv.Open
			}
		}
		h = total.Hours() / 7
	}
	if h <= 0 {
		return 0
	}
	return perDay / h
}
