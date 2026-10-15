package domain

import (
	"testing"
	"time"
)

var btNow = time.Date(2026, 10, 16, 11, 0, 0, 0, time.UTC)

// history — по portions порций блюда в середине каждых из days прошлых суток.
func history(dish string, days int, portions int64) []OrderedDish {
	var h []OrderedDish
	for k := 1; k <= days; k++ {
		h = append(h, OrderedDish{At: btNow.Add(-time.Duration(k)*day + 12*time.Hour), DishID: dish, Portions: portions})
	}
	return h
}

func backtest(t *testing.T, in BacktestInput) BacktestResult {
	t.Helper()
	if in.Now.IsZero() {
		in.Now = btNow
	}
	if in.WindowDays == 0 {
		in.WindowDays = 7
	}
	if in.PriorDays == 0 {
		in.PriorDays = 2
	}
	r, err := DemandBacktest(in)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Спрос ровно такой, как априор: модель и базовая линия не ошибаются.
func TestBacktestPerfectPrior(t *testing.T) {
	r := backtest(t, BacktestInput{Days: 7, Priors: map[string]float64{"soup": 10}, History: history("soup", 14, 10)})
	if !r.EnoughData || r.DaysUsed != 7 || r.WAPEPct != 0 || r.BaselineWAPEPct != 0 {
		t.Fatalf("%+v", r)
	}
}

// Априор вдвое занижен, спрос стабильный: обучение быстро догоняет, базовая линия ошибается на 50 %.
func TestBacktestLearningBeatsPrior(t *testing.T) {
	r := backtest(t, BacktestInput{Days: 10, Priors: map[string]float64{"soup": 5}, History: history("soup", 20, 10)})
	if !r.EnoughData {
		t.Fatalf("%+v", r)
	}
	if r.BaselineWAPEPct != 50 {
		t.Fatalf("база %v", r.BaselineWAPEPct)
	}
	if r.WAPEPct >= r.BaselineWAPEPct {
		t.Fatalf("обучение не лучше априора: %v ≥ %v", r.WAPEPct, r.BaselineWAPEPct)
	}
	if len(r.Dishes) != 1 || r.Dishes[0].ActualPerDay != 10 || r.Dishes[0].WAPEPct != r.WAPEPct {
		t.Fatalf("%+v", r.Dishes)
	}
}

func TestBacktestNotEnoughData(t *testing.T) {
	// Начало истории — полтора суток назад: проверять почти нечего.
	r := backtest(t, BacktestInput{Days: 7, Since: btNow.Add(-36 * time.Hour), Priors: map[string]float64{"soup": 10}, History: history("soup", 1, 10)})
	if r.EnoughData || r.DaysUsed >= BacktestMinDays {
		t.Fatalf("%+v", r)
	}
	// Заказов нет вовсе: Σ факт = 0.
	r = backtest(t, BacktestInput{Days: 7, Priors: map[string]float64{"soup": 10}})
	if r.EnoughData || r.WAPEPct != 0 {
		t.Fatalf("%+v", r)
	}
}

// Сутки с прыжком времени не проверяются: заказов в пропущенном интервале быть не могло.
func TestBacktestSkipsJumpDays(t *testing.T) {
	jump := TimeJump{From: btNow.Add(-60 * time.Hour), To: btNow.Add(-30 * time.Hour)}
	with := backtest(t, BacktestInput{Days: 7, Priors: map[string]float64{"soup": 10}, History: history("soup", 14, 10), Jumps: []TimeJump{jump}})
	without := backtest(t, BacktestInput{Days: 7, Priors: map[string]float64{"soup": 10}, History: history("soup", 14, 10)})
	if with.DaysUsed >= without.DaysUsed {
		t.Fatalf("прыжок не исключил сутки: %d против %d", with.DaysUsed, without.DaysUsed)
	}
}

func TestBacktestDeterministicAndRejectedCap(t *testing.T) {
	h := append(history("soup", 14, 10), OrderedDish{At: btNow.Add(-12 * time.Hour), DishID: "steak", Portions: 9999, Rejected: true})
	in := BacktestInput{Days: 7, Priors: map[string]float64{"soup": 10, "steak": 4}, History: h}
	a, b := backtest(t, in), backtest(t, in)
	if a.WAPEPct != b.WAPEPct || len(a.Dishes) != 2 || a.Dishes[0].DishID != "soup" || a.Dishes[1].DishID != "steak" {
		t.Fatalf("%+v %+v", a, b)
	}
	// Один отклонённый заказ на 9999 порций даёт не больше RejectedCap порций за сутки.
	if got := a.Dishes[1].ActualPerDay; got > float64(DefaultRejectedCap)/float64(a.DaysUsed)+1e-9 {
		t.Fatalf("отклонённый заказ раздул факт: %v", got)
	}
}

func TestBacktestValidation(t *testing.T) {
	_, err := DemandBacktest(BacktestInput{Now: btNow, Days: 3, WindowDays: 7, PriorDays: 0, Priors: map[string]float64{"a": 1}, History: history("a", 5, 1)})
	if err == nil {
		t.Fatal("n0 = 0 должен быть ошибкой")
	}
}

// Прыжок часов растягивает окно оценки назад: с полной историей прогноз по стабильному спросу точен
// (регрессия на обрезанную историю: заказы до прыжка не должны пропадать из окна).
func TestBacktestWindowCrossesJumpWithFullHistory(t *testing.T) {
	jump := TimeJump{From: btNow.Add(-10 * day), To: btNow.Add(-5*day + time.Hour)}
	// В пропущенном прыжком интервале заказов быть не могло.
	var h []OrderedDish
	for _, o := range history("soup", 40, 10) {
		if !(o.At.After(jump.From) && o.At.Before(jump.To)) {
			h = append(h, o)
		}
	}
	r := backtest(t, BacktestInput{Days: 3, WindowDays: 14, PriorDays: 3, Priors: map[string]float64{"soup": 10}, History: h, Jumps: []TimeJump{jump}})
	if r.WAPEPct != 0 {
		t.Fatalf("WAPE %v при стабильном спросе, равном априору: %+v", r.WAPEPct, r)
	}
}
