package store

import (
	"testing"
	"time"

	"umirhack/internal/domain"
)

func TestBuildSupplyInput(t *testing.T) {
	now := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	snap := Snapshot{
		Now:      now,
		Schedule: domain.Schedule{Location: time.UTC, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour},
		Batches: []domain.Batch{
			// Первая партия: за час при темпе 100/ч уйдёт 100 из 500, остальное под риском.
			{ID: 1, ProductID: "salmon", Remaining: 500, ExpiresAt: now.Add(time.Hour)},
			{ID: 2, ProductID: "salmon", Remaining: 1000, ExpiresAt: now.Add(72 * time.Hour)},
		},
		Dishes: []domain.Dish{
			{ID: "steak", Recipe: []domain.RecipeItem{{ProductID: "salmon", Quantity: 200}}},
			{ID: "bowl", Recipe: []domain.RecipeItem{{ProductID: "salmon", Quantity: 150}, {ProductID: "rice", Quantity: 100}}},
		},
		Products: map[string]ProductRef{"salmon": {Name: "Лосось"}, "rice": {Name: "Рис"}, "salt": {Name: "Соль"}},
		Stopped:  map[string]bool{"bowl": true},
		RateDay:  map[string]float64{"salmon": 100},
		RateHour: map[string]float64{"salmon": 100},
	}
	meta := map[string]SupplyMeta{"salmon": {LeadDays: 2, PackSize: 500}}
	in := BuildSupplyInput(snap, meta, 3, 10)

	if len(in.Products) != 2 {
		t.Fatalf("только продукты из рецептов, got %d", len(in.Products))
	}
	rice, salmon := in.Products[0], in.Products[1]
	if rice.ID != "rice" || salmon.ID != "salmon" {
		t.Fatalf("порядок по id: %v", in.Products)
	}
	if salmon.UsableStock != 1100 || salmon.LeadDays != 2 || salmon.PackSize != 500 || salmon.MaxRecipeNorm != 200 {
		t.Errorf("salmon: %+v", salmon)
	}
	if salmon.BlocksDishes != 0 {
		t.Errorf("лосося хватает на порцию bowl: %+v", salmon)
	}
	if rice.BlocksDishes != 1 || rice.PackSize != 1 || rice.UsableStock != 0 {
		t.Errorf("rice: %+v", rice)
	}
	if in.CoverDays != 3 || in.MinPortions != 10 {
		t.Errorf("конфиг не передан: %+v", in)
	}
}
