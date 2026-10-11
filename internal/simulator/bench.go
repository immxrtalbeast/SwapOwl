package simulator

import (
	"fmt"
	"math"
	"sync"
)

// Agg — суммы по seed для одного варианта в клетке.
type Agg struct {
	Waste    int64 `json:"waste_cost"`    // списания по просрочке, копейки
	Received int64 `json:"received_cost"` // поступило, копейки
	WasteQty int64 `json:"waste_qty"`     // списания по количеству
	Orders   int   `json:"orders"`
	Rejected int   `json:"rejected"`
	HotLines int   `json:"hot_lines"`
}

// WasteShare — доля списанного от поступившего.
func (a Agg) WasteShare() float64 {
	if a.Received == 0 {
		return 0
	}
	return float64(a.Waste) / float64(a.Received)
}

// Stat — среднее и 95 % интервал среднего (нормальное приближение).
type Stat struct {
	Mean float64 `json:"mean"`
	Lo   float64 `json:"ci95_lo"`
	Hi   float64 `json:"ci95_hi"`
}

// Cell — сценарий × модель × u: все варианты на общих seed.
type Cell struct {
	Scenario  string       `json:"scenario"`
	Title     string       `json:"title"`
	Model     Model        `json:"model"`
	U         float64      `json:"u"`
	Main      bool         `json:"main"` // M2 при основном u
	Seeds     int          `json:"seeds"`
	Algos     map[Algo]Agg `json:"algos"`
	DiffBgR   Stat         `json:"diff_bg_minus_r"` // парная разность списаний Bg − R, копейки на seed
	SignBgVsR string       `json:"sign_bg_vs_r"`    // "bg<r", "bg>r", "tie"
}

// Report — итог bench.
type Report struct {
	Days    int     `json:"days"`
	Seeds   int     `json:"seeds"`
	Cells   []Cell  `json:"cells"`
	Verdict Verdict `json:"verdict"`
}

// Models — порядок моделей в отчёте.
var Models = []Model{ModelM2, ModelM1}

// Bench гонит seed'ы seed0 … seed0+n−1 по пяти сценариям, двум моделям и значениям u.
// Основная точка — us[0] в модели M2; остальные u проверяются на знак. Затем
// применяется правило победителя.
func Bench(seed0 int64, n, days int, us []float64) (Report, error) {
	if n < 1 || days < 1 || len(us) == 0 {
		return Report{}, fmt.Errorf("simulator: нужны n ≥ 1, days ≥ 1 и хотя бы одно u")
	}
	scs := Scenarios()
	type cellKey struct {
		m Model
		u float64
	}
	var keys []cellKey
	for _, m := range Models {
		for _, u := range us {
			keys = append(keys, cellKey{m, u})
		}
	}

	// results[сценарий][seed][клетка][вариант]
	results := make([][][]map[Algo]Result, len(scs))
	var firstErr error
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for si := range scs {
		results[si] = make([][]map[Algo]Result, n)
		for k := 0; k < n; k++ {
			wg.Add(1)
			sem <- struct{}{}
			go func(si, k int) {
				defer wg.Done()
				defer func() { <-sem }()
				out, err := benchSeed(scs[si].World, seed0+int64(k), days, func(yield func(Model, float64)) {
					for _, key := range keys {
						yield(key.m, key.u)
					}
				})
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
					return
				}
				results[si][k] = out
			}(si, k)
		}
	}
	wg.Wait()
	if firstErr != nil {
		return Report{}, firstErr
	}

	rep := Report{Days: days, Seeds: n}
	for si, sc := range scs {
		for ci, key := range keys {
			c := Cell{Scenario: sc.Name, Title: sc.Title, Model: key.m, U: key.u, Seeds: n,
				Main: key.m == ModelM2 && key.u == us[0], Algos: map[Algo]Agg{}}
			var diffs []float64
			for k := 0; k < n; k++ {
				row := results[si][k][ci]
				for _, a := range Algos {
					r := row[a]
					g := c.Algos[a]
					g.Waste += r.WriteoffCost
					g.Received += r.ReceivedCost
					g.WasteQty += r.WriteoffQty
					g.Orders += r.Orders
					g.Rejected += r.Rejected
					g.HotLines += r.HotLines
					c.Algos[a] = g
				}
				diffs = append(diffs, float64(row[AlgoBg].WriteoffCost-row[AlgoR].WriteoffCost))
			}
			c.DiffBgR = stat(diffs)
			switch {
			case n < 2 || (c.DiffBgR.Lo <= 0 && c.DiffBgR.Hi >= 0):
				c.SignBgVsR = "tie"
			case c.DiffBgR.Mean < 0:
				c.SignBgVsR = "bg<r"
			default:
				c.SignBgVsR = "bg>r"
			}
			rep.Cells = append(rep.Cells, c)
		}
	}
	rep.Verdict = Winner(rep)
	return rep, nil
}

// benchSeed прогоняет один seed одного сценария по всем клеткам; C0 считается один раз.
func benchSeed(w World, seedValue int64, days int, cells func(yield func(Model, float64))) ([]map[Algo]Result, error) {
	t0, _, err := Start()
	if err != nil {
		return nil, err
	}
	ev, err := w.Events(seedValue, t0, days)
	if err != nil {
		return nil, err
	}
	var out []map[Algo]Result
	var c0 *Result
	var rerr error
	cells(func(m Model, u float64) {
		if rerr != nil {
			return
		}
		p := Params{Seed: seedValue, Days: days, U: u, Model: m, Cfg: DefaultConfig()}
		row := map[Algo]Result{}
		for _, a := range Algos {
			if a == AlgoC0 && c0 != nil {
				row[a] = *c0
				continue
			}
			r, err := RunWorld(w, p, a, ev)
			if err != nil {
				rerr = err
				return
			}
			if a == AlgoC0 {
				c0 = &r
			}
			row[a] = r
		}
		out = append(out, row)
	})
	return out, rerr
}

func stat(x []float64) Stat {
	n := float64(len(x))
	if n == 0 {
		return Stat{}
	}
	var sum float64
	for _, v := range x {
		sum += v
	}
	mean := sum / n
	if n < 2 {
		return Stat{Mean: mean, Lo: mean, Hi: mean}
	}
	var ss float64
	for _, v := range x {
		ss += (v - mean) * (v - mean)
	}
	se := math.Sqrt(ss/(n-1)) / math.Sqrt(n)
	return Stat{Mean: mean, Lo: mean - 1.96*se, Hi: mean + 1.96*se}
}
