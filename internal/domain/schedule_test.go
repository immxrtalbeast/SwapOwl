package domain

import (
	"strings"
	"testing"
	"time"
)

func mustSchedule(t *testing.T, loc *time.Location, s string) Schedule {
	t.Helper()
	days, err := ParseSchedule(s)
	if err != nil {
		t.Fatal(err)
	}
	return Schedule{Location: loc, Days: days}
}

func TestWorkingHoursDays(t *testing.T) {
	msk := time.FixedZone("msk", 3*3600)
	sch := mustSchedule(t, msk, "mon-thu 10:00-23:00; fri-sat 11:00-02:00; sun 11:00-22:00")
	// 2026-01-05 — понедельник.
	at := func(d, h, m int) time.Time { return time.Date(2026, 1, d, h, m, 0, 0, msk) }
	cases := []struct {
		name     string
		from, to time.Time
		want     float64
	}{
		{"обычный день", at(5, 0, 0), at(6, 0, 0), 13},
		{"через полночь: пятница 11:00–02:00", at(9, 0, 0), at(10, 12, 0), 0}, // пт 11–24 = 13, хвост до 02:00 = 2, сб 11–12 = 1
		{"хвост после полуночи относится к дню открытия", at(10, 0, 0), at(10, 3, 0), 2},
		{"старт посреди ночного интервала", at(10, 1, 0), at(10, 11, 0), 1},
		{"суббота 11:00 — новый интервал", at(10, 1, 0), at(10, 13, 0), 3},
		{"выходной нет: воскресенье до 02:00 нет хвоста", at(11, 0, 0), at(11, 11, 0), 2},
		{"пустой интервал", at(5, 12, 0), at(5, 12, 0), 0},
	}
	cases[1].want = 16
	for _, c := range cases {
		if got := WorkingHours(c.from, c.to, sch); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestWorkingHoursDayOff(t *testing.T) {
	sch := mustSchedule(t, time.UTC, "mon-fri 09:00-18:00")
	// 2026-01-10 — суббота, 11-е — воскресенье.
	got := WorkingHours(time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC), sch)
	if got != 0 {
		t.Fatalf("выходные: %v", got)
	}
	// Пятница 09:00 — понедельник 18:00 = 9 + 9.
	got = WorkingHours(time.Date(2026, 1, 9, 9, 0, 0, 0, time.UTC), time.Date(2026, 1, 12, 18, 0, 0, 0, time.UTC), sch)
	if got != 18 {
		t.Fatalf("пт–пн: %v", got)
	}
}

func TestWorkingHoursBerlinDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip(err)
	}
	// Зимнее время: 2026-10-25 (воскресенье) в 03:00 часы переводят на 02:00, сутки 25 ч.
	sch := mustSchedule(t, loc, "sat-sun 20:00-04:00")
	from := time.Date(2026, 10, 24, 0, 0, 0, 0, loc)
	to := time.Date(2026, 10, 26, 0, 0, 0, 0, loc)
	// Сб 20:00 → вс 04:00: 8 ч настенных, на деле 9 ч; вс 20:00 → пн 00:00 (конец окна) = 4 ч.
	if got := WorkingHours(from, to, sch); got != 13 {
		t.Fatalf("осень: %v, want 13", got)
	}
	// Летнее время: 2026-03-29 (воскресенье) 02:00 → 03:00, сутки 23 ч. Сб 20:00 → вс 04:00 = 7 ч, вс 20:00 → пн 00:00 = 4 ч.
	from = time.Date(2026, 3, 28, 0, 0, 0, 0, loc)
	to = time.Date(2026, 3, 30, 0, 0, 0, 0, loc)
	if got := WorkingHours(from, to, sch); got != 11 {
		t.Fatalf("весна: %v, want 11", got)
	}
	// Дневной интервал в день перехода сохраняет настенные часы.
	day := mustSchedule(t, loc, "sun 10:00-12:00")
	if got := WorkingHours(time.Date(2026, 10, 25, 0, 0, 0, 0, loc), time.Date(2026, 10, 26, 0, 0, 0, 0, loc), day); got != 2 {
		t.Fatalf("10–12 в день перехода: %v", got)
	}
}

func TestWorkingHoursLegacyUnchanged(t *testing.T) {
	sch := Schedule{Location: time.UTC, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour}
	got := WorkingHours(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC), sch)
	if got != 26 {
		t.Fatalf("got %v", got)
	}
}

func TestParseSchedule(t *testing.T) {
	good := []string{
		"mon 10:00-22:00",
		"MON-SUN 00:00-23:59",
		"fri-mon 18:00-02:00",
		"mon 10:00-14:00; mon 17:00-23:00",
		"mon 10:00-02:00; tue 03:00-10:00", // вплотную к концу ночного интервала
		"",
	}
	for _, s := range good {
		if _, err := ParseSchedule(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	bad := map[string]string{
		"xyz 10:00-22:00":                  "unknown day",
		"mon 10:00":                        "HH:MM-HH:MM",
		"mon 10:00-24:00":                  "HH:MM",
		"mon 10:00-10:00":                  "(0, 24h)", // 24 ч
		"mon 10:00-14:00; mon 13:00-20:00": "overlap",
		"mon 20:00-02:00; tue 01:00-10:00": "overlap", // хвост ночи пересекает вторник
		"sun 20:00-02:00; mon 01:00-10:00": "overlap", // через границу недели
		"mon":                              "want",
		"mon-tue-wed 10:00-12:00":          "bad days",
		"mon 10:00-12:00;;":                "want",
	}
	for s, want := range bad {
		_, err := ParseSchedule(s)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err=%v, want %q", s, err, want)
		}
	}
}

func TestShiftEnd(t *testing.T) {
	days := map[time.Weekday][]Interval{
		time.Friday:   {{Open: 11 * time.Hour, Close: 26 * time.Hour}},
		time.Saturday: {{Open: 11 * time.Hour, Close: 26 * time.Hour}},
	}
	sch := Schedule{Location: time.UTC, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour, Days: days}
	d := func(day, h int) time.Time { return time.Date(2026, 10, day, h, 0, 0, 0, time.UTC) }
	cases := []struct{ now, want time.Time }{
		{d(9, 22), d(10, 2)}, // пятница, в смене
		{d(10, 1), d(10, 2)}, // суббота 01:00 — пятничная смена
		{d(9, 9), d(10, 2)},  // до открытия: последняя смена дня
		{d(10, 3), d(11, 2)}, // между сменами: суббота ещё впереди
		{d(11, 3), d(11, 3)}, // воскресенье — выходной
	}
	lunch := Schedule{Location: time.UTC, Days: map[time.Weekday][]Interval{
		time.Monday: {{Open: 11 * time.Hour, Close: 15 * time.Hour}, {Open: 18 * time.Hour, Close: 23 * time.Hour}},
	}}
	for _, h := range []int{11, 12, 16, 19} { // 2026-10-12 — понедельник
		if got, want := ShiftEnd(time.Date(2026, 10, 12, h, 0, 0, 0, time.UTC), lunch), time.Date(2026, 10, 12, 23, 0, 0, 0, time.UTC); !got.Equal(want) {
			t.Errorf("lunch break %d:00: got %v want %v", h, got, want)
		}
	}
	for _, c := range cases {
		if got := ShiftEnd(c.now, sch); !got.Equal(c.want) {
			t.Errorf("now=%v: got %v want %v", c.now, got, c.want)
		}
	}
}
