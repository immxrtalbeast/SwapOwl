package domain

import (
	"testing"
	"time"
)

var cfg = ExpiryConfig{Share: 0.2, MinHours: 12, MaxShare: 0.5}

func batchLife(recv time.Time, life time.Duration) Batch {
	return Batch{ReceivedAt: recv, ExpiresAt: recv.Add(life)}
}

func TestStatusBoundaries(t *testing.T) {
	recv := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		life time.Duration
		left time.Duration // сколько осталось на момент now
		want BatchStatus
	}{
		{"fresh", 100 * time.Hour, 80 * time.Hour, StatusOK},
		{"threshold exact is still ok (20h=20%)", 100 * time.Hour, 20 * time.Hour, StatusOK},
		{"just under 20%, above 12h", 100 * time.Hour, 20*time.Hour - time.Second, StatusExpiring},
		{"min 12h wins over 20% of 24h", 24 * time.Hour, 12 * time.Hour, StatusOK},
		{"min 12h: under", 24 * time.Hour, 12*time.Hour - time.Second, StatusExpiring},
		{"cap 50% for 10h life: ok at 5h", 10 * time.Hour, 5 * time.Hour, StatusOK},
		{"cap 50% for 10h life: expiring under 5h", 10 * time.Hour, 5*time.Hour - time.Second, StatusExpiring},
		{"10h life not expiring right after receipt", 10 * time.Hour, 10 * time.Hour, StatusOK},
		{"expired at exactly expires_at", 10 * time.Hour, 0, StatusExpired},
		{"expired after", 10 * time.Hour, -time.Minute, StatusExpired},
	}
	for _, c := range cases {
		b := batchLife(recv, c.life)
		now := b.ExpiresAt.Add(-c.left)
		if got := Status(b, now, cfg); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestExpiringAtMatchesStatus(t *testing.T) {
	b := batchLife(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), 100*time.Hour)
	at := ExpiringAt(b, cfg)
	if Status(b, at, cfg) != StatusOK || Status(b, at.Add(time.Nanosecond), cfg) != StatusExpiring {
		t.Fatalf("transition at %v", at)
	}
}

func TestExpiresAtEndOfDay(t *testing.T) {
	msk, _ := time.LoadLocation("Europe/Moscow")
	ber, _ := time.LoadLocation("Europe/Berlin")
	if got, want := ExpiresAtEndOfDay(2026, 10, 12, msk), time.Date(2026, 10, 12, 21, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("msk: %v want %v", got, want)
	}
	// Берлин: 24–25 октября 2026 — переход на зимнее время (последнее воскресенье, 25-е).
	if got, want := ExpiresAtEndOfDay(2026, 10, 25, ber), time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("berlin DST day: %v want %v", got, want)
	}
	if got, want := ExpiresAtEndOfDay(2026, 10, 24, ber), time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("berlin before DST: %v want %v", got, want)
	}
	// Конец месяца и года.
	if got, want := ExpiresAtEndOfDay(2026, 12, 31, msk), time.Date(2026, 12, 31, 21, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("year end: %v want %v", got, want)
	}
}
