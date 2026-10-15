package domain

import (
	"math"
	"sort"
	"time"
)

// BacktestMinDays — меньше дней с историей оценку точности не считаем: на двух днях процент ничего не значит.
const BacktestMinDays = 3

// BacktestInput — вход DemandBacktest: те же параметры и история, что у DishDemand.
type BacktestInput struct {
	Now        time.Time
	Since      time.Time // начало истории стенда; нулевое — не ограничивать
	Days       int       // сколько прошлых суток проверяем
	WindowDays float64
	PriorDays  float64
	Priors     map[string]float64
	History    []OrderedDish
	Jumps      []TimeJump
	// RejectedCap — как в DemandInput; 0 — DefaultRejectedCap.
	RejectedCap int64
}

// DishAccuracy — точность прогноза по одному блюду.
type DishAccuracy struct {
	DishID          string
	ActualPerDay    float64 // среднее фактическое за проверенные сутки
	PredictedPerDay float64
	WAPEPct         float64 // с обучением: прогноз DishDemand на начало суток
	BaselineWAPEPct float64 // без обучения: только априор
}

// BacktestResult — итог проверки.
type BacktestResult struct {
	DaysUsed        int
	EnoughData      bool
	WAPEPct         float64
	BaselineWAPEPct float64
	Dishes          []DishAccuracy
}

// DemandBacktest — «как бы мы угадали вчера»: для каждых прошедших суток [Now−k·24 ч, Now−(k−1)·24 ч] берёт λ,
// какой её посчитал бы движок на начало суток (только заказы до этого момента), и сравнивает с фактом суток.
// WAPE = Σ|прогноз − факт| / Σ факт; базовая линия — априор без обучения. Чистая функция: без БД и часов.
// Сутки, пересекающиеся с прыжком времени, и сутки до начала истории пропускаются.
func DemandBacktest(in BacktestInput) (BacktestResult, error) {
	rejectedCap := in.RejectedCap
	if rejectedCap <= 0 {
		rejectedCap = DefaultRejectedCap
	}
	type acc struct{ actual, abs, base, pred float64 }
	byDish := make(map[string]*acc, len(in.Priors))
	for id := range in.Priors {
		byDish[id] = &acc{}
	}
	res := BacktestResult{}
	for k := 1; k <= in.Days; k++ {
		end := in.Now.Add(-time.Duration(k-1) * day)
		start := end.Add(-day)
		if !in.Since.IsZero() && start.Before(in.Since) {
			continue
		}
		skip := false
		for _, j := range in.Jumps {
			if j.To.After(j.From) && j.From.Before(end) && j.To.After(start) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		pred, err := DishDemand(DemandInput{Now: start, Since: in.Since, WindowDays: in.WindowDays, PriorDays: in.PriorDays,
			Priors: in.Priors, History: in.History, Jumps: in.Jumps, RejectedCap: in.RejectedCap})
		if err != nil {
			return BacktestResult{}, err
		}
		actual := make(map[string]int64, len(in.Priors))
		for _, o := range in.History {
			if o.At.After(start) && !o.At.After(end) && o.Portions > 0 {
				p := o.Portions
				if o.Rejected && p > rejectedCap {
					p = rejectedCap
				}
				actual[o.DishID] += p
			}
		}
		res.DaysUsed++
		for id, prior := range in.Priors {
			a := float64(actual[id])
			d := byDish[id]
			d.actual += a
			d.pred += pred[id]
			d.abs += math.Abs(pred[id] - a)
			d.base += math.Abs(prior - a)
		}
	}
	var totA, totAbs, totBase float64
	ids := make([]string, 0, len(byDish))
	for id := range byDish {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	n := float64(res.DaysUsed)
	for _, id := range ids {
		d := byDish[id]
		totA += d.actual
		totAbs += d.abs
		totBase += d.base
		row := DishAccuracy{DishID: id}
		if res.DaysUsed > 0 {
			row.ActualPerDay, row.PredictedPerDay = d.actual/n, d.pred/n
		}
		if d.actual > 0 {
			row.WAPEPct, row.BaselineWAPEPct = round1(d.abs/d.actual*100), round1(d.base/d.actual*100)
		}
		res.Dishes = append(res.Dishes, row)
	}
	res.EnoughData = res.DaysUsed >= BacktestMinDays && totA > 0
	if res.EnoughData {
		res.WAPEPct, res.BaselineWAPEPct = round1(totAbs/totA*100), round1(totBase/totA*100)
	}
	return res, nil
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
