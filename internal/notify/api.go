// Package notify — уведомления управляющему в Telegram (docs/notify.md). Процесс ничего не знает
// о БД: читает наш API по ключу viewer и слушает GET /events.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Qty — количество в ответах API: {"qty": 1500, "unit": "g"}.
type Qty struct {
	Qty  int64  `json:"qty"`
	Unit string `json:"unit"`
}

// Alert — строка GET /api/alerts.
type Alert struct {
	BatchID     int64     `json:"batch_id"`
	ProductID   string    `json:"product_id"`
	ProductName string    `json:"product_name"`
	Remaining   Qty       `json:"remaining"`
	ExpiresAt   time.Time `json:"expires_at"`
	Status      string    `json:"status"`
	Doomed      bool      `json:"doomed"`
}

// Reason — партия, ради которой блюдо в горячих предложениях.
type Reason struct {
	ProductID string `json:"product_id"`
	BatchID   int64  `json:"batch_id"`
}

// Offer — блюдо из GET /api/hot-offers.
type Offer struct {
	DishID         string   `json:"dish_id"`
	DishName       string   `json:"dish_name"`
	TargetPortions int64    `json:"target_portions"`
	Reasons        []Reason `json:"reasons"`
}

// MoneyRow — строка отчёта о потерях.
type MoneyRow struct {
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	Quantity    Qty    `json:"quantity"`
	CostMinor   int64  `json:"cost_minor"`
}

// MoneyReport — ответ /api/analytics/writeoffs и /api/analytics/shortfalls.
type MoneyReport struct {
	TotalMinor int64      `json:"total_minor"`
	ByProduct  []MoneyRow `json:"by_product"`
}

// Clock — ответ GET /api/clock.
type Clock struct {
	Now      time.Time `json:"now"`
	Timezone string    `json:"timezone"`
}

// APIClient ходит в наш API по ключу Authorization: Bearer.
type APIClient struct {
	Base, Key string
	HC        *http.Client
}

func (c *APIClient) get(ctx context.Context, path string, q url.Values, out any) error {
	u := strings.TrimRight(c.Base, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	resp, err := c.HC.Do(req)
	if err != nil {
		return fmt.Errorf("api %s: %w", path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("api %s: статус %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.Unmarshal(b, out)
}

// Alerts — GET /api/alerts.
func (c *APIClient) Alerts(ctx context.Context) (as []Alert, err error) {
	err = c.get(ctx, "/api/alerts", nil, &as)
	return
}

// HotOffers — предложения GET /api/hot-offers.
func (c *APIClient) HotOffers(ctx context.Context) ([]Offer, error) {
	var r struct {
		Offers []Offer `json:"offers"`
	}
	err := c.get(ctx, "/api/hot-offers", nil, &r)
	return r.Offers, err
}

func period(from, to time.Time) url.Values {
	return url.Values{"from": {from.UTC().Format(time.RFC3339)}, "to": {to.UTC().Format(time.RFC3339)}}
}

// Writeoffs — GET /api/analytics/writeoffs.
func (c *APIClient) Writeoffs(ctx context.Context, from, to time.Time) (r MoneyReport, err error) {
	err = c.get(ctx, "/api/analytics/writeoffs", period(from, to), &r)
	return
}

// Shortfalls — GET /api/analytics/shortfalls (расхождения кассы).
func (c *APIClient) Shortfalls(ctx context.Context, from, to time.Time) (r MoneyReport, err error) {
	err = c.get(ctx, "/api/analytics/shortfalls", period(from, to), &r)
	return
}

// Clock — GET /api/clock.
func (c *APIClient) Clock(ctx context.Context) (r Clock, err error) {
	err = c.get(ctx, "/api/clock", nil, &r)
	return
}
