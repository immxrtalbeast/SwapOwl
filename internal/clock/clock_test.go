package clock

import (
	"context"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func TestFixed(t *testing.T) {
	f := NewFixed(t0)
	f.Advance(time.Hour)
	if !f.Now().Equal(t0.Add(time.Hour)) {
		t.Fatal("advance")
	}
	f.Set(t0)
	if !f.Now().Equal(t0) {
		t.Fatal("set")
	}
	var _ Clock = Real{}
	if (Real{}).Now().IsZero() {
		t.Fatal("real")
	}
}

func TestAcceleratedSpeedAndPause(t *testing.T) {
	ctx := context.Background()
	c, err := NewAccelerated(ctx, &MemStore{}, t0, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetSpeed(ctx, 3600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if d := c.Now().Sub(t0); d < 60*time.Second {
		t.Fatalf("too slow: %v", d)
	}
	_ = c.Pause(ctx)
	a := c.Now()
	time.Sleep(10 * time.Millisecond)
	if !c.Now().Equal(a) {
		t.Fatal("paused clock moved")
	}
	if err := c.SetSpeed(ctx, -1); err != ErrInvalidSpeed {
		t.Fatalf("got %v", err)
	}
}

func TestJumpBounds(t *testing.T) {
	ctx := context.Background()
	st := &MemStore{}
	c, _ := NewAccelerated(ctx, st, t0, time.Time{})
	_ = c.Pause(ctx)
	for _, h := range []float64{0, -1, 720.0001, 1e9} {
		if _, err := c.Jump(ctx, h); err != ErrInvalidJump {
			t.Fatalf("hours=%v: got %v", h, err)
		}
	}
	if len(st.Jumps()) != 0 {
		t.Fatal("invalid jump journaled")
	}
	before := c.Now()
	to, err := c.Jump(ctx, 720)
	if err != nil || !to.Equal(before.Add(720*time.Hour)) {
		t.Fatalf("max jump: %v %v", to, err)
	}
	if _, err := c.Jump(ctx, 0.0001); err != nil {
		t.Fatal(err)
	}
	if len(st.Jumps()) != 2 {
		t.Fatalf("journal: %d", len(st.Jumps()))
	}
}

func TestRestart(t *testing.T) {
	ctx := context.Background()
	st := &MemStore{}
	c, _ := NewAccelerated(ctx, st, t0, time.Time{})
	_ = c.SetSpeed(ctx, 10)
	_, _ = c.Jump(ctx, 24)
	last := c.Now()
	_ = c.Persist(ctx)

	// простой процесса не засчитывается; событие новее сохранённого — база по нему
	c2, err := NewAccelerated(ctx, st, t0, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if c2.Now().Before(last) || c2.Speed() != 10 {
		t.Fatalf("restart went back: %v < %v, speed %v", c2.Now(), last, c2.Speed())
	}
	ev := c2.Now().Add(48 * time.Hour)
	c3, _ := NewAccelerated(ctx, st, t0, ev)
	if c3.Now().Before(ev) {
		t.Fatal("now before last event")
	}
	// событие старше сохранённого времени не откатывает часы
	c4, _ := NewAccelerated(ctx, st, t0, t0)
	if c4.Now().Before(c3.Now().Add(-time.Second)) {
		t.Fatal("rolled back by old event")
	}
}

func TestMonotonicConcurrent(t *testing.T) {
	ctx := context.Background()
	c, _ := NewAccelerated(ctx, &MemStore{}, t0, time.Time{})
	stop := make(chan struct{})
	var wg, ctl sync.WaitGroup
	fail := make(chan string, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			prev := c.Now()
			for {
				select {
				case <-stop:
					return
				default:
				}
				n := c.Now()
				if n.Before(prev) {
					fail <- "time went backwards"
					return
				}
				prev = n
			}
		}()
	}
	ctl.Add(1)
	go func() {
		defer ctl.Done()
		speeds := []float64{1, 500, 0, 7, 100000, 0.5}
		for i := 0; i < 300; i++ {
			_ = c.SetSpeed(ctx, speeds[i%len(speeds)])
			if i%5 == 0 {
				_, _ = c.Jump(ctx, float64(1+i%720))
			}
			if i%7 == 0 {
				_ = c.Persist(ctx)
			}
		}
	}()
	ctl.Wait()
	close(stop)
	wg.Wait()
	select {
	case m := <-fail:
		t.Fatal(m)
	default:
	}
}

func TestRunPersistsOnCancel(t *testing.T) {
	st := &MemStore{}
	c, err := NewAccelerated(context.Background(), st, t0, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetSpeed(context.Background(), 600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx, time.Hour); close(done) }() // тик не успеет сработать
	time.Sleep(30 * time.Millisecond)
	cancelAt := c.Now()
	cancel()
	<-done
	got, ok, _ := st.Load(context.Background())
	if !ok || got.SimTime.Before(cancelAt) {
		t.Fatalf("сохранено %v, раньше момента отмены %v", got.SimTime, cancelAt)
	}
}
