package b

import (
	"math/rand"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/hotoffers"
)

// Якоря К3 (критерии кейса, критерий 3, 9 баллов) на чистой функции Rank алгоритма Bg.

func indexOf(offers []hotoffers.Offer, id string) int {
	for i, o := range offers {
		if o.DishID == id {
			return i
		}
	}
	return -1
}

// Малый остаток дешёвого продукта не обгоняет большой остаток дорогого.
func TestScaleCheapSmallDoesNotBeatExpensiveLarge(t *testing.T) {
	exp := now.Add(4 * time.Hour)
	for _, cheapQty := range []domain.Qty{1, 20, 50, 100} {
		in := hotoffers.Input{
			Now: now,
			Batches: []domain.Batch{
				{ID: 1, ProductID: "herb", Remaining: cheapQty, ExpiresAt: exp},
				{ID: 2, ProductID: "steak", Remaining: 2000, ExpiresAt: exp},
			},
			Dishes:   []domain.Dish{dish("salad", "herb", 1), dish("grill", "steak", 200)},
			Price:    map[string]int64{"herb": 10, "steak": 1000},
			RateHour: map[string]float64{},
			Schedule: sched(),
			Config:   cfg(),
		}
		offers, _ := Rank(in)
		g, s := indexOf(offers, "grill"), indexOf(offers, "salad")
		if g != 0 || (s >= 0 && s < g) {
			t.Errorf("herb=%d: grill must lead, got %+v", cheapQty, offers)
		}
	}
}

// Сумма целей по блюдам с общим истекающим продуктом не превышает того, что под угрозой.
func TestScaleSharedProductTargetWithinRisk(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	exp := now.Add(6 * time.Hour)
	for i := 0; i < 200; i++ {
		risk := domain.Qty(100 + r.Intn(3000))
		var dishes []domain.Dish
		norms := map[string]domain.Qty{}
		for j := 0; j < 1+r.Intn(4); j++ {
			id := string(rune('a' + j))
			q := 20 + r.Intn(300)
			norms[id] = domain.Qty(q)
			dishes = append(dishes, dish(id, "milk", q))
		}
		in := hotoffers.Input{
			Now:      now,
			Batches:  []domain.Batch{{ID: 1, ProductID: "milk", Remaining: risk, ExpiresAt: exp}},
			Dishes:   dishes,
			Price:    map[string]int64{"milk": 100},
			RateHour: map[string]float64{"milk": 0},
			Schedule: sched(),
			Config:   hotoffers.Config{TopN: 10, WMin: 0.5, CoverHours: 12, MaxIter: 1000},
		}
		offers, _ := Rank(in)
		var total domain.Qty
		for _, o := range offers {
			total += domain.Qty(o.TargetPortions) * norms[o.DishID]
		}
		if total > risk {
			t.Fatalf("iter %d: target uses %d > at risk %d: %+v", i, total, risk, offers)
		}
	}
}

// Дефицитный неистекающий ингредиент: блюдо не рекомендуется, причина возвращается.
func TestScaleScarceIngredientExcludedWithReason(t *testing.T) {
	in := hotoffers.Input{
		Now: now,
		Batches: []domain.Batch{
			{ID: 1, ProductID: "milk", Remaining: 1000, ExpiresAt: now.Add(4 * time.Hour)},
			{ID: 2, ProductID: "truffle", Remaining: 100, ExpiresAt: now.Add(72 * time.Hour)},
		},
		Dishes:   []domain.Dish{dish("latte", "milk", 100), dish("truffle_latte", "milk", 100, "truffle", 10)},
		Price:    map[string]int64{"milk": 100, "truffle": 5000},
		RateHour: map[string]float64{"milk": 0, "truffle": 20}, // 100 < 12 ч × 20
		Schedule: sched(),
		Config:   cfg(),
	}
	offers, excluded := Rank(in)
	if indexOf(offers, "truffle_latte") >= 0 {
		t.Fatalf("scarce dish recommended: %+v", offers)
	}
	if indexOf(offers, "latte") < 0 {
		t.Fatalf("plain dish lost: %+v", offers)
	}
	if len(excluded) != 1 || excluded[0].DishID != "truffle_latte" || excluded[0].ProductID != "truffle" || excluded[0].Why == "" {
		t.Fatalf("want exclusion with reason, got %+v", excluded)
	}
}

// Продукт, который успеет разойтись при обычном спросе, не делает блюдо горячим.
func TestScaleSellsOutAtNormalDemandNotHot(t *testing.T) {
	in := hotoffers.Input{
		Now:      now,
		Batches:  []domain.Batch{{ID: 1, ProductID: "milk", Remaining: 1000, ExpiresAt: now.Add(10 * time.Hour)}},
		Dishes:   []domain.Dish{dish("latte", "milk", 100)},
		Price:    map[string]int64{"milk": 100},
		RateHour: map[string]float64{"milk": 200}, // 200 × 9 ч = 1800 ≥ 1000
		Schedule: sched(),
		Config:   cfg(),
	}
	if offers, _ := Rank(in); len(offers) != 0 {
		t.Fatalf("sells out by itself, got %+v", offers)
	}
}

// После прыжка времени риск продуктов с далёким сроком не растёт скачком.
func TestScaleTimeJumpFarExpiryRiskNoJump(t *testing.T) {
	far := now.Add(10 * 24 * time.Hour)
	batches := []domain.Batch{{ID: 1, ProductID: "rice", Remaining: 5000, ExpiresAt: far}}
	rate := 10.0
	prev := domain.Qty(-1)
	for h := 0; h <= 48; h += 6 {
		t0 := now.Add(time.Duration(h) * time.Hour)
		st := buildStates(hotoffers.Input{
			Now: t0, Batches: batches, RateHour: map[string]float64{"rice": rate},
			Schedule: sched(), Config: cfg(),
		})["rice"]
		if prev >= 0 {
			// Риск растёт плавно: не больше чем на спрос за рабочие часы шага.
			if d := st.risk - prev; d < 0 || float64(d) > 6*rate+1 {
				t.Fatalf("h=%d: risk jumped %d → %d", h, prev, st.risk)
			}
		}
		prev = st.risk
	}
	// Прыжок на 3 суток вперёд: риск растёт не более чем на спрос за рабочие часы прыжка.
	a := buildStates(hotoffers.Input{Now: now, Batches: batches, RateHour: map[string]float64{"rice": rate}, Schedule: sched(), Config: cfg()})["rice"]
	j := buildStates(hotoffers.Input{Now: now.Add(72 * time.Hour), Batches: batches, RateHour: map[string]float64{"rice": rate}, Schedule: sched(), Config: cfg()})["rice"]
	if j.risk < a.risk || float64(j.risk-a.risk) > 3*13*rate+1 {
		t.Fatalf("jump changed risk %d → %d", a.risk, j.risk)
	}
}
