package simulator

import (
	"context"
	"reflect"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

func whatIfSnap() store.Snapshot {
	now := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	return store.Snapshot{
		Now:      now,
		Schedule: domain.Schedule{Location: time.UTC, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour},
		Batches:  []domain.Batch{{ID: 1, ProductID: "salmon", Quantity: 3000, Remaining: 1400, ExpiresAt: now.Add(5 * time.Hour)}},
		Dishes:   []domain.Dish{{ID: "steak", Name: "Стейк", Recipe: []domain.RecipeItem{{ProductID: "salmon", Quantity: 200}}}},
		Products: map[string]store.ProductRef{"salmon": {Name: "Лосось", Unit: domain.UnitG}},
		Price:    map[string]int64{"salmon": 90000},
		Lambda:   map[string]float64{"steak": 13},
		RateDay:  map[string]float64{"salmon": 2600},
		RateHour: map[string]float64{"salmon": 200},
	}
}

func TestWhatIfDoesNotMutateSnapshotAndIsDeterministic(t *testing.T) {
	snap, before := whatIfSnap(), whatIfSnap()
	sc := []WhatIfScenario{
		{Type: WhatIfDiscount, DishID: "steak", Percent: 30},
		{Type: WhatIfOrderLess, ProductID: "salmon", Percent: 100},
		{Type: WhatIfDemand, DishID: "steak", Factor: 2},
	}
	a, err := WhatIf(context.Background(), snap, 3, sc)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := WhatIf(context.Background(), snap, 3, sc)
	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(snap, before) {
		t.Fatal("результат не детерминирован или снимок изменён")
	}
	if a.Outcomes[1].TotalMinor != 0 || a.Outcomes[2].TotalMinor != 0 || a.BaseMinor != 36000 {
		t.Fatalf("%+v", a)
	}
}

func TestWhatIfDiscountMonotone(t *testing.T) {
	prev := int64(1 << 60)
	for p := 5.0; p <= 50; p += 5 {
		r, err := WhatIf(context.Background(), whatIfSnap(), 3, []WhatIfScenario{{Type: WhatIfDiscount, DishID: "steak", Percent: p}})
		if err != nil {
			t.Fatal(err)
		}
		got := r.Outcomes[0].TotalMinor
		if got > r.BaseMinor || got > prev {
			t.Fatalf("скидка %v%% увеличила списания: %d (база %d, прошлая %d)", p, got, r.BaseMinor, prev)
		}
		prev = got
	}
}

func TestWhatIfCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := WhatIf(ctx, whatIfSnap(), 1, []WhatIfScenario{{Type: WhatIfDemand, DishID: "steak", Factor: 1}}); err == nil {
		t.Fatal("ожидалась ошибка отменённого контекста")
	}
}
