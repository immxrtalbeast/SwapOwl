package simulator

import (
	"fmt"
	"math"
)

// SeedRow — результаты трёх вариантов на одном seed.
type SeedRow struct {
	Seed    int64  `json:"seed"`
	C0      Result `json:"c0"`
	R       Result `json:"r"`
	Bg      Result `json:"bg"`
	DiffBgR int64  `json:"diff_bg_minus_r"` // списания Bg − R, копейки; < 0 — Bg лучше
}

// Stat — среднее и 95 % интервал среднего (нормальное приближение).
type Stat struct {
	Mean float64 `json:"mean"`
	Lo   float64 `json:"ci95_lo"`
	Hi   float64 `json:"ci95_hi"`
}

// Point — результаты для одного значения u.
type Point struct {
	U          float64   `json:"u"`
	Main       bool      `json:"main"`
	Rows       []SeedRow `json:"rows"`
	TotalC0    int64     `json:"total_c0"`
	TotalR     int64     `json:"total_r"`
	TotalBg    int64     `json:"total_bg"`
	RejectsC0  int       `json:"rejects_c0"`
	RejectsR   int       `json:"rejects_r"`
	RejectsBg  int       `json:"rejects_bg"`
	OrdersC0   int       `json:"orders_c0"`
	DiffBgR    Stat      `json:"diff_bg_minus_r"`
	SignBgVsR  string    `json:"sign_bg_vs_r"` // "bg<r", "bg>r", "tie" (интервал включает 0)
	ReductionR float64   `json:"r_vs_c0"`      // снижение списаний R относительно C0, доля
	ReductionB float64   `json:"bg_vs_c0"`
}

// Report — итог bench. Победитель не выбирается: это отдельный тикет (этап 3).
type Report struct {
	Days   int     `json:"days"`
	Points []Point `json:"points"`
}

// Bench гонит seed'ы seed0 … seed0+n−1 для каждого u. Основная точка us[0] идёт
// первой; остальные значения — после неё.
func Bench(seed0 int64, n, days int, us []float64) (Report, error) {
	if n < 1 || days < 1 || len(us) == 0 {
		return Report{}, fmt.Errorf("simulator: нужны n ≥ 1, days ≥ 1 и хотя бы одно u")
	}
	rep := Report{Days: days}
	for i, u := range us {
		pt := Point{U: u, Main: i == 0}
		var diffs []float64
		for k := 0; k < n; k++ {
			p := Params{Seed: seed0 + int64(k), Days: days, U: u, Cfg: DefaultConfig()}
			ev, err := Events(p)
			if err != nil {
				return Report{}, err
			}
			row := SeedRow{Seed: p.Seed}
			for _, a := range Algos {
				r, err := Run(p, a, ev)
				if err != nil {
					return Report{}, err
				}
				switch a {
				case AlgoC0:
					row.C0 = r
				case AlgoR:
					row.R = r
				case AlgoBg:
					row.Bg = r
				}
			}
			row.DiffBgR = row.Bg.WriteoffCost - row.R.WriteoffCost
			diffs = append(diffs, float64(row.DiffBgR))
			pt.Rows = append(pt.Rows, row)
			pt.TotalC0 += row.C0.WriteoffCost
			pt.TotalR += row.R.WriteoffCost
			pt.TotalBg += row.Bg.WriteoffCost
			pt.RejectsC0 += row.C0.Rejected
			pt.RejectsR += row.R.Rejected
			pt.RejectsBg += row.Bg.Rejected
			pt.OrdersC0 += row.C0.Orders
		}
		pt.DiffBgR = stat(diffs)
		switch {
		case n < 2 || (pt.DiffBgR.Lo <= 0 && pt.DiffBgR.Hi >= 0):
			pt.SignBgVsR = "tie"
		case pt.DiffBgR.Mean < 0:
			pt.SignBgVsR = "bg<r"
		default:
			pt.SignBgVsR = "bg>r"
		}
		if pt.TotalC0 > 0 {
			pt.ReductionR = 1 - float64(pt.TotalR)/float64(pt.TotalC0)
			pt.ReductionB = 1 - float64(pt.TotalBg)/float64(pt.TotalC0)
		}
		rep.Points = append(rep.Points, pt)
	}
	return rep, nil
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
