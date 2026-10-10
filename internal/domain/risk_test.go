package domain

import (
	"testing"
	"time"
)

var schedule = Schedule{Location: time.UTC, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour}

func at(h int) time.Time { return time.Date(2026, 10, 1, h, 0, 0, 0, time.UTC) }

func TestWorkingHours(t *testing.T) {
	next := func(h int) time.Time { return at(h).AddDate(0, 0, 1) }
	cases := []struct {
		name     string
		from, to time.Time
		want     float64
	}{
		{"inside", at(11), at(15), 4},
		{"whole day", at(0), next(0), 13},
		{"night gap", at(22), next(11), 2},
		{"closed only", at(23), next(10), 0},
		{"reversed", at(15), at(11), 0},
		{"empty", at(12), at(12), 0},
		{"three days", at(0), at(0).AddDate(0, 0, 3), 39},
	}
	for _, c := range cases {
		if got := WorkingHours(c.from, c.to, schedule); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if got := WorkingHours(at(0), at(23), Schedule{}); got != 0 {
		t.Errorf("empty schedule: %v", got)
	}
}

func TestWorkingHoursTimezone(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	sch := Schedule{Location: msk, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour}
	// 07:00–20:00 UTC = 10:00–23:00 MSK
	if got := WorkingHours(at(7), at(20), sch); got != 13 {
		t.Fatalf("%v", got)
	}
}

func TestRiskByBatchFEFO(t *testing.T) {
	// now 10:00, 100/ч: до 15:00 — 5 ч → 500, до 20:00 — 10 ч → 1000.
	batches := []Batch{
		{ID: 2, Remaining: 800, ExpiresAt: at(20)},
		{ID: 1, Remaining: 300, ExpiresAt: at(15)},
	}
	got := RiskByBatch(batches, 100, at(10), schedule)
	if len(got) != 2 || got[0].BatchID != 1 || got[1].BatchID != 2 {
		t.Fatalf("order: %+v", got)
	}
	if got[0].Risk != 0 {
		t.Fatalf("b1 risk = %d", got[0].Risk)
	}
	if got[1].Risk != 100 { // cap 1000−300=700, остаток 800
		t.Fatalf("b2 risk = %d", got[1].Risk)
	}
}

func TestRiskByBatchTieBreakByID(t *testing.T) {
	batches := []Batch{
		{ID: 9, Remaining: 100, ExpiresAt: at(15)},
		{ID: 3, Remaining: 100, ExpiresAt: at(15)},
	}
	got := RiskByBatch(batches, 20, at(10), schedule) // cap 100 на обе
	if got[0].BatchID != 3 || got[0].Risk != 0 || got[1].Risk != 100 {
		t.Fatalf("%+v", got)
	}
}

func TestRiskByBatchZeroRate(t *testing.T) {
	got := RiskByBatch([]Batch{{ID: 1, Remaining: 50, ExpiresAt: at(20)}}, 0, at(10), schedule)
	if got[0].Risk != 50 || got[0].Doomed {
		t.Fatalf("%+v", got[0])
	}
}

func TestRiskByBatchEmpty(t *testing.T) {
	if got := RiskByBatch(nil, 10, at(10), schedule); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestRiskByBatchExpiryBoundary(t *testing.T) {
	now := at(12)
	for _, exp := range []time.Time{now, now.Add(-time.Hour)} {
		g := RiskByBatch([]Batch{{ID: 1, Remaining: 10, ExpiresAt: exp}}, 100, now, schedule)
		if !g[0].Doomed || g[0].Risk != 10 || g[0].WorkingHoursLeft != 0 {
			t.Fatalf("exp %v: %+v", exp, g[0])
		}
	}
	// ровно W_min = 0,5 ч — не обречена
	g := RiskByBatch([]Batch{{ID: 1, Remaining: 100, ExpiresAt: now.Add(30 * time.Minute)}}, 100, now, schedule)
	if g[0].Doomed || g[0].Risk != 50 {
		t.Fatalf("at W_min: %+v", g[0])
	}
	g = RiskByBatch([]Batch{{ID: 1, Remaining: 100, ExpiresAt: now.Add(30*time.Minute - time.Second)}}, 100, now, schedule)
	if !g[0].Doomed || g[0].Risk != 100 {
		t.Fatalf("below W_min: %+v", g[0])
	}
}

func TestRiskByBatchDoomedDoesNotConsumeCapacity(t *testing.T) {
	now := at(12)
	batches := []Batch{
		{ID: 1, Remaining: 1000, ExpiresAt: now.Add(10 * time.Minute)}, // обречена
		{ID: 2, Remaining: 200, ExpiresAt: at(14)},                     // cap 200
	}
	g := RiskByBatch(batches, 100, now, schedule)
	if !g[0].Doomed || g[0].Risk != 1000 || g[1].Risk != 0 {
		t.Fatalf("%+v", g)
	}
}

func TestRiskByBatchClosedHoursNotCounted(t *testing.T) {
	b := Batch{ID: 1, Remaining: 500, ExpiresAt: at(10).AddDate(0, 0, 1)}
	g := RiskByBatch([]Batch{b}, 100, at(22), schedule) // 1 рабочий час
	if g[0].WorkingHoursLeft != 1 || g[0].Risk != 400 {
		t.Fatalf("%+v", g[0])
	}
}

func TestRiskByBatchDoesNotMutateInput(t *testing.T) {
	in := []Batch{{ID: 2, Remaining: 1, ExpiresAt: at(20)}, {ID: 1, Remaining: 1, ExpiresAt: at(15)}}
	RiskByBatch(in, 1, at(10), schedule)
	if in[0].ID != 2 {
		t.Fatal("input reordered")
	}
}
