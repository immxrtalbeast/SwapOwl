package simulator

import "testing"

// scen задаёт основную точку одного сценария: разность Bg−R и её интервал.
type scen struct{ mean, lo, hi float64 }

var (
	bgWins = scen{-100, -150, -50} // Bg списывает меньше, вне порога 5 %
	rWins  = scen{100, 50, 150}
	tieCI  = scen{-100, -200, 50} // интервал включает 0
	tieThr = scen{-10, -15, -5}   // разница меньше 5 % списаний C0 (C0 = 1000 на seed)
)

// report строит отчёт: пять основных клеток M2 и по желанию клетку M1 для ограничения.
func report(sc [5]scen, rejR, rejBg int) Report {
	rep := Report{Days: 7, Seeds: 10}
	for i, s := range sc {
		rep.Cells = append(rep.Cells, Cell{
			Scenario: string(rune('a' + i)), Model: ModelM2, U: 1.5, Main: true, Seeds: 10,
			Algos: map[Algo]Agg{
				AlgoC0:  {Waste: 10000, Orders: 1000},
				AlgoR:   {Waste: 8000, Orders: 1000, Rejected: rejR},
				AlgoBg:  {Waste: 8000, Orders: 1000, Rejected: rejBg},
				AlgoRND: {Waste: 10000}, AlgoC1: {Waste: 9000},
			},
			DiffBgR: Stat{Mean: s.mean, Lo: s.lo, Hi: s.hi},
		})
	}
	return rep
}

func TestWinner(t *testing.T) {
	five := func(s scen) [5]scen { return [5]scen{s, s, s, s, s} }
	mix := func(a scen, n int, b scen) [5]scen {
		var out [5]scen
		for i := range out {
			out[i] = b
			if i < n {
				out[i] = a
			}
		}
		return out
	}
	cases := []struct {
		name        string
		sc          [5]scen
		rejR, rejBg int
		main        Algo
		claims      bool
		result      Outcome
	}{
		{"Bg побеждает во всех пяти", five(bgWins), 0, 0, AlgoBg, true, OutcomeBg},
		{"Bg побеждает в четырёх, в пятом ничья", mix(bgWins, 4, tieCI), 0, 0, AlgoBg, true, OutcomeBg},
		{"Bg побеждает в трёх — ничья, оба прошли, R", mix(bgWins, 3, tieCI), 0, 0, AlgoR, true, OutcomeTie},
		{"Bg побеждает в четырёх, но хуже в одном — ничья", [5]scen{bgWins, bgWins, bgWins, bgWins, rWins}, 0, 0, AlgoR, true, OutcomeTie},
		{"R побеждает в четырёх", mix(rWins, 4, tieThr), 0, 0, AlgoR, true, OutcomeR},
		{"ничья по порогу 5 % списаний C0", five(tieThr), 0, 0, AlgoR, true, OutcomeTie},
		{"ничья по интервалу", five(tieCI), 0, 0, AlgoR, true, OutcomeTie},
		{"ничья, прошёл только Bg", five(tieCI), 11, 0, AlgoBg, true, OutcomeTie},
		{"ничья, прошёл только R", five(tieCI), 0, 11, AlgoR, true, OutcomeTie},
		{"Bg победил, но нарушил отказы — R", five(bgWins), 0, 11, AlgoR, true, OutcomeBg},
		{"R победил, но нарушил отказы — Bg", five(rWins), 11, 0, AlgoBg, true, OutcomeR},
		{"отказы ровно +1 % допустимы", five(bgWins), 10, 10, AlgoBg, true, OutcomeBg},
		{"оба нарушили — R без утверждений", five(bgWins), 11, 11, AlgoR, false, OutcomeBg},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := Winner(report(c.sc, c.rejR, c.rejBg))
			if v.Main != c.main || v.Claims != c.claims || v.Result != c.result {
				t.Fatalf("main=%s claims=%v result=%s (%s), ждали %s %v %s", v.Main, v.Claims, v.Result, v.Reason, c.main, c.claims, c.result)
			}
			if v.Analytics.Available != c.claims {
				t.Fatalf("analytics.available=%v при claims=%v", v.Analytics.Available, c.claims)
			}
		})
	}
}

func TestWinnerRejectsInAnyCell(t *testing.T) {
	rep := report([5]scen{bgWins, bgWins, bgWins, bgWins, bgWins}, 0, 0)
	// Не основная клетка модели M1: нарушение там тоже лишает права стать основным.
	rep.Cells = append(rep.Cells, Cell{
		Scenario: "a", Model: ModelM1, U: 2, Seeds: 10,
		Algos: map[Algo]Agg{AlgoC0: {Orders: 1000}, AlgoBg: {Orders: 1000, Rejected: 11}, AlgoR: {Orders: 1000}},
	})
	v := Winner(rep)
	if v.PassBg || !v.PassR || v.Main != AlgoR || !v.Claims {
		t.Fatalf("%+v", v)
	}
}

func TestWinnerNoMainPoint(t *testing.T) {
	v := Winner(Report{})
	if v.Main != AlgoR || v.Claims || v.Analytics.Available {
		t.Fatalf("%+v", v)
	}
}

func TestWinnerAnalytics(t *testing.T) {
	v := Winner(report([5]scen{bgWins, bgWins, bgWins, bgWins, bgWins}, 0, 0))
	a := v.Analytics
	// Bg: 5×8000 против RND 5×10000 и C1 5×9000.
	if !a.Available || a.Runs != 10 || a.MainAlgo != "b" || a.WasteReductionVsRandomPct != 20 || a.WasteReductionVsNearestPct != 11.1 {
		t.Fatalf("%+v", a)
	}
	if r := Winner(report([5]scen{rWins, rWins, rWins, rWins, rWins}, 0, 0)); r.Analytics.MainAlgo != "a" {
		t.Fatalf("%+v", r.Analytics)
	}
}
