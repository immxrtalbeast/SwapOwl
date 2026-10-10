package simulator

import (
	"sort"
	"time"

	"umirhack/internal/domain"
)

// warehouse — склад в памяти: партии с остатками, FEFO по (expires_at, id).
type warehouse struct {
	batches []domain.Batch
	nextID  int64
}

func (w *warehouse) receive(product string, qty domain.Qty, at time.Time, shelf time.Duration) {
	if qty <= 0 {
		return
	}
	w.nextID++
	w.batches = append(w.batches, domain.Batch{
		ID: w.nextID, ProductID: product, Quantity: qty, Remaining: qty,
		ReceivedAt: at, ExpiresAt: at.Add(shelf),
	})
}

// expire списывает партии, срок которых наступил к моменту now (expires_at <= now).
func (w *warehouse) expire(now time.Time, visit func(domain.Batch)) {
	kept := w.batches[:0]
	for _, b := range w.batches {
		if b.Remaining > 0 && !now.Before(b.ExpiresAt) {
			visit(b)
			continue
		}
		if b.Remaining > 0 {
			kept = append(kept, b)
		}
	}
	w.batches = kept
}

// available — непросроченные партии с остатком.
func (w *warehouse) available(now time.Time) []domain.Batch {
	var out []domain.Batch
	for _, b := range w.batches {
		if b.Remaining > 0 && now.Before(b.ExpiresAt) {
			out = append(out, b)
		}
	}
	return out
}

func (w *warehouse) stock(now time.Time) map[string]domain.Qty {
	s := map[string]domain.Qty{}
	for _, b := range w.available(now) {
		s[b.ProductID] += b.Remaining
	}
	return s
}

// consume атомарно списывает потребность по FEFO; при нехватке не меняет склад.
func (w *warehouse) consume(needs map[string]domain.Qty, now time.Time) bool {
	stock := w.stock(now)
	for p, q := range needs {
		if stock[p] < q {
			return false
		}
	}
	idx := make([]int, 0, len(w.batches))
	for i, b := range w.batches {
		if b.Remaining > 0 && now.Before(b.ExpiresAt) {
			idx = append(idx, i)
		}
	}
	sort.Slice(idx, func(a, b int) bool {
		x, y := w.batches[idx[a]], w.batches[idx[b]]
		if !x.ExpiresAt.Equal(y.ExpiresAt) {
			return x.ExpiresAt.Before(y.ExpiresAt)
		}
		return x.ID < y.ID
	})
	left := map[string]domain.Qty{}
	for p, q := range needs {
		left[p] = q
	}
	for _, i := range idx {
		b := &w.batches[i]
		l := left[b.ProductID]
		if l <= 0 {
			continue
		}
		take := l
		if b.Remaining < take {
			take = b.Remaining
		}
		b.Remaining -= take
		left[b.ProductID] = l - take
	}
	return true
}
