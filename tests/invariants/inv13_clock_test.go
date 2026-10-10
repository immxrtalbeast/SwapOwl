package invariants

import (
	"context"
	"sync"
	"testing"
	"time"

	"umirhack/internal/clock"
)

// Инвариант 13. Время не идёт назад: последовательные чтения часов не убывают при смене
// скорости, прыжке, паузе и после рестарта.
func TestInv13ClockNeverGoesBack(t *testing.T) {
	ctx := context.Background()
	store := &clock.MemStore{}
	start := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	c, err := clock.NewAccelerated(ctx, store, start, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
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
				cur := c.Now()
				if cur.Before(prev) {
					fail <- cur.Sub(prev).String()
					return
				}
				prev = cur
			}
		}()
	}
	speeds := []float64{600, 0, 3600, 1, 0.5, 0, 100}
	for i := 0; i < 70; i++ {
		var err error
		switch i % 3 {
		case 0:
			err = c.SetSpeed(ctx, speeds[i%len(speeds)])
		case 1:
			_, err = c.Jump(ctx, float64(1+i%24))
		default:
			err = c.Pause(ctx)
		}
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
	select {
	case d := <-fail:
		t.Fatalf("чтение часов дало момент раньше предыдущего на %s", d)
	default:
	}

	// Рестарт: новый экземпляр над тем же хранилищем и событие позже сохранённого времени.
	last := c.Now()
	if err := c.Persist(ctx); err != nil {
		t.Fatal(err)
	}
	event := last.Add(90 * time.Minute)
	c2, err := clock.NewAccelerated(ctx, store, start, event)
	if err != nil {
		t.Fatal(err)
	}
	if now := c2.Now(); now.Before(last) || now.Before(event) {
		t.Fatalf("после рестарта now=%s раньше последнего события %s", now, event)
	}
	// Рестарт без свежего Persist: время не откатывается к сохранённому раньше.
	c3, err := clock.NewAccelerated(ctx, store, start, last)
	if err != nil {
		t.Fatal(err)
	}
	if c3.Now().Before(last) {
		t.Fatalf("после рестарта now=%s раньше последнего события %s", c3.Now(), last)
	}
}
