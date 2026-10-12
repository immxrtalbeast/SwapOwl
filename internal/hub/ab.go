package hub

import (
	"context"
	"net/url"
	"sync"
	"time"
)

// GroupAB — группа точек для живого A/B-сравнения (docs/network.md): ровно две точки.
const GroupAB = "ab"

// maxCatchUp — сколько часовых запросов за один опрос точки; остальное дочитается на следующих (длинный прогон).
const maxCatchUp = 24

// recheckHours — сколько последних закрытых часов перечитывается на каждом опросе. «Дворник» списывает просрочку
// после окна опоздания (LATE_EVENTS_HOURS, 2 ч по умолчанию), а движение датирует моментом expires_at — уже
// закрытым часом. Прочитанный один раз час остался бы нулевым навсегда.
const recheckHours = 4

// SeriesPoint — накопленные списания с начала прогона на момент Hour (часы стенда с начала прогона).
type SeriesPoint struct {
	Hour      float64 `json:"hour"`
	CostMinor int64   `json:"cost_minor"`
}

// Run — прогон точки группы ab: ряд «списания в ₽ по часам стенда» с его начала.
type Run struct {
	Running    bool          `json:"running"` // генератор заказов идёт
	StartedAt  time.Time     `json:"started_at"`
	StandTime  time.Time     `json:"stand_time"`
	TotalMinor int64         `json:"total_minor"` // списано с начала прогона, копейки
	Series     []SeriesPoint `json:"series"`
}

// abRun — накопленное состояние прогона. Закрытые часы не меняются, поэтому читаются один раз.
type abRun struct {
	started time.Time // начало прогона, выровнено по часу стенда
	closed  []int64   // списания по закрытым часам, копейки
	partial int64     // списания в текущем (неполном) часе
	caught  bool      // все закрытые часы прочитаны, partial актуален
	now     time.Time
	running bool
}

func (r *abRun) snapshot() *Run {
	out := &Run{Running: r.running, StartedAt: r.started, StandTime: r.now, Series: []SeriesPoint{{Hour: 0}}}
	var cum int64
	for i, v := range r.closed {
		cum += v
		out.Series = append(out.Series, SeriesPoint{Hour: float64(i + 1), CostMinor: cum})
	}
	if r.caught && r.now.After(r.started.Add(time.Duration(len(r.closed))*time.Hour)) {
		cum += r.partial
		h := r.now.Sub(r.started).Hours()
		out.Series = append(out.Series, SeriesPoint{Hour: h, CostMinor: cum})
	}
	out.TotalMinor = cum
	return out
}

// withRun подмешивает ряд прогона в снимок точки группы ab (только для точки на связи).
func (c *Collector) withRun(st PointState) PointState {
	if r := c.runs[st.ID]; r != nil && st.Status == StatusOK {
		st.Run = r.snapshot()
	}
	return st
}

// RunAB опрашивает точки группы ab каждые interval до отмены ctx. Без таких точек сразу возвращается.
func (c *Collector) RunAB(ctx context.Context, interval time.Duration) {
	if len(c.abPoints()) == 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		c.PollAB(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (c *Collector) abPoints() []Point {
	var out []Point
	for _, p := range c.points {
		if p.Group == GroupAB {
			out = append(out, p)
		}
	}
	return out
}

// PollAB обновляет ряды списаний точек группы ab параллельно.
func (c *Collector) PollAB(ctx context.Context) {
	var wg sync.WaitGroup
	for _, p := range c.abPoints() {
		wg.Add(1)
		go func(p Point) {
			defer wg.Done()
			c.pollRun(ctx, p)
		}(p)
	}
	wg.Wait()
}

func (c *Collector) writeoffs(ctx context.Context, p Point, from, to time.Time) (int64, error) {
	var rep struct {
		TotalMinor int64 `json:"total_minor"`
	}
	err := c.get(ctx, p, "/api/analytics/writeoffs", url.Values{
		"from": {from.UTC().Format(time.RFC3339)}, "to": {to.UTC().Format(time.RFC3339)}}, &rep)
	return rep.TotalMinor, err
}

// pollRun: прогон начинается, когда впервые замечен идущий генератор (начало — час стенда этого момента);
// часы стенда назад (сброс) или переход генератора «стоял → идёт» (ab-start всегда делает сброс, а пауза
// не возобновляется) начинают прогон заново — сброс в другой день уводит часы вперёд. Ошибка точки оставляет прошлый ряд.
func (c *Collector) pollRun(ctx context.Context, p Point) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var clk struct {
		Now     time.Time `json:"now"`
		Running bool      `json:"simulator_running"`
	}
	if err := c.get(ctx, p, "/api/clock", nil, &clk); err != nil {
		return
	}
	c.mu.RLock()
	prev := c.runs[p.ID]
	c.mu.RUnlock()

	var r abRun
	switch {
	case prev != nil && !clk.Now.Before(prev.now) && !(clk.Running && !prev.running):
		r = *prev
		r.closed = append([]int64(nil), prev.closed...)
	case clk.Running:
		r = abRun{started: clk.Now.Truncate(time.Hour)}
	default:
		c.mu.Lock()
		delete(c.runs, p.ID) // сброс стенда: прогона нет, пока не запустят генератор
		c.mu.Unlock()
		return
	}
	r.now, r.running = clk.Now, clk.Running

	full := int(clk.Now.Sub(r.started) / time.Hour)
	for n := 0; len(r.closed) < full && n < maxCatchUp; n++ {
		from := r.started.Add(time.Duration(len(r.closed)) * time.Hour)
		v, err := c.writeoffs(ctx, p, from, from.Add(time.Hour))
		if err != nil {
			return
		}
		r.closed = append(r.closed, v)
	}
	r.caught = len(r.closed) == full
	if r.caught {
		for i := max(0, len(r.closed)-recheckHours); i < len(r.closed); i++ {
			from := r.started.Add(time.Duration(i) * time.Hour)
			v, err := c.writeoffs(ctx, p, from, from.Add(time.Hour))
			if err != nil {
				return
			}
			r.closed[i] = v
		}
	}
	if r.caught {
		from := r.started.Add(time.Duration(full) * time.Hour)
		r.partial = 0
		if clk.Now.After(from) {
			v, err := c.writeoffs(ctx, p, from, clk.Now)
			if err != nil {
				return
			}
			r.partial = v
		}
	}
	c.mu.Lock()
	c.runs[p.ID] = &r
	c.mu.Unlock()
}
