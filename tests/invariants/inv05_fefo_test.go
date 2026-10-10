package invariants

import (
	"math/rand"
	"sort"
	"testing"
	"time"

	"umirhack/internal/domain"
)

var t0 = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

// Инвариант 5. FEFO: списание идёт с партии с ближайшим expires_at, при равенстве — с меньшим id;
// следующая партия трогается только после исчерпания предыдущей.
func TestInv05FEFOTable(t *testing.T) {
	batches := []domain.Batch{
		{ID: 3, ProductID: "p", Quantity: 100, Remaining: 100, ExpiresAt: t0.Add(2 * time.Hour)},
		{ID: 1, ProductID: "p", Quantity: 100, Remaining: 100, ExpiresAt: t0.Add(5 * time.Hour)},
		{ID: 2, ProductID: "p", Quantity: 100, Remaining: 100, ExpiresAt: t0.Add(2 * time.Hour)},
	}
	plan, short := domain.PlanWriteoff([]domain.Need{{ProductID: "p", Qty: 150}}, batches)
	if len(short) != 0 {
		t.Fatalf("неожиданная нехватка: %+v", short)
	}
	want := []domain.Writeoff{{BatchID: 2, ProductID: "p", Qty: 100}, {BatchID: 3, ProductID: "p", Qty: 50}}
	if len(plan) != len(want) {
		t.Fatalf("план %+v, ожидался %+v", plan, want)
	}
	for i := range want {
		if plan[i] != want[i] {
			t.Fatalf("план %+v, ожидался %+v", plan, want)
		}
	}

	_, short = domain.PlanWriteoff([]domain.Need{{ProductID: "p", Qty: 301}}, batches)
	if len(short) != 1 || short[0].Missing != 1 || short[0].Available != 300 || short[0].Required != 301 {
		t.Fatalf("нехватка %+v", short)
	}
}

func TestInv05FEFOProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	for iter := 0; iter < 2000; iter++ {
		var batches []domain.Batch
		for i := 0; i < 1+rng.Intn(8); i++ {
			q := domain.Qty(1 + rng.Intn(500))
			batches = append(batches, domain.Batch{ID: int64(i + 1), ProductID: "p", Quantity: q, Remaining: q,
				ExpiresAt: t0.Add(time.Duration(1+rng.Intn(6)) * time.Hour)})
		}
		rng.Shuffle(len(batches), func(i, j int) { batches[i], batches[j] = batches[j], batches[i] })
		need := domain.Qty(1 + rng.Intn(1500))
		plan, short := domain.PlanWriteoff([]domain.Need{{ProductID: "p", Qty: need}}, batches)

		var total domain.Qty
		for _, b := range batches {
			total += b.Remaining
		}
		if need > total {
			if len(plan) != 0 || len(short) != 1 {
				t.Fatalf("итер %d: при нехватке нужен пустой план и одна нехватка, получено %+v %+v", iter, plan, short)
			}
			continue
		}
		if len(short) != 0 {
			t.Fatalf("итер %d: лишняя нехватка %+v", iter, short)
		}
		taken := map[int64]domain.Qty{}
		var sum domain.Qty
		for _, w := range plan {
			taken[w.BatchID] += w.Qty
			sum += w.Qty
		}
		if sum != need {
			t.Fatalf("итер %d: списано %d, нужно %d", iter, sum, need)
		}
		sorted := append([]domain.Batch(nil), batches...)
		sort.Slice(sorted, func(i, j int) bool {
			if !sorted[i].ExpiresAt.Equal(sorted[j].ExpiresAt) {
				return sorted[i].ExpiresAt.Before(sorted[j].ExpiresAt)
			}
			return sorted[i].ID < sorted[j].ID
		})
		last := -1
		for i, b := range sorted {
			if taken[b.ID] > b.Remaining {
				t.Fatalf("итер %d: из партии %d взято больше остатка", iter, b.ID)
			}
			if taken[b.ID] > 0 {
				last = i
			}
		}
		for i := 0; i < last; i++ {
			if sorted[i].Remaining-taken[sorted[i].ID] != 0 {
				t.Fatalf("итер %d: партия %d раньше последней затронутой не исчерпана", iter, sorted[i].ID)
			}
		}
	}
}
