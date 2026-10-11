package simulator

import "math"

// Пороги правила победителя (solution.md, 2.7).
const (
	maxRejectPct = 1.0  // отказы не больше +1 % заказов к C0
	tieShare     = 0.05 // ничья, если разница меньше 5 % списаний C0 в сценарии
	winScenarios = 4    // победа — лучше в ≥ 4 сценариях из 5
)

// Outcome — исход сравнения R и Bg в одном сценарии основной точки.
type Outcome string

// Исходы.
const (
	OutcomeTie Outcome = "tie"
	OutcomeR   Outcome = "r"  // R лучше
	OutcomeBg  Outcome = "bg" // Bg лучше
)

// ScenarioOutcome — исход по сценарию.
type ScenarioOutcome struct {
	Scenario string  `json:"scenario"`
	Outcome  Outcome `json:"outcome"`
	Why      string  `json:"why"`
}

// BenchResult — тело GET /api/analytics/bench (solution.md, 7.4).
type BenchResult struct {
	Available                  bool    `json:"available"`
	Runs                       int     `json:"runs"`
	MainAlgo                   string  `json:"main_algo,omitempty"` // "a" — R, "b" — Bg
	WasteReductionVsRandomPct  float64 `json:"waste_reduction_vs_random_pct"`
	WasteReductionVsNearestPct float64 `json:"waste_reduction_vs_nearest_pct"`
}

// Verdict — итог правила победителя.
type Verdict struct {
	Main      Algo              `json:"main"`      // основной алгоритм: R или Bg
	Claims    bool              `json:"claims"`    // можно ли утверждать результат сравнения
	Result    Outcome           `json:"result"`    // итог по сценариям до ограничения
	PassR     bool              `json:"pass_r"`    // R прошёл ограничение по отказам
	PassBg    bool              `json:"pass_bg"`   // Bg прошёл ограничение по отказам
	Scenarios []ScenarioOutcome `json:"scenarios"` // исходы по сценариям основной точки
	Reason    string            `json:"reason"`    // как принято решение
	Analytics BenchResult       `json:"analytics"` // цифры для экрана и README
}

// Winner применяет правило победителя из solution.md, 2.7, к отчёту.
//
//   - Ограничение: отказы algo не больше +1 % заказов к C0 во всех клетках обеих
//     моделей; нарушитель основным не становится.
//   - По сценариям основной точки (M2, основное u): ничья, если 95 % интервал
//     парной разности Bg−R включает 0 либо разница меньше 5 % списаний C0;
//     победа — лучше в ≥ 4 сценариях и не хуже ни в одном; иначе ничья.
//   - Ничья: основным становится прошедший ограничение; прошли оба — R; не прошёл
//     ни один — R без утверждений о сравнении.
//
// Победа алгоритма, не прошедшего ограничение, ему не засчитывается: основным
// идёт соперник, если он прошёл, иначе R без утверждений.
func Winner(rep Report) Verdict {
	v := Verdict{Main: AlgoR, Result: OutcomeTie, PassR: true, PassBg: true}
	for _, c := range rep.Cells {
		c0 := c.Algos[AlgoC0]
		if !withinRejectLimit(c.Algos[AlgoR], c0) {
			v.PassR = false
		}
		if !withinRejectLimit(c.Algos[AlgoBg], c0) {
			v.PassBg = false
		}
	}
	var main []Cell
	for _, c := range rep.Cells {
		if c.Main {
			main = append(main, c)
		}
	}
	if len(main) == 0 {
		v.PassR, v.PassBg = false, false
		v.Reason = "в отчёте нет основной точки: основным идёт R, утверждений о сравнении нет"
		return v
	}
	var nR, nBg int
	for _, c := range main {
		o := compare(c)
		v.Scenarios = append(v.Scenarios, o)
		switch o.Outcome {
		case OutcomeR:
			nR++
		case OutcomeBg:
			nBg++
		}
	}
	switch {
	case nBg >= winScenarios && nR == 0:
		v.Result = OutcomeBg
	case nR >= winScenarios && nBg == 0:
		v.Result = OutcomeR
	}

	switch {
	case !v.PassR && !v.PassBg:
		v.Main, v.Reason = AlgoR, "ни один алгоритм не прошёл ограничение по отказам: основным идёт R, утверждений о сравнении нет"
	case v.Result == OutcomeBg && v.PassBg:
		v.Main, v.Claims, v.Reason = AlgoBg, true, "Bg победил в сценариях и прошёл ограничение по отказам"
	case v.Result == OutcomeR && v.PassR:
		v.Main, v.Claims, v.Reason = AlgoR, true, "R победил в сценариях и прошёл ограничение по отказам"
	case v.Result == OutcomeBg: // победитель не прошёл ограничение, значит прошёл R
		v.Main, v.Claims, v.Reason = AlgoR, true, "Bg победил, но нарушил ограничение по отказам: основным идёт R"
	case v.Result == OutcomeR: // победитель не прошёл ограничение, значит прошёл Bg
		v.Main, v.Claims, v.Reason = AlgoBg, true, "R победил, но нарушил ограничение по отказам: основным идёт Bg"
	case v.PassR: // ничья; прошёл R либо оба
		v.Main, v.Claims, v.Reason = AlgoR, true, "ничья: основным идёт R"
	default: // ничья, прошёл только Bg
		v.Main, v.Claims, v.Reason = AlgoBg, true, "ничья, ограничение прошёл только Bg"
	}
	if v.Claims {
		v.Analytics = analytics(main, v.Main)
	}
	return v
}

func withinRejectLimit(a, c0 Agg) bool {
	return float64(a.Rejected-c0.Rejected)*100 <= maxRejectPct*float64(c0.Orders)
}

// compare — исход по сценарию: интервал разности Bg−R и порог 5 % списаний C0.
func compare(c Cell) ScenarioOutcome {
	o := ScenarioOutcome{Scenario: c.Scenario, Outcome: OutcomeTie}
	c0 := 0.0
	if c.Seeds > 0 {
		c0 = float64(c.Algos[AlgoC0].Waste) / float64(c.Seeds)
	}
	d := c.DiffBgR
	switch {
	case c.Seeds < 2 || (d.Lo <= 0 && d.Hi >= 0):
		o.Why = "95 % интервал разности включает 0"
	case math.Abs(d.Mean) < tieShare*c0:
		o.Why = "разница меньше 5 % списаний C0"
	case d.Mean < 0:
		o.Outcome, o.Why = OutcomeBg, "Bg списывает меньше"
	default:
		o.Outcome, o.Why = OutcomeR, "R списывает меньше"
	}
	return o
}

// analytics — снижение списаний основного алгоритма относительно RND и C1
// по сумме сценариев основной точки. База порогов правила — C0, база эффекта — RND.
func analytics(main []Cell, algo Algo) BenchResult {
	var w, rnd, c1 int64
	for _, c := range main {
		w += c.Algos[algo].Waste
		rnd += c.Algos[AlgoRND].Waste
		c1 += c.Algos[AlgoC1].Waste
	}
	r := BenchResult{Available: true, Runs: main[0].Seeds, MainAlgo: "a"}
	if algo == AlgoBg {
		r.MainAlgo = "b"
	}
	r.WasteReductionVsRandomPct = reduction(w, rnd)
	r.WasteReductionVsNearestPct = reduction(w, c1)
	return r
}

func reduction(w, base int64) float64 {
	if base <= 0 {
		return 0
	}
	return math.Round((1-float64(w)/float64(base))*1000) / 10
}
