package notify

import (
	"strings"
	"testing"
)

// Столбики по дням — непрозрачные квадраты, нулевой день — прочерк; начало столбика сразу после числа дня.
func TestFormatWeeklyBars(t *testing.T) {
	r := &WeeklyReport{DayMinor: [7]int64{22260, 0, 8520, 40500, 1500, 49200, 311200}, DayLabels: [7]int{9, 10, 11, 12, 13, 14, 15}}
	text := FormatWeekly(r, "")
	for _, want := range []string{"09 🟩 ", "10 — ", "15 🟩🟩🟩🟩🟩🟩🟩🟩 "} {
		if !strings.Contains(text, want) {
			t.Fatalf("нет %q в:\n%s", want, text)
		}
	}
	if strings.Contains(text, "▇") {
		t.Fatalf("остался полупрозрачный символ:\n%s", text)
	}
}
