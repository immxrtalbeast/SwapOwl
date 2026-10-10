package domain

import "time"

// ExpiringThreshold — порог «истекает» для партии: min(max(Share·срок, MinHours), MaxShare·срок),
// срок = expires_at − received_at (.ai/solution.md, 2.4).
func ExpiringThreshold(b Batch, cfg ExpiryConfig) time.Duration {
	total := float64(b.ExpiresAt.Sub(b.ReceivedAt))
	hours := cfg.MinHours * float64(time.Hour)
	t := cfg.Share * total
	if t < hours {
		t = hours
	}
	if upper := cfg.MaxShare * total; upper < t {
		t = upper
	}
	return time.Duration(t)
}

// Status возвращает статус партии на момент now. Граница строгая: при now == expires_at
// партия уже просрочена, при осталось == порог — ещё годна.
func Status(b Batch, now time.Time, cfg ExpiryConfig) BatchStatus {
	if !now.Before(b.ExpiresAt) {
		return StatusExpired
	}
	if b.ExpiresAt.Sub(now) < ExpiringThreshold(b, cfg) {
		return StatusExpiring
	}
	return StatusOK
}

// ExpiringAt — момент перехода партии в «истекает»: expires_at − порог.
func ExpiringAt(b Batch, cfg ExpiryConfig) time.Time {
	return b.ExpiresAt.Add(-ExpiringThreshold(b, cfg))
}

// ExpiresAtEndOfDay — expires_at для «годен до даты D»: начало дня D+1 по времени ресторана.
// Дата берётся по календарю loc, поэтому длина суток при переходе на летнее время не важна.
// Зоны с переводом часов в полночь не поддерживаются (.ai/solution.md, 2.4).
func ExpiresAtEndOfDay(year int, month time.Month, day int, loc *time.Location) time.Time {
	return time.Date(year, month, day+1, 0, 0, 0, 0, loc).UTC()
}
