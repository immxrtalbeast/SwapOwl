package store

import (
	"testing"
	"time"

	"umirhack/internal/domain"
)

var fcNow = time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)

func fcSnap(rate float64) Snapshot {
	return Snapshot{
		Now:      fcNow,
		Schedule: domain.Schedule{Location: time.UTC, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour},
		Batches: []domain.Batch{
			{ID: 1, ProductID: "salmon", Quantity: 3000, Remaining: 1400, ExpiresAt: fcNow.Add(5 * time.Hour)},
			{ID: 2, ProductID: "beef", Quantity: 900, Remaining: 900, ExpiresAt: fcNow.Add(72 * time.Hour)},
		},
		Products: map[string]ProductRef{"salmon": {Name: "Лосось", Unit: domain.UnitG}, "beef": {Name: "Говядина", Unit: domain.UnitG}},
		Price:    map[string]int64{"salmon": 90000, "beef": 60000},
		RateHour: map[string]float64{"salmon": rate, "beef": 0},
	}
}

func TestForecastBatchThatSellsOutIsZero(t *testing.T) {
	f := ForecastWriteoffs(fcSnap(300), 24) // 300 г/ч · 5 ч ≥ 1400 г
	if f.TotalMinor != 0 || len(f.Products) != 0 || len(f.Days) != 0 {
		t.Fatalf("want empty forecast, got %+v", f)
	}
}

func TestForecastLargeRemainderPricedAtRisk(t *testing.T) {
	f := ForecastWriteoffs(fcSnap(200), 24) // cap 1000 г, риск 400 г × 900 ₽/кг = 360 ₽
	if f.TotalMinor != 36000 || len(f.Products) != 1 || f.Products[0].AtRisk != 400 || f.Products[0].TotalMinor != 36000 {
		t.Fatalf("got %+v", f)
	}
	if len(f.Days) != 1 || f.Days[0].Date != "2026-10-10" || f.Days[0].TotalMinor != 36000 {
		t.Fatalf("days: %+v", f.Days)
	}
	if !f.GeneratedAt.Equal(fcNow) {
		t.Fatalf("generated_at: %v", f.GeneratedAt)
	}
}

func TestForecastHorizonHidesFarBatch(t *testing.T) {
	// Говядина без спроса, срок через 3 суток: 24 ч её не видят, 72 ч — тоже (срок не раньше границы), 168 ч — видят.
	for _, tc := range []struct {
		hours int
		want  int64
	}{{24, 36000}, {72, 36000}, {168, 36000 + 54000}} {
		if got := ForecastWriteoffs(fcSnap(200), tc.hours).TotalMinor; got != tc.want {
			t.Fatalf("hours=%d: got %d want %d", tc.hours, got, tc.want)
		}
	}
}
