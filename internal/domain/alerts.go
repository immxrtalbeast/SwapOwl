package domain

import (
	"sort"
	"time"
)

// AlertExpiredWindow — как долго просроченная партия остаётся в ленте предупреждений
// (.ai/solution.md, 7.4: «просроченные за последние 24 ч стенда»).
const AlertExpiredWindow = 24 * time.Hour

// Alert — строка ленты предупреждений.
type Alert struct {
	BatchID   int64
	ProductID string
	// Remaining — что под угрозой: остаток партии, а у просроченной — то, что она потеряла
	// (остаток плюс уже списанное по сроку).
	Remaining Qty
	ExpiresAt time.Time
	TimeLeft  time.Duration // отрицательное у просроченных
	Status    BatchStatus
	// Doomed — до срока меньше wMin рабочих часов: продать уже не успеют (2.6). У просроченной — true.
	Doomed bool
}

// BuildAlerts отбирает партии для ленты: истекающие и просроченные не раньше чем AlertExpiredWindow назад.
// Партии без угрожаемого количества пропускаются. Порядок — (ExpiresAt, ID): просроченные первыми.
func BuildAlerts(batches []Batch, now time.Time, cfg ExpiryConfig, sch Schedule, wMin float64) []Alert {
	out := []Alert{}
	for _, b := range batches {
		if b.Remaining <= 0 {
			continue
		}
		st := Status(b, now, cfg)
		switch st {
		case StatusOK:
			continue
		case StatusExpired:
			if now.Sub(b.ExpiresAt) > AlertExpiredWindow {
				continue
			}
		}
		out = append(out, Alert{
			BatchID: b.ID, ProductID: b.ProductID, Remaining: b.Remaining, ExpiresAt: b.ExpiresAt,
			TimeLeft: b.ExpiresAt.Sub(now), Status: st,
			Doomed: WorkingHours(now, b.ExpiresAt, sch) < wMin,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].ExpiresAt.Equal(out[j].ExpiresAt) {
			return out[i].ExpiresAt.Before(out[j].ExpiresAt)
		}
		return out[i].BatchID < out[j].BatchID
	})
	return out
}

// ShouldNotifyExpiring — нужно ли послать batch.expiring: партия в статусе «истекает» на now.
// Партия, ставшая просроченной минуя «истекает», сигнала не получает (2.4).
func ShouldNotifyExpiring(b Batch, now time.Time, cfg ExpiryConfig) bool {
	return b.Remaining > 0 && Status(b, now, cfg) == StatusExpiring
}
