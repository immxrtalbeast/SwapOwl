package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Interval — интервал работы в день открытия: Open и Close — смещения от начала суток
// дня открытия. Close > 24 ч означает, что интервал заканчивается после полуночи
// (11:00–02:00 → Open 11 ч, Close 26 ч). Всегда 0 < Close − Open < 24 ч.
type Interval struct {
	Open, Close time.Duration
}

const (
	week = 7 * day
)

var weekdayNames = map[string]time.Weekday{
	"mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday,
	"fri": time.Friday, "sat": time.Saturday, "sun": time.Sunday,
}

// ParseSchedule строго разбирает SCHEDULE: `mon-thu 10:00-23:00; fri-sat 11:00-02:00; sun 11:00-22:00`.
// Группа — дни (`mon` или диапазон `mon-thu`, диапазон может переходить через воскресенье)
// и интервал HH:MM-HH:MM; если конец не позже начала, интервал идёт через полночь.
// Несколько интервалов в один день — несколькими группами. Пустая строка — nil.
// Ошибка: неизвестный день или формат, длина не в (0, 24 ч), пересечение интервалов.
func ParseSchedule(s string) (map[time.Weekday][]Interval, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	days := map[time.Weekday][]Interval{}
	for _, group := range strings.Split(s, ";") {
		f := strings.Fields(group)
		if len(f) != 2 {
			return nil, fmt.Errorf("schedule group %q: want `<days> HH:MM-HH:MM`", strings.TrimSpace(group))
		}
		wds, err := parseDays(f[0])
		if err != nil {
			return nil, fmt.Errorf("schedule group %q: %w", strings.TrimSpace(group), err)
		}
		iv, err := parseInterval(f[1])
		if err != nil {
			return nil, fmt.Errorf("schedule group %q: %w", strings.TrimSpace(group), err)
		}
		for _, wd := range wds {
			days[wd] = append(days[wd], iv)
		}
	}
	if err := ValidateDays(days); err != nil {
		return nil, err
	}
	return days, nil
}

func parseDays(s string) ([]time.Weekday, error) {
	parts := strings.Split(strings.ToLower(s), "-")
	if len(parts) > 2 {
		return nil, fmt.Errorf("bad days %q", s)
	}
	from, ok := weekdayNames[parts[0]]
	if !ok {
		return nil, fmt.Errorf("unknown day %q", parts[0])
	}
	to := from
	if len(parts) == 2 {
		if to, ok = weekdayNames[parts[1]]; !ok {
			return nil, fmt.Errorf("unknown day %q", parts[1])
		}
	}
	out := []time.Weekday{from}
	for d := from; d != to; {
		d = (d + 1) % 7
		out = append(out, d)
	}
	return out, nil
}

func parseInterval(s string) (Interval, error) {
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return Interval{}, fmt.Errorf("bad interval %q: want HH:MM-HH:MM", s)
	}
	open, err := parseClock(a)
	if err != nil {
		return Interval{}, err
	}
	closeAt, err := parseClock(b)
	if err != nil {
		return Interval{}, err
	}
	if closeAt <= open {
		closeAt += day
	}
	return Interval{Open: open, Close: closeAt}, nil
}

func parseClock(s string) (time.Duration, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

// ValidateDays проверяет расписание: 0 < длина < 24 ч, Open в пределах суток,
// интервалы (в том числе перетекающие за полночь в следующий день) не пересекаются.
func ValidateDays(days map[time.Weekday][]Interval) error {
	type abs struct{ a, b time.Duration }
	var all []abs
	for wd, ivs := range days {
		for _, iv := range ivs {
			if iv.Open < 0 || iv.Open >= day {
				return fmt.Errorf("%s: open must be within the day", wd)
			}
			if d := iv.Close - iv.Open; d <= 0 || d >= day {
				return fmt.Errorf("%s: interval length must be in (0, 24h), got %s", wd, d)
			}
			base := time.Duration(wd) * day
			all = append(all, abs{base + iv.Open, base + iv.Close})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].a < all[j].a })
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			// j начинается не раньше i; проверяем и сдвиг на неделю (воскресенье → понедельник).
			if all[j].a < all[i].b || all[i].a+week < all[j].b {
				return fmt.Errorf("schedule intervals overlap")
			}
		}
	}
	return nil
}

// WorkingHours — W(from, to): рабочие часы ресторана в интервале (дробное число).
// Сутки считаются в часовом поясе расписания (nil — UTC), границы интервалов — по
// настенным часам, поэтому на переходе на зимнее/летнее время сутки не 24 ч.
// Интервал через полночь относится к дню открытия; выходной — нет интервала.
func WorkingHours(from, to time.Time, sch Schedule) float64 {
	if !to.After(from) {
		return 0
	}
	intervals := func(wd time.Weekday) []Interval {
		if len(sch.Days) > 0 {
			return sch.Days[wd]
		}
		if sch.CloseAt <= sch.OpenAt {
			return nil
		}
		return []Interval{{sch.OpenAt, sch.CloseAt}}
	}
	loc := sch.Location
	if loc == nil {
		loc = time.UTC
	}
	from, to = from.In(loc), to.In(loc)
	total := 0.0
	// День перед from нужен для интервала, начавшегося вчера и идущего через полночь.
	d := time.Date(from.Year(), from.Month(), from.Day()-1, 0, 0, 0, 0, loc)
	for ; d.Before(to); d = d.AddDate(0, 0, 1) {
		for _, iv := range intervals(d.Weekday()) {
			a := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, int(iv.Open/time.Second), 0, loc)
			b := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, int(iv.Close/time.Second), 0, loc)
			if a.Before(from) {
				a = from
			}
			if b.After(to) {
				b = to
			}
			if b.After(a) {
				total += b.Sub(a).Hours()
			}
		}
	}
	return total
}

// ShiftEnd — конец текущей смены: Close интервала, содержащего now (в том числе начавшегося вчера
// и идущего через полночь), иначе конец последнего интервала открытия сегодняшнего дня после now.
// Если больше нет рабочего времени сегодня — now. Выбор интервалов тот же, что в WorkingHours.
func ShiftEnd(now time.Time, sch Schedule) time.Time {
	loc := sch.Location
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	intervals := func(wd time.Weekday) []Interval {
		if len(sch.Days) > 0 {
			return sch.Days[wd]
		}
		if sch.CloseAt <= sch.OpenAt {
			return nil
		}
		return []Interval{{sch.OpenAt, sch.CloseAt}}
	}
	at := func(d time.Time, off time.Duration) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, int(off/time.Second), 0, loc)
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	for _, d := range []time.Time{today.AddDate(0, 0, -1), today} {
		for _, iv := range intervals(d.Weekday()) {
			if a, b := at(d, iv.Open), at(d, iv.Close); !now.Before(a) && now.Before(b) {
				// Перерыв внутри дня не обрывает смену: берём конец последнего интервала того же дня.
				for _, o := range intervals(d.Weekday()) {
					if e := at(d, o.Close); e.After(b) {
						b = e
					}
				}
				return b
			}
		}
	}
	end := now
	for _, iv := range intervals(today.Weekday()) {
		if a, b := at(today, iv.Open), at(today, iv.Close); a.After(now) && b.After(end) {
			end = b
		}
	}
	return end
}
