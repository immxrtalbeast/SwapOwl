package simulator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"umirhack/internal/generator"
)

// HTTPConfig — параметры прогона генератора против API по HTTP.
// О БД и ядре симулятор ничего не знает: только публичные ручки API.
type HTTPConfig struct {
	BaseURL string // адрес API, например http://localhost:8080
	Key     string // ключ API (Authorization: Bearer); пусто — без заголовка
	Seed    int64
	Speed   float64 // множитель темпа относительно часов стенда, 1 по умолчанию

	StopAt       time.Time     // время стенда, после которого прогон завершается; ноль — без предела
	PollInterval time.Duration // пауза между опросами часов стенда, по умолчанию 1 с
	RetryPause   time.Duration // пауза перед повтором при 5xx, по умолчанию 1 с
	MaxRetries   int           // повторов одного запроса при 5xx, по умолчанию 30
	Client       *http.Client
	Sleep        func(ctx context.Context, d time.Duration) error // для тестов
	Log          io.Writer                                        // nil — без вывода
}

// HTTPStats — итог прогона.
type HTTPStats struct {
	Deliveries int // принято поставок (201/200)
	Orders     int // принято заказов
	Rejected   int // заказы с 409 insufficient_stock — штатный исход
	Failed     int // прочие неуспешные ответы 4xx
	Retries    int // повторы после 5xx и сетевых ошибок
}

type clockResp struct {
	Now time.Time `json:"now"`
}

type apiError struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

// RunHTTP превращает события генератора в запросы POST /api/deliveries и
// POST /api/orders. Темп задаёт GET /api/clock: событие уходит, когда
// время стенда (с учётом Speed) доходит до его At. Возвращается по отмене ctx
// или по достижении StopAt; отмена — не ошибка.
func RunHTTP(ctx context.Context, cfg HTTPConfig) (HTTPStats, error) {
	if cfg.Speed <= 0 {
		cfg.Speed = 1
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.RetryPause <= 0 {
		cfg.RetryPause = time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 30
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleepCtx
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	h := &httpRun{cfg: cfg}

	origin, err := h.clock(ctx)
	if err != nil {
		return h.stats, h.finish(ctx, err)
	}
	cursor := origin
	for {
		now, err := h.clock(ctx)
		if err != nil {
			return h.stats, h.finish(ctx, err)
		}
		virt := origin.Add(time.Duration(float64(now.Sub(origin)) * cfg.Speed))
		if !cfg.StopAt.IsZero() && virt.After(cfg.StopAt) {
			virt = cfg.StopAt
		}
		if virt.After(cursor) {
			events, err := generator.Generate(cfg.Seed, cursor, virt)
			if err != nil {
				return h.stats, err
			}
			for _, e := range events {
				if err := h.send(ctx, e); err != nil {
					return h.stats, h.finish(ctx, err)
				}
			}
			cursor = virt
		}
		if !cfg.StopAt.IsZero() && !cursor.Before(cfg.StopAt) {
			return h.stats, nil
		}
		if err := cfg.Sleep(ctx, cfg.PollInterval); err != nil {
			return h.stats, h.finish(ctx, err)
		}
	}
}

func (h *httpRun) finish(ctx context.Context, err error) error {
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return nil
	}
	return err
}

type httpRun struct {
	cfg   HTTPConfig
	stats HTTPStats
}

func (h *httpRun) logf(format string, a ...any) {
	if h.cfg.Log != nil {
		fmt.Fprintf(h.cfg.Log, format+"\n", a...)
	}
}

func (h *httpRun) clock(ctx context.Context) (time.Time, error) {
	for attempt := 0; ; attempt++ {
		t, status, err := h.getClock(ctx)
		if err == nil && status == http.StatusOK {
			return t, nil
		}
		if err == nil && status < 500 {
			return time.Time{}, fmt.Errorf("GET /api/clock: статус %d", status)
		}
		if ctx.Err() != nil {
			return time.Time{}, ctx.Err()
		}
		if attempt >= h.cfg.MaxRetries {
			return time.Time{}, fmt.Errorf("GET /api/clock: стенд недоступен (статус %d, ошибка %v)", status, err)
		}
		h.stats.Retries++
		if err := h.cfg.Sleep(ctx, h.cfg.RetryPause); err != nil {
			return time.Time{}, err
		}
	}
}

func (h *httpRun) getClock(ctx context.Context) (time.Time, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.BaseURL+"/api/clock", nil)
	if err != nil {
		return time.Time{}, 0, err
	}
	resp, err := h.cfg.Client.Do(req)
	if err != nil {
		return time.Time{}, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, resp.StatusCode, nil
	}
	var c clockResp
	if err := json.Unmarshal(body, &c); err != nil || c.Now.IsZero() {
		return time.Time{}, resp.StatusCode, fmt.Errorf("разбор /api/clock: %v", err)
	}
	return c.Now, resp.StatusCode, nil
}

func (h *httpRun) send(ctx context.Context, e generator.Event) error {
	switch e.Kind {
	case generator.KindDelivery:
		body, err := json.Marshal(deliveryBody(e.Delivery))
		if err != nil {
			return err
		}
		ok, _, err := h.post(ctx, "/api/deliveries", e.Delivery.ID, body)
		if err != nil {
			return err
		}
		if ok {
			h.stats.Deliveries++
		}
	case generator.KindOrder:
		body, err := json.Marshal(orderBody(e.Order))
		if err != nil {
			return err
		}
		ok, rejected, err := h.post(ctx, "/api/orders", e.Order.ID, body)
		if err != nil {
			return err
		}
		switch {
		case ok:
			h.stats.Orders++
		case rejected:
			h.stats.Rejected++
		}
	}
	return nil
}

// post отправляет запрос; на 5xx и сетевые ошибки повторяет с паузой, с тем же
// телом и id (сервер идемпотентен). ok — принято; rejected — 409 insufficient_stock.
func (h *httpRun) post(ctx context.Context, path, id string, body []byte) (ok, rejected bool, err error) {
	for attempt := 0; ; attempt++ {
		status, code, perr := h.doPost(ctx, path, body)
		switch {
		case perr == nil && (status == http.StatusOK || status == http.StatusCreated):
			return true, false, nil
		case perr == nil && status == http.StatusConflict && code == "insufficient_stock":
			return false, true, nil
		case perr == nil && status == http.StatusUnauthorized, perr == nil && status == http.StatusForbidden:
			return false, false, fmt.Errorf("POST %s: статус %d — проверьте ключ API", path, status)
		case perr == nil && status < 500:
			h.stats.Failed++
			h.logf("POST %s id=%s: статус %d %s", path, id, status, code)
			return false, false, nil
		}
		if ctx.Err() != nil {
			return false, false, ctx.Err()
		}
		if attempt >= h.cfg.MaxRetries {
			return false, false, fmt.Errorf("POST %s id=%s: сервер не ответил после %d повторов (статус %d, ошибка %v)", path, id, attempt, status, perr)
		}
		h.stats.Retries++
		h.logf("POST %s id=%s: статус %d, повтор %d", path, id, status, attempt+1)
		if err := h.cfg.Sleep(ctx, h.cfg.RetryPause); err != nil {
			return false, false, err
		}
	}
}

func (h *httpRun) doPost(ctx context.Context, path string, body []byte) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+h.cfg.Key)
	}
	resp, err := h.cfg.Client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var ae apiError
	_ = json.Unmarshal(raw, &ae)
	return resp.StatusCode, ae.Error.Code, nil
}

type deliveryItemJSON struct {
	ProductID      string `json:"product_id"`
	Name           string `json:"name"`
	Quantity       string `json:"quantity"`
	Unit           string `json:"unit,omitempty"`
	ShelfLifeHours int    `json:"shelf_life_hours"`
}

type deliveryJSON struct {
	ID        string             `json:"id"`
	Number    string             `json:"number"`
	CreatedAt time.Time          `json:"created_at"`
	Items     []deliveryItemJSON `json:"items"`
}

func deliveryBody(d *generator.Delivery) deliveryJSON {
	out := deliveryJSON{ID: d.ID, Number: d.Number, CreatedAt: d.CreatedAt}
	for _, it := range d.Items {
		out.Items = append(out.Items, deliveryItemJSON{
			ProductID: it.ProductID, Name: it.Name, Quantity: it.Quantity, Unit: it.Unit, ShelfLifeHours: it.ShelfLifeHours,
		})
	}
	return out
}

type orderItemJSON struct {
	DishID   string `json:"dish_id"`
	Portions int    `json:"portions"`
}

type orderJSON struct {
	ID    string          `json:"id"`
	Items []orderItemJSON `json:"items"`
}

func orderBody(o *generator.Order) orderJSON {
	out := orderJSON{ID: o.ID}
	for _, it := range o.Items {
		out.Items = append(out.Items, orderItemJSON{DishID: it.DishID, Portions: it.Portions})
	}
	return out
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
