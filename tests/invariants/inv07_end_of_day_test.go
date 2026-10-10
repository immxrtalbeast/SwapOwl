package invariants

import (
	"encoding/json"
	"testing"
	"time"
)

// Инвариант 7. Конец дня: «годен до даты D» = доступно до начала дня D+1 по времени ресторана,
// включая переходы на летнее и зимнее время. Для проверки нужен запущенный стенд (PostgreSQL),
// потому что расчёт expires_at живёт в приёме поставки (docs/api.md): отдельной доменной функции в контрактах нет.
func TestInv07EndOfDay(t *testing.T) {
	for _, tc := range []struct {
		zone, day string
	}{
		{"Europe/Moscow", "2026-10-12"},
		{"Europe/Berlin", "2025-03-29"},
		{"Europe/Berlin", "2025-03-30"}, // переход на летнее время
		{"Europe/Berlin", "2025-10-25"},
		{"Europe/Berlin", "2025-10-26"}, // переход на зимнее время
		{"Europe/Berlin", "2027-03-27"},
		{"Europe/Berlin", "2027-03-28"},
		{"Europe/Berlin", "2027-10-30"},
		{"Europe/Berlin", "2027-10-31"},
	} {
		t.Run(tc.zone+" "+tc.day, func(t *testing.T) {
			s := newStand(t, standOpts{TZ: tc.zone})
			s.pause()
			loc, _ := time.LoadLocation(tc.zone)
			d, _ := time.ParseInLocation("2006-01-02", tc.day, loc)
			wantExpires := time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, loc)

			st, body := s.do("POST", "/api/deliveries", map[string]any{
				"id": "D-" + tc.zone + tc.day, "number": "1", "created_at": time.Now().Format(time.RFC3339),
				"items": []map[string]any{{"product_id": "cream", "name": "Сливки", "unit": "g",
					"quantity": "600", "expires_on": tc.day}},
			})
			if st != 201 {
				t.Fatalf("поставка: %d %s", st, body)
			}
			var resp struct {
				Batches []struct {
					ExpiresAt time.Time `json:"expires_at"`
				} `json:"batches"`
			}
			if err := json.Unmarshal(body, &resp); err != nil || len(resp.Batches) != 1 {
				t.Fatalf("ответ поставки: %v %s", err, body)
			}
			got := resp.Batches[0].ExpiresAt
			if !got.Equal(wantExpires) {
				t.Fatalf("expires_at = %s, ожидалось начало дня D+1: %s", got, wantExpires)
			}
			// Последний момент дня D строго раньше границы, первый момент D+1 — не раньше.
			if !wantExpires.Add(-time.Millisecond).Before(got) || got.After(wantExpires) {
				t.Fatalf("граница %s не отделяет 23:59:59.999 дня D от 00:00:00 дня D+1", got)
			}
		})
	}
}
