package main

import (
	"fmt"
	"io"
	"math"
	"sync"
)

type report struct {
	Profile    profile
	Days, Runs int
	Seed0      int64
	Variants   []variant
	Res        [][]result // [режим][прогон]
	Orders     float64    // среднее число заказов в сутки
}

func compute(days, runs int, seed0 int64, acc []int, pf profile) (*report, error) {
	t0, loc, err := start()
	if err != nil {
		return nil, err
	}
	vs := variants(acc)
	rep := &report{Profile: pf, Days: days, Runs: runs, Seed0: seed0, Variants: vs, Res: make([][]result, len(vs))}
	for i := range vs {
		rep.Res[i] = make([]result, runs)
	}
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		orders   int
	)
	sem := make(chan struct{}, 8)
	for k := 0; k < runs; k++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(k int) {
			defer wg.Done()
			defer func() { <-sem }()
			s := seed0 + int64(k)
			var err error
			chs := genChecks(s, days, pf, t0, loc)
			for i := 0; err == nil && i < len(vs); i++ {
				var r result
				r, err = run(vs[i], s, days, pf, chs, t0, loc)
				mu.Lock()
				rep.Res[i][k] = r
				if i == 0 {
					orders += r.Orders
				}
				mu.Unlock()
			}
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(k)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	rep.Orders = float64(orders) / float64(runs*days)
	return rep, nil
}

// meanSD — среднее и выборочное стандартное отклонение.
func meanSD(x []float64) (m, sd float64) {
	for _, v := range x {
		m += v
	}
	m /= float64(len(x))
	if len(x) < 2 {
		return m, 0
	}
	for _, v := range x {
		sd += (v - m) * (v - m)
	}
	return m, math.Sqrt(sd / float64(len(x)-1))
}

func column(rs []result, f func(result) float64) []float64 {
	out := make([]float64, len(rs))
	for i, r := range rs {
		out[i] = f(r)
	}
	return out
}

func rub(r result) float64 { return float64(r.Writeoff) / 100 }

func (rep *report) write(w io.Writer) {
	pf := rep.Profile
	fmt.Fprintf(w, "Прогонов: %d, сиды: %d…%d, суток в прогоне: %d, заказов в сутки (среднее): %.1f\n", rep.Runs, rep.Seed0, rep.Seed0+int64(rep.Runs)-1, rep.Days, rep.Orders)
	fmt.Fprintf(w, "Профиль: %d чеков в сутки; закупка до уровня «интервал между поставками + %.1f сут.» × (1 + %.0f %%) ± %.0f %% ошибки прогноза; %d %% поставки с коротким сроком (×%.1f)\n\n",
		pf.Checks, pf.CoverDays, 100*pf.Reserve, 100*pf.ForecastErr, pf.ShortShare, pf.ShortFactor)

	fmt.Fprintln(w, "| Режим | Выручка, ₽ (среднее) | Себестоимость проданного, ₽ (среднее) | Себестоимость, % выручки | Списания, ₽ (среднее ± σ) | Списания, % себестоимости (среднее ± σ) |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|")
	for i, v := range rep.Variants {
		rs := rep.Res[i]
		rv, _ := meanSD(column(rs, func(r result) float64 { return float64(r.Revenue) / 100 }))
		cg, _ := meanSD(column(rs, func(r result) float64 { return float64(r.COGS) / 100 }))
		wm, ws := meanSD(column(rs, rub))
		pm, ps := meanSD(column(rs, func(r result) float64 { return 100 * float64(r.Writeoff) / float64(r.COGS) }))
		fmt.Fprintf(w, "| %s | %.0f | %.0f | %.1f | %.0f ± %.0f | %.1f ± %.1f |\n", v.Name, rv, cg, 100*cg/rv, wm, ws, pm, ps)
	}

	fmt.Fprintln(w, "\n| Режим | Блюдо-часов в стоп-листе (среднее ± σ) | Продукт закончился, раз (среднее ± σ) | Отказов в заказе, шт. (среднее) | Горячих блюд взято, шт. (среднее) |")
	fmt.Fprintln(w, "|---|---|---|---|---|")
	for i, v := range rep.Variants {
		rs := rep.Res[i]
		hm, hs := meanSD(column(rs, func(r result) float64 { return r.StopDishHours }))
		sm, ss := meanSD(column(rs, func(r result) float64 { return float64(r.StockOuts) }))
		rj, _ := meanSD(column(rs, func(r result) float64 { return float64(r.Rejected) }))
		ht, _ := meanSD(column(rs, func(r result) float64 { return float64(r.HotTaken) }))
		fmt.Fprintf(w, "| %s | %.0f ± %.0f | %.1f ± %.1f | %.1f | %.1f |\n", v.Name, hm, hs, sm, ss, rj, ht)
	}

	fmt.Fprintln(w, "\nПарная разница списаний на одном сиде (₽ за период), среднее и 95 % интервал среднего:")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Сравнение | Разница, ₽ | 95 % интервал | Вывод |")
	fmt.Fprintln(w, "|---|---|---|---|")
	pair := func(a, b int) {
		d := make([]float64, rep.Runs)
		for k := range d {
			d[k] = rub(rep.Res[a][k]) - rub(rep.Res[b][k])
		}
		m, sd := meanSD(d)
		se := sd / math.Sqrt(float64(rep.Runs))
		lo, hi := m-1.96*se, m+1.96*se
		verdict := "различие больше разброса"
		if lo <= 0 && hi >= 0 {
			verdict = "разница внутри разброса"
		}
		fmt.Fprintf(w, "| %s − %s | %.0f | %.0f … %.0f | %s |\n", rep.Variants[a].Name, rep.Variants[b].Name, m, lo, hi, verdict)
	}
	pair(1, 0)
	for i := 2; i < len(rep.Variants); i++ {
		pair(i, 1)
		pair(i, 0)
	}
}
