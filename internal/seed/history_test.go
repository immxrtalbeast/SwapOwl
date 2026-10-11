package seed

import (
	"reflect"
	"testing"
	"time"
)

var testT0 = time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC) // 11:00 по Москве

func TestHistoryDeterministic(t *testing.T) {
	a, err := buildHistory(testT0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildHistory(testT0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("история зависит не только от t0")
	}
	if len(a.orders) < 100 || len(a.batches) < 50 {
		t.Fatalf("история подозрительно мала: заказов %d, партий %d", len(a.orders), len(a.batches))
	}
}

func TestHistoryConsistentWithJournal(t *testing.T) {
	h, err := buildHistory(testT0)
	if err != nil {
		t.Fatal(err)
	}
	left := map[int64]int64{}
	for _, b := range h.batches {
		if !b.ReceivedAt.Before(testT0) || !b.ExpiresAt.After(b.ReceivedAt) {
			t.Fatalf("плохие времена партии %+v", b)
		}
		left[b.ID] = 0
	}
	expiresAt := map[int64]time.Time{}
	for _, b := range h.batches {
		expiresAt[b.ID] = b.ExpiresAt
	}
	for _, m := range h.moves {
		if !m.At.Before(testT0) {
			t.Fatalf("движение не в прошлом: %+v", m)
		}
		switch m.Kind {
		case "receipt":
			left[m.BatchID] += m.Qty
		case "sale":
			left[m.BatchID] -= m.Qty
			if !m.At.Before(expiresAt[m.BatchID]) {
				t.Fatalf("продажа просроченной партии: %+v", m)
			}
		case "writeoff":
			left[m.BatchID] -= m.Qty
			if m.Reason != "expired" || !m.At.Equal(expiresAt[m.BatchID]) {
				t.Fatalf("списание просрочки не в expires_at: %+v", m)
			}
		default:
			t.Fatalf("неизвестный вид движения %q", m.Kind)
		}
	}
	expired := 0
	for _, b := range h.batches {
		if left[b.ID] != b.Remaining || b.Remaining < 0 {
			t.Fatalf("партия %d: по журналу %d, остаток %d", b.ID, left[b.ID], b.Remaining)
		}
		if !b.ExpiresAt.After(testT0) {
			// Партия, проданная целиком до срока, списания не требует.
			if b.Remaining != 0 || (b.WrittenOffAt != nil && !b.WrittenOffAt.Equal(b.ExpiresAt)) {
				t.Fatalf("истёкшая партия не списана: %+v", b)
			}
			if b.WrittenOffAt != nil {
				expired++
			}
		}
	}
	if expired == 0 {
		t.Fatal("в истории нет ни одной списанной по сроку партии")
	}
}
