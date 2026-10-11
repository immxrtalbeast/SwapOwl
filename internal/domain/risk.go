package domain

import (
	"math"
	"sort"
	"time"
)

// DoomedMinHours — W_min: партия с меньшим числом рабочих часов до срока «обречённая»
// (значение HOT_W_MIN_HOURS по умолчанию; сигнатура RiskByBatch порога не принимает).
const DoomedMinHours = 0.5

// RiskByBatch — проекция FEFO по партиям одного продукта в порядке (ExpiresAt, ID):
// cap = rate·W(now, e) − used; use = min(remaining, max(cap, 0)); risk = remaining − use.
// Обречённая партия (W(now, e) < DoomedMinHours) в проекцию не входит: risk = remaining.
// Результат идёт в порядке FEFO. Дробный cap округляется вниз, то есть риск — вверх.
func RiskByBatch(batches []Batch, ratePerHour float64, now time.Time, sch Schedule) []BatchRisk {
	sorted := make([]Batch, len(batches))
	copy(sorted, batches)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].ExpiresAt.Equal(sorted[j].ExpiresAt) {
			return sorted[i].ExpiresAt.Before(sorted[j].ExpiresAt)
		}
		return sorted[i].ID < sorted[j].ID
	})
	out := make([]BatchRisk, 0, len(sorted))
	used := 0.0
	for _, b := range sorted {
		w := 0.0
		if b.ExpiresAt.After(now) {
			w = WorkingHours(now, b.ExpiresAt, sch)
		}
		r := BatchRisk{BatchID: b.ID, Risk: b.Remaining, WorkingHoursLeft: w}
		if w < DoomedMinHours {
			r.Doomed = true
			out = append(out, r)
			continue
		}
		use := Qty(0)
		if c := math.Floor(ratePerHour*w - used); c > 0 {
			if c >= float64(b.Remaining) {
				use = b.Remaining
			} else {
				use = Qty(c)
			}
		}
		r.Risk = b.Remaining - use
		used += float64(use)
		out = append(out, r)
	}
	return out
}
