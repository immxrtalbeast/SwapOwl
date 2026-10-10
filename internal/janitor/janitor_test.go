package janitor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"umirhack/internal/store"
)

type fakeStore struct {
	sweeps atomic.Int32
	out    []store.ExpiredBatch
}

func (f *fakeStore) SweepExpired(context.Context, func() time.Time) ([]store.ExpiredBatch, error) {
	f.sweeps.Add(1)
	out := f.out
	f.out = nil
	return out, nil
}

func (f *fakeStore) NextExpiry(context.Context, time.Time) (*time.Time, error) { return nil, nil }

func TestSweepReportsOnlyWhenSomethingExpired(t *testing.T) {
	fs := &fakeStore{out: []store.ExpiredBatch{{BatchID: 1, ProductID: "p", Qty: 5}}}
	var calls int
	j := New(fs, time.Now, func([]store.ExpiredBatch) { calls++ })
	_, _ = j.Sweep(context.Background())
	_, _ = j.Sweep(context.Background())
	if calls != 1 {
		t.Fatalf("OnSweep calls: %d", calls)
	}
}

func TestRunSweepsOnKickAndStopsOnCancel(t *testing.T) {
	fs := &fakeStore{}
	j := New(fs, time.Now, nil)
	j.MinInterval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { j.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for fs.sweeps.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	j.Kick()
	for fs.sweeps.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if fs.sweeps.Load() < 2 {
		t.Fatalf("kick did not trigger a sweep: %d", fs.sweeps.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}
