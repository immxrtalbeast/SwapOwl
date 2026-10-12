package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Статусы связи с точкой.
const (
	StatusOK           = "ok"
	StatusUnreachable  = "unreachable"
	StatusUnauthorized = "unauthorized" // точка ответила 401/403: ключ неверен или отозван
	StatusError        = "error"        // точка ответила, но не 2xx или не JSON
)

// PointState — снимок точки после последнего опроса.
type PointState struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	LastAttemptAt *time.Time `json:"last_attempt_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	Error         string     `json:"error,omitempty"`

	StandTime *time.Time `json:"stand_time"` // часы стенда точки; nil, если данных нет

	ExpiringBatches int `json:"expiring_batches"`
	ExpiredBatches  int `json:"expired_batches"`
	// ExpiringSumMinor — null: API точки не отдаёт цену партии (см. docs/network.md).
	ExpiringSumMinor *int64 `json:"expiring_sum_minor"`

	StopListCount int      `json:"stop_list_count"`
	StopListNames []string `json:"stop_list_names"`

	WriteoffsTodayMinor int64 `json:"writeoffs_today_minor"`
	Writeoffs7dMinor    int64 `json:"writeoffs_7d_minor"`

	TopHotOffer *HotOffer `json:"top_hot_offer"`

	// Ленты для /api/network/points/{id}.
	Alerts   []Alert        `json:"alerts,omitempty"`
	StopList []StopListItem `json:"stop_list,omitempty"`
}

// HotOffer — первое горячее предложение точки.
type HotOffer struct {
	DishID      string `json:"dish_id"`
	DishName    string `json:"dish_name"`
	Explanation string `json:"explanation,omitempty"`
}

// Alert — запись ленты предупреждений точки (/api/alerts).
type Alert struct {
	BatchID         int64     `json:"batch_id"`
	ProductID       string    `json:"product_id"`
	ProductName     string    `json:"product_name"`
	Remaining       Qty       `json:"remaining"`
	ExpiresAt       time.Time `json:"expires_at"`
	TimeLeftSeconds int64     `json:"time_left_seconds"`
	Status          string    `json:"status"`
	Doomed          bool      `json:"doomed"`
}

// Qty — количество в базовых единицах.
type Qty struct {
	Qty  int64  `json:"qty"`
	Unit string `json:"unit"`
}

// StopListItem — блюдо из стоп-листа точки.
type StopListItem struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Missing []json.RawMessage `json:"missing,omitempty"`
}

// Collector опрашивает точки и хранит последние снимки.
type Collector struct {
	points  []Point
	client  *http.Client
	timeout time.Duration
	now     func() time.Time

	mu     sync.RWMutex
	states map[string]PointState
}

// NewCollector создаёт сборщик; timeout — на весь опрос одной точки.
func NewCollector(cfg Config, timeout time.Duration, now func() time.Time) *Collector {
	if now == nil {
		now = time.Now
	}
	c := &Collector{points: cfg.Points, timeout: timeout, now: now,
		client: &http.Client{}, states: map[string]PointState{}}
	for _, p := range cfg.Points {
		c.states[p.ID] = PointState{ID: p.ID, Name: p.Name, Status: StatusUnreachable,
			StopListNames: []string{}}
	}
	return c
}

// Run опрашивает сразу и затем каждые interval до отмены ctx.
func (c *Collector) Run(ctx context.Context, interval time.Duration) {
	c.PollAll(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.PollAll(ctx)
		}
	}
}

// PollAll опрашивает все точки параллельно.
func (c *Collector) PollAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, p := range c.points {
		wg.Add(1)
		go func(p Point) {
			defer wg.Done()
			c.pollOne(ctx, p)
		}(p)
	}
	wg.Wait()
}

func (c *Collector) pollOne(ctx context.Context, p Point) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	at := c.now()
	c.mu.RLock()
	prev := c.states[p.ID]
	c.mu.RUnlock()

	st, err := c.fetch(ctx, p)
	st.ID, st.Name = p.ID, p.Name
	st.LastAttemptAt = &at
	if err != nil {
		// Данных нет: прошлые цифры не показываем как свежие, оставляем только время успеха.
		st = PointState{ID: p.ID, Name: p.Name, LastAttemptAt: &at, LastSuccessAt: prev.LastSuccessAt,
			StopListNames: []string{}, Status: classify(err), Error: scrub(err, p)}
	} else {
		st.Status, st.LastSuccessAt = StatusOK, &at
	}
	c.mu.Lock()
	c.states[p.ID] = st
	c.mu.Unlock()
}

type httpStatusError struct{ code int }

func (e httpStatusError) Error() string { return fmt.Sprintf("http status %d", e.code) }

func classify(err error) string {
	var he httpStatusError
	if errors.As(err, &he) {
		if he.code == http.StatusUnauthorized || he.code == http.StatusForbidden {
			return StatusUnauthorized
		}
		return StatusError
	}
	var je *json.SyntaxError
	var ju *json.UnmarshalTypeError
	if errors.As(err, &je) || errors.As(err, &ju) {
		return StatusError
	}
	return StatusUnreachable
}

// scrub: текст ошибки сети может содержать URL; ключ в URL не передаётся, но на всякий случай
// ошибка отдаётся без адреса — только тип сбоя.
func scrub(err error, _ Point) string {
	var he httpStatusError
	switch {
	case errors.As(err, &he):
		return he.Error()
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case classify(err) == StatusError:
		return "invalid response"
	}
	return "connection failed"
}

func (c *Collector) get(ctx context.Context, p Point, path string, q url.Values, out any) error {
	u := p.URL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err2(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return httpStatusError{resp.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

// err2 оборачивает ошибку клиента (url.Error с адресом) в безадресную.
func err2(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("request: %w", ue.Err)
	}
	return fmt.Errorf("request: %w", err)
}

func (c *Collector) fetch(ctx context.Context, p Point) (PointState, error) {
	var st PointState
	var clock struct {
		Now      time.Time `json:"now"`
		Timezone string    `json:"timezone"`
	}
	if err := c.get(ctx, p, "/api/clock", nil, &clock); err != nil {
		return st, err
	}
	var alerts []Alert
	if err := c.get(ctx, p, "/api/alerts", nil, &alerts); err != nil {
		return st, err
	}
	var stop []StopListItem
	if err := c.get(ctx, p, "/api/stop-list", nil, &stop); err != nil {
		return st, err
	}
	var offers struct {
		Offers []HotOffer `json:"offers"`
	}
	if err := c.get(ctx, p, "/api/hot-offers", nil, &offers); err != nil {
		return st, err
	}

	loc, err := time.LoadLocation(clock.Timezone)
	if err != nil {
		loc = time.UTC
	}
	today := clock.Now.In(loc).Format("2006-01-02")
	var wToday, w7 struct {
		TotalMinor int64 `json:"total_minor"`
	}
	if err := c.get(ctx, p, "/api/analytics/writeoffs", url.Values{"from": {today}, "to": {today}}, &wToday); err != nil {
		return st, err
	}
	if err := c.get(ctx, p, "/api/analytics/writeoffs", url.Values{
		"from": {clock.Now.In(loc).AddDate(0, 0, -6).Format("2006-01-02")}, "to": {today}}, &w7); err != nil {
		return st, err
	}

	t := clock.Now
	st.StandTime = &t
	for _, a := range alerts {
		switch a.Status {
		case "expiring":
			st.ExpiringBatches++
		case "expired":
			st.ExpiredBatches++
		}
	}
	st.Alerts = alerts
	st.StopList = stop
	st.StopListCount = len(stop)
	st.StopListNames = make([]string, 0, len(stop))
	for _, d := range stop {
		st.StopListNames = append(st.StopListNames, d.Name)
	}
	st.WriteoffsTodayMinor, st.Writeoffs7dMinor = wToday.TotalMinor, w7.TotalMinor
	if len(offers.Offers) > 0 {
		o := offers.Offers[0]
		st.TopHotOffer = &o
	}
	return st, nil
}

// Snapshot возвращает снимки точек в порядке конфига.
func (c *Collector) Snapshot() []PointState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]PointState, 0, len(c.points))
	for _, p := range c.points {
		out = append(out, c.states[p.ID])
	}
	return out
}

// Point возвращает снимок одной точки.
func (c *Collector) Point(id string) (PointState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	st, ok := c.states[id]
	return st, ok
}

// Totals — итог по сети.
type Totals struct {
	Points              int   `json:"points"`
	OK                  int   `json:"ok"`
	Unavailable         int   `json:"unavailable"`
	ExpiringBatches     int   `json:"expiring_batches"`
	StopListCount       int   `json:"stop_list_count"`
	WriteoffsTodayMinor int64 `json:"writeoffs_today_minor"`
	Writeoffs7dMinor    int64 `json:"writeoffs_7d_minor"`
}

// Summarize считает итог по точкам; недоступные в суммы не входят.
func Summarize(states []PointState) Totals {
	t := Totals{Points: len(states)}
	for _, s := range states {
		if s.Status != StatusOK {
			t.Unavailable++
			continue
		}
		t.OK++
		t.ExpiringBatches += s.ExpiringBatches
		t.StopListCount += s.StopListCount
		t.WriteoffsTodayMinor += s.WriteoffsTodayMinor
		t.Writeoffs7dMinor += s.Writeoffs7dMinor
	}
	return t
}
