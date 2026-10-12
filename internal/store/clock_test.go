package store

import (
	"context"
	"testing"
	"time"

	"umirhack/internal/clock"
)

func TestClockStoreRoundTripAndJumpLog(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	cs := db.ClockStore()
	if _, ok, err := cs.Load(ctx); err != nil || ok {
		t.Fatalf("empty: ok=%v err=%v", ok, err)
	}
	st := clock.State{SimTime: t0, Speed: 600}
	if err := cs.Save(ctx, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := cs.Load(ctx)
	if err != nil || !ok || !got.SimTime.Equal(t0) || got.Speed != 600 {
		t.Fatalf("load: %+v %v %v", got, ok, err)
	}
	to := t0.Add(6 * time.Hour)
	if err := cs.RecordJump(ctx, clock.Jump{From: t0, To: to, Hours: 6}, clock.State{SimTime: to, Speed: 600}); err != nil {
		t.Fatal(err)
	}
	got, _, _ = cs.Load(ctx)
	var n int
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM clock_jumps WHERE from_at = $1 AND to_at = $2`, t0, to).Scan(&n)
	if !got.SimTime.Equal(to) || n != 1 {
		t.Fatalf("after jump: %+v jumps=%d", got, n)
	}
}

func TestLastEventTimeIsMaxOfEvents(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	if last, err := db.LastEventTime(ctx); err != nil || !last.IsZero() {
		t.Fatalf("empty db: %v %v", last, err)
	}
	if _, err := db.ReceiveDelivery(ctx, delivery("D1"), clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
	last, err := db.LastEventTime(ctx)
	if err != nil || !last.Equal(t0) {
		t.Fatalf("after delivery: %v %v", last, err)
	}
}

func TestSimulatorRunningSeparateFromSaveClock(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	cs := db.ClockStore()
	if on, err := cs.SimulatorRunning(ctx); err != nil || on {
		t.Fatalf("no row: on=%v err=%v", on, err)
	}
	if err := cs.Save(ctx, clock.State{SimTime: t0, Speed: 1}); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetSimulatorRunning(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := cs.Save(ctx, clock.State{SimTime: t0.Add(time.Hour), Speed: 600}); err != nil {
		t.Fatal(err)
	}
	if on, err := cs.SimulatorRunning(ctx); err != nil || !on {
		t.Fatalf("SaveClock must keep the flag: on=%v err=%v", on, err)
	}
}
