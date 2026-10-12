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
	Label         string     `json:"label,omitempty"`
	Group         string     `json:"group,omitempty"`
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

	// Только для точек группы ab (ab.go): прогон генератора и списания в ₽ по часам стенда с его начала.
	Run *Run `json:"run,omitempty"`

	// Для подсказок о перемещении (transfers.go); наружу не отдаются.
	RiskBatches []RiskBatch `json:"-"`
	// Prices — цена продукта по каталогу точки, копейки за 1000 базовых единиц (из /api/forecast/writeoffs).
	Prices map[string]int64 `json:"-"`
	Demand []Demand         `json:"-"`

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
	runs   map[string]*abRun // состояние A/B-прогона по id точки группы ab

	transferHours float64
	prices        map[string]int64 // product_id -> копейки за 1000 базовых единиц (HUB_PRICES)
}

// SetTransfers задаёт запас времени на перевозку (часы) и цены продуктов для подсказок о перемещении.
func (c *Collector) SetTransfers(hours float64, prices map[string]int64) {
	c.mu.Lock()
	c.transferHours, c.prices = hours, prices
	c.mu.Unlock()
}

// Transfers — подсказки «переместить партию в другую точку» по последним снимкам (не больше MaxTransfers).
func (c *Collector) Transfers() []Transfer {
	c.mu.RLock()
	defer c.mu.RUnlock()
	states := make([]PointState, 0, len(c.points))
	for _, p := range c.points {
		states = append(states, c.states[p.ID])
	}
	out := SuggestTransfers(states, c.transferHours, c.prices)
	if out == nil {
		out = []Transfer{}
	}
	return out
}

// NewCollector создаёт сборщик; timeout — на весь опрос одной точки.
func NewCollector(cfg Config, timeout time.Duration, now func() time.Time) *Collector {
	if now == nil {
		now = time.Now
	}
	c := &Collector{points: cfg.Points, timeout: timeout, now: now,
		client: &http.Client{}, states: map[string]PointState{}, runs: map[string]*abRun{},
		transferHours: DefaultTransferHours}
	for _, p := range cfg.Points {
		c.states[p.ID] = PointState{ID: p.ID, Name: p.Name, Label: p.Label, Group: p.Group, Status: StatusUnreachable,
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
	st.ID, st.Name, st.Label, st.Group = p.ID, p.Name, p.Label, p.Group
	st.LastAttemptAt = &at
	if err != nil {
		// Данных нет: прошлые цифры не показываем как свежие, оставляем только время успеха.
		st = PointState{ID: p.ID, Name: p.Name, Label: p.Label, Group: p.Group, LastAttemptAt: &at, LastSuccessAt: prev.LastSuccessAt,
			StopListNames: []string{}, Status: classify(err), Error: scrub(err, p)}
	} else {
		st.Status, st.LastSuccessAt = StatusOK, &at
	}
	c.mu.Lock()
	c.states[p.ID] = st
	c.mu.Unlock()
}

type hotOffersResp struct {
	Algo   string `json:"algo"`
	Offers []struct {
		HotOffer
		Reasons []struct {
			RiskBatch
			WorkingHoursLeft float64 `json:"working_hours_left"`
		} `json:"reasons"`
	} `json:"offers"`
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
	var offers hotOffersResp
	if err := c.get(ctx, p, "/api/hot-offers", nil, &offers); err != nil {
		return st, err
	}
	// at_risk у алгоритма a — сумма остатков партий до опорной, а не остаток одной партии. Для подсказок о
	// перемещении нужен алгоритм b, где at_risk относится к одной партии: при другом алгоритме читаем его отдельно.
	riskOffers := offers
	if offers.Algo != "b" {
		riskOffers = hotOffersResp{}
		if err := c.get(ctx, p, "/api/hot-offers", url.Values{"algo": {"b"}}, &riskOffers); err != nil {
			riskOffers = hotOffersResp{} // подсказок по этой точке не будет, сводка остаётся
		}
	}
	// Цены каталога точки — только для подсказок; нет маршрута или ошибка — цены не известны.
	var fc struct {
		Products []struct {
			ProductID  string `json:"product_id"`
			AtRisk     Qty    `json:"at_risk"`
			TotalMinor int64  `json:"total_minor"`
		} `json:"products"`
	}
	_ = c.get(ctx, p, "/api/forecast/writeoffs", nil, &fc)
	// Расход нужен только подсказкам о перемещении: точка без этого маршрута не должна пропадать из сводки.
	var sugg []struct {
		Demand
	}
	if err := c.get(ctx, p, "/api/supply-suggestions", nil, &sugg); err != nil {
		var he httpStatusError
		if errors.As(err, &he) && he.code != http.StatusUnauthorized && he.code != http.StatusForbidden {
			sugg = nil
		} else {
			return st, err
		}
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
		o := offers.Offers[0].HotOffer
		st.TopHotOffer = &o
	}
	seen := map[int64]bool{}
	for _, o := range riskOffers.Offers {
		for _, r := range o.Reasons {
			if r.AtRisk.Qty > 0 && !seen[r.BatchID] {
				seen[r.BatchID] = true
				st.RiskBatches = append(st.RiskBatches, r.RiskBatch)
			}
		}
	}
	for _, f := range fc.Products {
		if f.AtRisk.Qty > 0 && f.TotalMinor > 0 {
			if st.Prices == nil {
				st.Prices = map[string]int64{}
			}
			st.Prices[f.ProductID] = f.TotalMinor * 1000 / f.AtRisk.Qty
		}
	}
	for _, d := range sugg {
		st.Demand = append(st.Demand, d.Demand)
	}
	return st, nil
}

// Snapshot возвращает снимки точек в порядке конфига.
func (c *Collector) Snapshot() []PointState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]PointState, 0, len(c.points))
	for _, p := range c.points {
		out = append(out, c.withRun(c.states[p.ID]))
	}
	return out
}

// Point возвращает снимок одной точки.
func (c *Collector) Point(id string) (PointState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	st, ok := c.states[id]
	return c.withRun(st), ok
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
