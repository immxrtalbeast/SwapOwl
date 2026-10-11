package stand

import (
	"context"
	"fmt"
	neturl "net/url"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"umirhack/internal/clock"
	"umirhack/internal/domain"
	"umirhack/internal/events"
	"umirhack/internal/hotoffers"
	"umirhack/internal/hotoffers/a"
	"umirhack/internal/janitor"
	"umirhack/internal/store"
)

var wall = time.Date(2026, 10, 10, 9, 30, 0, 0, time.UTC) // 12:30 по Москве → T0 = 08:00 UTC

func newStand(t *testing.T) (*Stand, *store.DB) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	// Своя база: пакеты тестов идут параллельно и не должны сбрасывать схему друг друга.
	admin, err := store.Open(ctx, url, 1)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("stand_%d_%d", os.Getpid(), time.Now().UnixNano())
	if _, err := admin.Pool.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	u, err := neturl.Parse(url)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	db, err := store.Open(ctx, u.String(), 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Pool.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		admin.Close()
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	t0 := T0(wall, loc)
	if loaded, err := Load(ctx, db, t0); err != nil || !loaded {
		t.Fatalf("seed on empty: %v %v", loaded, err)
	}
	if loaded, err := Load(ctx, db, t0); err != nil || loaded {
		t.Fatalf("seed must not load twice: %v %v", loaded, err)
	}
	clk, err := clock.NewAccelerated(ctx, db.ClockStore(), t0, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	gate := &Gate{}
	jan := janitor.New(GatedJanitorStore{Inner: db, Gate: gate}, clk.Now, nil)
	return &Stand{DB: db, Clock: clk, Gate: gate, Sweep: jan, Bus: events.New(clk.Now), Loc: loc,
		Wall: func() time.Time { return wall }}, db
}

// state — стоп-лист, горячие предложения и снимок склада на момент now.
type state struct {
	Snap   store.Snapshot
	Offers []hotoffers.Offer
}

func capture(t *testing.T, s *Stand, now time.Time) state {
	t.Helper()
	snap, err := s.DB.Snapshot(context.Background(), now, store.SnapshotParams{
		WindowDays: 14, PriorDays: 3,
		Schedule: domain.Schedule{Location: s.Loc, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour}})
	if err != nil {
		t.Fatal(err)
	}
	offers, _ := a.Rank(hotoffers.Input{Now: snap.Now, Batches: snap.Batches, Dishes: snap.Dishes,
		Stopped: snap.Stopped, Price: snap.Price, RateHour: snap.RateHour, Schedule: snap.Schedule,
		Config: hotoffers.Config{TopN: 3, WMin: 0.5, CoverHours: 12, MaxIter: 200}})
	return state{snap, offers}
}

func placeOrder(s *Stand, id string, d domain.Dish, portions int) error {
	needs := make([]domain.Need, 0, len(d.Recipe))
	for _, r := range d.Recipe {
		needs = append(needs, domain.Need{ProductID: r.ProductID, Qty: r.Quantity * domain.Qty(portions)})
	}
	_, err := s.DB.PlaceOrder(context.Background(), store.OrderInput{ID: id,
		Items: []store.OrderItem{{DishID: d.ID, Portions: portions}}, Needs: needs}, s.Now)
	return err
}

func uuid(i int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", i) }

func TestResetRestoresReferenceState(t *testing.T) {
	s, _ := newStand(t)
	ctx := context.Background()
	if err := s.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	t0 := s.Now()
	ref := capture(t, s, t0)
	if len(ref.Snap.Dishes) == 0 || len(ref.Snap.Batches) == 0 {
		t.Fatalf("empty seed: %d dishes %d batches", len(ref.Snap.Dishes), len(ref.Snap.Batches))
	}

	// Портим состояние: заказы, скорость, прыжок со списанием просрочки.
	for i, d := range ref.Snap.Dishes {
		_ = placeOrder(s, uuid(i+1), d, 200) // отказы тоже пишут заказ
	}
	if err := s.SetSpeed(ctx, 600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Jump(ctx, 72); err != nil {
		t.Fatal(err)
	}
	if got := capture(t, s, s.Now()); reflect.DeepEqual(got.Snap.Stopped, ref.Snap.Stopped) && len(got.Snap.Batches) == len(ref.Snap.Batches) {
		t.Fatal("scenario did not change the stand")
	}

	if err := s.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.Now().Before(t0.Add(time.Second)) || s.Speed() != 1 {
		t.Fatalf("clock after reset: %v speed %v, want T0 %v ×1", s.Now(), s.Speed(), t0)
	}
	got := capture(t, s, t0)
	if !reflect.DeepEqual(got.Snap.Stopped, ref.Snap.Stopped) || !reflect.DeepEqual(got.Snap.Batches, ref.Snap.Batches) ||
		!reflect.DeepEqual(got.Snap.Lambda, ref.Snap.Lambda) {
		t.Fatalf("snapshot differs after reset:\nref %+v\ngot %+v", ref.Snap.Stopped, got.Snap.Stopped)
	}
	if !reflect.DeepEqual(got.Offers, ref.Offers) {
		t.Fatalf("hot offers differ after reset:\nref %+v\ngot %+v", ref.Offers, got.Offers)
	}
	var jumps int
	_ = s.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM clock_jumps`).Scan(&jumps)
	if jumps != 0 {
		t.Fatalf("clock_jumps not cleared: %d", jumps)
	}
}

func TestResetWithParallelOrdersDoesNotDeadlock(t *testing.T) {
	s, _ := newStand(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := s.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	t0 := s.Now()
	ref := capture(t, s, t0)
	dishes := ref.Snap.Dishes

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				// Как звено роутера: изменяющий запрос держит затвор на чтение.
				s.Gate.RLock()
				_ = placeOrder(s, uuid(w*100000+i+1000), dishes[i%len(dishes)], 1)
				s.Gate.RUnlock()
			}
		}(w)
	}
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 3; i++ {
			if err := s.Reset(ctx); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("deadlock: reset did not finish")
	}
	close(stop)
	wg.Wait()

	// После последнего сброса вернуть эталон и сверить: заказы, прошедшие до сброса, не остались.
	if err := s.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	got := capture(t, s, t0)
	if !reflect.DeepEqual(got.Snap.Stopped, ref.Snap.Stopped) || !reflect.DeepEqual(got.Snap.Batches, ref.Snap.Batches) ||
		!reflect.DeepEqual(got.Offers, ref.Offers) {
		t.Fatal("state after resets under load differs from the reference")
	}
}

func TestJumpSweepsAndClockSurvivesRestart(t *testing.T) {
	s, db := newStand(t)
	ctx := context.Background()
	if err := s.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	sub := s.Bus.Subscribe()
	defer sub.Unsubscribe()
	before := s.Now()
	to, err := s.Jump(ctx, 24*10)
	if err != nil {
		t.Fatal(err)
	}
	if to.Sub(before) < 240*time.Hour {
		t.Fatalf("jump: %v → %v", before, to)
	}
	var expired int
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM movements WHERE reason = 'expired' AND at > $1`, before).Scan(&expired)
	if expired == 0 {
		t.Fatal("jump did not sweep expired batches synchronously")
	}
	var gotClock bool
	for len(sub.C) > 0 {
		if ev := <-sub.C; ev.Type == events.ClockChanged {
			gotClock = true
		}
	}
	if !gotClock {
		t.Fatal("no clock.changed event")
	}
	if _, err := s.Jump(ctx, 0); err != clock.ErrInvalidJump {
		t.Fatalf("jump 0: %v", err)
	}
	if err := s.SetSpeed(ctx, -1); err != clock.ErrInvalidSpeed {
		t.Fatalf("speed -1: %v", err)
	}

	// Рестарт: время не уходит назад, простой процесса не засчитывается.
	last, err := db.LastEventTime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Clock.Persist(ctx); err != nil {
		t.Fatal(err)
	}
	c2, err := clock.NewAccelerated(ctx, db.ClockStore(), time.Time{}, last)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Now().Before(to) {
		t.Fatalf("time went back after restart: %v < %v", c2.Now(), to)
	}
}

func TestRealClockRejectsSpeedAndJump(t *testing.T) {
	s := &Stand{}
	if err := s.SetSpeed(context.Background(), 2); err != ErrNotSimulated {
		t.Fatalf("speed: %v", err)
	}
	if _, err := s.Jump(context.Background(), 1); err != ErrNotSimulated {
		t.Fatalf("jump: %v", err)
	}
	if s.Simulated() || s.Speed() != 1 {
		t.Fatal("real clock info")
	}
}
