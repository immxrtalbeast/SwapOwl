package domain

import (
	"errors"
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func baseInput(now time.Time) DemandInput {
	return DemandInput{
		Now: now, WindowDays: 14, PriorDays: 3,
		Priors: map[string]float64{"soup": 10, "cake": 0},
	}
}

func TestDishDemandZeroHistoryEqualsPriorBlend(t *testing.T) {
	got, err := DishDemand(baseInput(t0))
	if err != nil {
		t.Fatal(err)
	}
	if want := 10 * 3.0 / 17.0; math.Abs(got["soup"]-want) > 1e-9 {
		t.Fatalf("soup = %v, want %v", got["soup"], want)
	}
	if got["cake"] != 0 {
		t.Fatalf("cake = %v", got["cake"])
	}
}

func TestDishDemandCountsOnlyWindow(t *testing.T) {
	in := baseInput(t0)
	in.History = []OrderedDish{
		{At: t0.Add(-time.Hour), DishID: "soup", Portions: 17},
		{At: t0.Add(-15 * day), DishID: "soup", Portions: 1000}, // вне окна
		{At: t0.Add(-time.Hour), DishID: "unknown", Portions: 5},
	}
	got, _ := DishDemand(in)
	if want := (30.0 + 17) / 17; math.Abs(got["soup"]-want) > 1e-9 {
		t.Fatalf("soup = %v, want %v", got["soup"], want)
	}
}

func TestDishDemandTimeJumpWithoutOrders(t *testing.T) {
	b, _ := DishDemand(baseInput(t0))
	for _, jump := range []time.Duration{time.Hour, 3 * day, 10 * day, 40 * day} {
		after := baseInput(t0.Add(jump))
		after.Jumps = []TimeJump{{From: t0, To: t0.Add(jump)}}
		a, err := DishDemand(after)
		if err != nil {
			t.Fatal(err)
		}
		for id := range b {
			if math.Abs(a[id]-b[id]) > 1e-6 {
				t.Fatalf("jump %v: %s %v != %v", jump, id, a[id], b[id])
			}
		}
	}
}

func TestDishDemandOverlappingJumps(t *testing.T) {
	b, _ := DishDemand(baseInput(t0))
	after := baseInput(t0.Add(6 * day))
	after.Jumps = []TimeJump{
		{From: t0, To: t0.Add(4 * day)},
		{From: t0.Add(2 * day), To: t0.Add(6 * day)},
	}
	a, _ := DishDemand(after)
	if math.Abs(a["soup"]-b["soup"]) > 1e-6 {
		t.Fatalf("%v != %v", a["soup"], b["soup"])
	}
}

func TestDishDemandJumpKeepsOldOrders(t *testing.T) {
	in := baseInput(t0.Add(5 * day))
	in.Jumps = []TimeJump{{From: t0, To: t0.Add(5 * day)}}
	in.History = []OrderedDish{{At: t0.Add(-13 * day), DishID: "soup", Portions: 14}}
	got, _ := DishDemand(in)
	if want := (30.0 + 14) / 17; math.Abs(got["soup"]-want) > 1e-9 {
		t.Fatalf("soup = %v, want %v", got["soup"], want)
	}
}

func TestDishDemandSinceBoundsWindow(t *testing.T) {
	in := baseInput(t0)
	in.Since = t0.Add(-2 * day)
	got, _ := DishDemand(in)
	if want := 30.0 / 5; math.Abs(got["soup"]-want) > 1e-9 {
		t.Fatalf("soup = %v, want %v", got["soup"], want)
	}
}

func TestDishDemandValidation(t *testing.T) {
	in := baseInput(t0)
	in.PriorDays = 0
	if _, err := DishDemand(in); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v", err)
	}
	in = baseInput(t0)
	in.WindowDays = 0
	if _, err := DishDemand(in); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v", err)
	}
}

func TestProductDemandAndHourlyRate(t *testing.T) {
	dishes := []Dish{
		{ID: "soup", Recipe: []RecipeItem{{ProductID: "water", Quantity: 300}, {ProductID: "salt", Quantity: 2}}},
		{ID: "tea", Recipe: []RecipeItem{{ProductID: "water", Quantity: 200}}},
	}
	per := ProductDemandPerDay(dishes, map[string]float64{"soup": 2, "tea": 5})
	if per["water"] != 1600 || per["salt"] != 4 {
		t.Fatalf("per = %v", per)
	}
	sch := Schedule{OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour}
	if got := RatePerHour(1300, sch); got != 100 {
		t.Fatalf("hourly = %v", got)
	}
	if got := RatePerHour(10, Schedule{}); got != 0 {
		t.Fatalf("degenerate = %v", got)
	}
}

func TestRatePerHourWithSchedule(t *testing.T) {
	days, err := ParseSchedule("mon 20:00-02:00") // 6 ч в неделю
	if err != nil {
		t.Fatal(err)
	}
	// OpenAt/CloseAt при Days игнорируются, даже перевёрнутые.
	sch := Schedule{Location: time.UTC, OpenAt: 23 * time.Hour, CloseAt: 10 * time.Hour, Days: days}
	if got, want := RatePerHour(60, sch), 60/(6.0/7); got != want {
		t.Fatalf("got %v want %v", got, want)
	}
	all, _ := ParseSchedule("mon-sun 10:00-23:00")
	if got := RatePerHour(1300, Schedule{Days: all}); got != 100 {
		t.Fatalf("13 ч каждый день: %v", got)
	}
}
