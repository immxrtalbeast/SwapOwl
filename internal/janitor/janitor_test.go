package janitor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

type fakeStore struct {
	sweeps atomic.Int32
	out    []store.ExpiredBatch
	expg   []store.ExpiringBatch
	next   *time.Time
}

func (f *fakeStore) Sweep(context.Context, func() time.Time, *domain.ExpiryConfig) (store.SweepResult, error) {
	f.sweeps.Add(1)
	res := store.SweepResult{Expired: f.out, Expiring: f.expg}
	f.out, f.expg = nil, nil
	return res, nil
}

func (f *fakeStore) NextExpiringAt(context.Context, time.Time, domain.ExpiryConfig) (*time.Time, error) {
	return f.next, nil
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

func TestSweepReportsExpiringBeforeExpired(t *testing.T) {
	fs := &fakeStore{
		out:  []store.ExpiredBatch{{BatchID: 1, ProductID: "p", Qty: 5}},
		expg: []store.ExpiringBatch{{BatchID: 2, ProductID: "q"}},
	}
	var order []string
	j := New(fs, time.Now, func([]store.ExpiredBatch) { order = append(order, "expired") })
	j.Expiry = &domain.ExpiryConfig{Share: 0.2, MinHours: 12, MaxShare: 0.5}
	j.OnExpiring = func(b []store.ExpiringBatch) { order = append(order, "expiring") }
	_, _ = j.Sweep(context.Background())
	_, _ = j.Sweep(context.Background())
	if len(order) != 2 || order[0] != "expiring" || order[1] != "expired" {
		t.Fatalf("order: %v", order)
	}
}

// «Дворник» просыпается на ближайший переход в «истекает», а не ждёт потолка MaxWait.
func TestRunWakesForNextExpiringAt(t *testing.T) {
	soon := time.Now().Add(30 * time.Millisecond)
	fs := &fakeStore{next: &soon}
	j := New(fs, time.Now, nil)
	j.Expiry = &domain.ExpiryConfig{Share: 0.2, MinHours: 12, MaxShare: 0.5}
	j.MinInterval = time.Millisecond
	j.MaxWait = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go j.Run(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for fs.sweeps.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if fs.sweeps.Load() < 2 {
		t.Fatalf("janitor did not wake for expiring transition: %d sweeps", fs.sweeps.Load())
	}
}
