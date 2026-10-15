// Package notify — уведомления управляющему в Telegram (docs/notify.md). Процесс ничего не знает
// о БД: читает наш API по ключу viewer и слушает GET /events.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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
	ProductID   string    `json:"product_id"`
	ProductName string    `json:"product_name"`
	ExpiresAt   time.Time `json:"expires_at"`
	BatchID     int64     `json:"batch_id"`
}

// Offer — блюдо из GET /api/hot-offers.
type Offer struct {
	DishID         string   `json:"dish_id"`
	DishName       string   `json:"dish_name"`
	TargetPortions int64    `json:"target_portions"`
	Reasons        []Reason `json:"reasons"`
	ManagerNote    string   `json:"manager_note"`
}

// HotData — полный ответ GET /api/hot-offers: расчёт, закреплённое и скрытое менеджером.
type HotData struct {
	Offers       []Offer `json:"offers"`
	ManagerPicks []struct {
		DishID   string `json:"dish_id"`
		DishName string `json:"dish_name"`
		Note     string `json:"note"`
	} `json:"manager_picks"`
	Hidden []struct {
		DishID string `json:"dish_id"`
	} `json:"hidden"`
}

// Suggestion — строка GET /api/supply-suggestions.
type Suggestion struct {
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	Quantity    Qty    `json:"quantity"`
	Urgency     string `json:"urgency"`
	Explanation string `json:"explanation"`
	// ShelfLifeHours — обычный срок поставщика (FS-160); nil — неизвестен.
	ShelfLifeHours *int `json:"shelf_life_hours"`
}

// ProductInfo — продукт из GET /api/products; ShelfLifeHours — обычный срок из карточки, если API его отдаёт.
type ProductInfo struct {
	ID             string `json:"id"`
	ShelfLifeHours *int   `json:"shelf_life_hours"`
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

// ReceivingBatch — партия из GET /api/receiving (срок не подтверждён).
type ReceivingBatch struct {
	ID             int64     `json:"id"`
	ProductName    string    `json:"product_name"`
	DeliveryNumber string    `json:"delivery_number"`
	Quantity       Qty       `json:"quantity"`
	Remaining      Qty       `json:"remaining"`
	ReceivedAt     time.Time `json:"received_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	ExpirySource   string    `json:"expiry_source"`
}

// StopDish — блюдо из GET /api/stop-list.
type StopDish struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Missing []struct {
		ProductName string `json:"product_name"`
		Required    Qty    `json:"required"`
		Available   Qty    `json:"available"`
		Missing     Qty    `json:"missing"`
	} `json:"missing"`
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

// Receiving — GET /api/receiving.
func (c *APIClient) Receiving(ctx context.Context) (bs []ReceivingBatch, err error) {
	err = c.get(ctx, "/api/receiving", nil, &bs)
	return
}

// StopList — GET /api/stop-list.
func (c *APIClient) StopList(ctx context.Context) (ds []StopDish, err error) {
	err = c.get(ctx, "/api/stop-list", nil, &ds)
	return
}

// APIError — ответ API с неуспешным статусом.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string { return fmt.Sprintf("api: статус %d: %s", e.Status, e.Body) }

// PatchExpiry — PATCH /api/batches/{id}/expiry; ровно одно из expiresOn и shelfHours.
func (c *APIClient) PatchExpiry(ctx context.Context, id int64, expiresOn string, shelfHours int) error {
	m := map[string]any{}
	if expiresOn != "" {
		m["expires_on"] = expiresOn
	} else {
		m["shelf_life_hours"] = shelfHours
	}
	body, _ := json.Marshal(m)
	u := fmt.Sprintf("%s/api/batches/%d/expiry", strings.TrimRight(c.Base, "/"), id)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HC.Do(req)
	if err != nil {
		return fmt.Errorf("api expiry: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	return nil
}

// HotData — GET /api/hot-offers целиком (с manager_picks и hidden).
func (c *APIClient) HotData(ctx context.Context) (r HotData, err error) {
	err = c.get(ctx, "/api/hot-offers", nil, &r)
	return
}

// SupplySuggestions — GET /api/supply-suggestions.
func (c *APIClient) SupplySuggestions(ctx context.Context) (r []Suggestion, err error) {
	err = c.get(ctx, "/api/supply-suggestions", nil, &r)
	return
}

// Products — GET /api/products.
func (c *APIClient) Products(ctx context.Context) (r []ProductInfo, err error) {
	err = c.get(ctx, "/api/products", nil, &r)
	return
}

// PostDelivery — POST /api/deliveries с одной позицией и явным сроком (ровно одно из expiresOn и shelfHours).
// Идемпотентна по id: повтор возвращает ту же поставку (200), новая — 201; created=true только для 201.
func (c *APIClient) PostDelivery(ctx context.Context, id string, createdAt time.Time, productID string, q Qty, expiresOn string, shelfHours int) (created bool, err error) {
	item := map[string]any{"product_id": productID, "quantity": strconv.FormatInt(q.Qty, 10), "unit": q.Unit}
	if expiresOn != "" {
		item["expires_on"] = expiresOn
	} else {
		item["shelf_life_hours"] = shelfHours
	}
	body, _ := json.Marshal(map[string]any{"id": id, "number": id, "created_at": createdAt.Format(time.RFC3339), "items": []any{item}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Base, "/")+"/api/deliveries", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HC.Do(req)
	if err != nil {
		return false, fmt.Errorf("api deliveries: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return false, &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	return resp.StatusCode == http.StatusCreated, nil
}
