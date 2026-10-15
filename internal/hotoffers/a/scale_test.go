package a

import (
	"testing"
	"time"

	"umirhack/internal/domain"
)

// Якоря К3 (критерии кейса, критерий 3, 9 баллов), применимые к алгоритму R:
// он не читает прогноз спроса и не делит цель между блюдами.

// Малый остаток дешёвого продукта не обгоняет большой остаток дорогого.
func TestScaleCheapSmallDoesNotBeatExpensiveLarge(t *testing.T) {
	exp := t0.Add(4 * time.Hour)
	for _, cheapQty := range []domain.Qty{1, 20, 50, 100} {
		in := base()
		in.Price = map[string]int64{"herb": 10, "steak": 1000}
		in.Batches = []domain.Batch{
			{ID: 1, ProductID: "herb", Remaining: cheapQty, ExpiresAt: exp},
			{ID: 2, ProductID: "steak", Remaining: 2000, ExpiresAt: exp},
		}
		in.Dishes = []domain.Dish{dish("salad", "herb", 1), dish("grill", "steak", 200)}
		offers, _ := Rank(in)
		if len(offers) == 0 || offers[0].DishID != "grill" {
			t.Errorf("herb=%d: grill must lead, got %+v", cheapQty, offers)
		}
	}
}

// После прыжка времени приоритет блюда с далёким сроком растёт плавно:
// не быстрее, чем уменьшается число рабочих часов до срока, и не падает.
func TestScaleTimeJumpFarExpiryNoJump(t *testing.T) {
	far := t0.Add(10 * 24 * time.Hour)
	var prev float64
	var prevW float64
	for h := 0; h <= 48; h += 6 {
		in := base()
		in.Now = t0.Add(time.Duration(h) * time.Hour)
		in.Batches = []domain.Batch{{ID: 1, ProductID: "x", Remaining: 5000, ExpiresAt: far}}
		in.Dishes = []domain.Dish{dish("d", "x", 100)}
		offers, _ := Rank(in)
		if len(offers) != 1 {
			t.Fatalf("h=%d: %+v", h, offers)
		}
		p, w := offers[0].Priority, offers[0].Reasons[0].WorkingHoursLeft
		if h > 0 {
			// priority ∝ 1/W: p·W постоянно с точностью округления до 6 знаков.
			if p < prev || p*w < prev*prevW*0.9999 || p*w > prev*prevW*1.0001 {
				t.Fatalf("h=%d: priority jumped %v (W=%v) → %v (W=%v)", h, prev, prevW, p, w)
			}
		}
		prev, prevW = p, w
	}
}
