// Package client — HTTP-клиент iikoServer API (подмножество, см. docs/iiko.md).
package client

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // iikoServer требует SHA-1 пароля
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"umirhack/internal/iiko"
	"umirhack/internal/iiko/mapping"
)

// DefaultBasePath — базовый путь iikoServer; в путях internal/iiko он входит в константы.
const DefaultBasePath = "/resto"

// Options — настройки клиента для настоящего сервера. Нулевые значения означают умолчания.
type Options struct {
	// BasePath — путь до API (/resto). Если он указан в самом адресе (https://host:443/resto), берётся оттуда.
	BasePath string
	// Timeout — таймаут одного запроса (по умолчанию 60 с); учитывается, только если hc не задан.
	Timeout time.Duration
	// Insecure — не проверять TLS-сертификат (самоподписанный); учитывается, только если hc не задан.
	Insecure bool
	// Retries — число повторов при 5xx и сетевой ошибке (по умолчанию 2; отрицательное — без повторов).
	Retries int
	// RetryPause — пауза перед первым повтором, дальше удваивается (по умолчанию 1 с).
	RetryPause time.Duration
	// MinInterval — минимальная пауза между запросами; 0 — без ограничения.
	MinInterval time.Duration
}

// Client — клиент iiko: токен переиспользуется (лицензионные сессии ограничены), при 401 один раз
// входит заново, Logout освобождает сессию.
type Client struct {
	base, basePath, login, pass string
	hc                          *http.Client
	opt                         Options
	mu                          sync.Mutex // токен и время последнего запроса
	token                       string
	last                        time.Time
	sleep                       func(context.Context, time.Duration) error
}

// NewHTTPClient собирает http.Client с таймаутом; insecure отключает проверку сертификата.
func NewHTTPClient(timeout time.Duration, insecure bool) *http.Client {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // явный флаг для самоподписанного сертификата
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}

// New создаёт клиента с умолчаниями. base — например http://localhost:8081 или https://host:443/resto.
func New(base, login, pass string, hc *http.Client) *Client {
	return NewWithOptions(base, login, pass, hc, Options{})
}

// NewWithOptions создаёт клиента с настройками; hc == nil — клиент по Timeout/Insecure.
func NewWithOptions(base, login, pass string, hc *http.Client, opt Options) *Client {
	if hc == nil {
		hc = NewHTTPClient(opt.Timeout, opt.Insecure)
	}
	base = strings.TrimRight(base, "/")
	bp := opt.BasePath
	if u, err := url.Parse(base); err == nil && u.Host != "" && strings.Trim(u.Path, "/") != "" {
		bp = "/" + strings.Trim(u.Path, "/")
		base = strings.TrimSuffix(base, u.Path)
	}
	if bp == "" {
		bp = DefaultBasePath
	}
	bp = "/" + strings.Trim(bp, "/")
	if bp == "/" {
		bp = ""
	}
	if opt.Retries == 0 {
		opt.Retries = 2
	} else if opt.Retries < 0 {
		opt.Retries = 0
	}
	if opt.RetryPause <= 0 {
		opt.RetryPause = time.Second
	}
	return &Client{base: base, basePath: bp, login: login, pass: pass, hc: hc, opt: opt, sleep: sleepCtx}
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

// path подставляет настроенный базовый путь вместо /resto из констант.
func (c *Client) path(p string) string {
	return c.basePath + strings.TrimPrefix(p, DefaultBasePath)
}

// throttle выдерживает MinInterval между запросами.
func (c *Client) throttle(ctx context.Context) error {
	if c.opt.MinInterval <= 0 {
		return nil
	}
	c.mu.Lock()
	wait := c.opt.MinInterval - time.Since(c.last)
	if wait < 0 {
		wait = 0
	}
	c.last = time.Now().Add(wait)
	c.mu.Unlock()
	if wait == 0 {
		return nil
	}
	return c.sleep(ctx, wait)
}

// send выполняет один запрос с повтором на 5xx и сетевых ошибках; возвращает статус и тело.
func (c *Client) send(ctx context.Context, method, rawURL string, payload []byte) (int, []byte, error) {
	pause := c.opt.RetryPause
	var lastErr error
	for try := 0; ; try++ {
		if err := c.throttle(ctx); err != nil {
			return 0, nil, err
		}
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
		if err != nil {
			return 0, nil, err
		}
		if payload != nil {
			ct := "application/json"
			if payload[0] == '<' {
				ct = "application/xml"
			}
			req.Header.Set("Content-Type", ct)
		}
		resp, err := c.hc.Do(req)
		var status int
		var b []byte
		if err == nil {
			status = resp.StatusCode
			b, err = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return 0, nil, ctx.Err()
			}
			lastErr = err
		case status >= 500:
			lastErr = fmt.Errorf("статус %d: %s", status, strings.TrimSpace(string(b)))
		default:
			return status, b, nil
		}
		if try >= c.opt.Retries {
			return 0, nil, lastErr
		}
		if err := c.sleep(ctx, pause); err != nil {
			return 0, nil, err
		}
		pause *= 2
	}
}

func (c *Client) auth(ctx context.Context) error {
	sum := sha1.Sum([]byte(c.pass)) //nolint:gosec
	q := url.Values{"login": {c.login}, "pass": {hex.EncodeToString(sum[:])}}
	status, b, err := c.send(ctx, http.MethodPost, c.base+c.path(iiko.PathAuth)+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("iiko auth: %w", err)
	}
	tok := strings.TrimSpace(string(b))
	if status != http.StatusOK || tok == "" {
		return fmt.Errorf("iiko auth: статус %d", status)
	}
	c.mu.Lock()
	c.token = tok
	c.mu.Unlock()
	return nil
}

func (c *Client) key() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

// Logout освобождает лицензионную сессию iiko и забывает токен; без токена ничего не делает.
func (c *Client) Logout(ctx context.Context) error {
	k := c.key()
	if k == "" {
		return nil
	}
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
	status, _, err := c.send(ctx, http.MethodPost, c.base+c.path(iiko.PathLogout)+"?"+url.Values{"key": {k}}.Encode(), nil)
	if err != nil {
		return fmt.Errorf("iiko logout: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("iiko logout: статус %d", status)
	}
	return nil
}

// do выполняет запрос с ключом; тело (если есть) отправляется как JSON.
func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any) ([]byte, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	return c.doRaw(ctx, method, path, q, payload)
}

// doRaw — как do, но тело уже готово (XML или JSON).
func (c *Client) doRaw(ctx context.Context, method, path string, q url.Values, payload []byte) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if c.key() == "" {
			if err := c.auth(ctx); err != nil {
				return nil, err
			}
		}
		qq := url.Values{}
		for k, v := range q {
			qq[k] = v
		}
		qq.Set("key", c.key())
		status, b, err := c.send(ctx, method, c.base+c.path(path)+"?"+qq.Encode(), payload)
		if err != nil {
			return nil, fmt.Errorf("iiko %s: %w", path, err)
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			c.mu.Lock()
			c.token = ""
			c.mu.Unlock()
			continue
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("iiko %s: статус %d: %s", path, status, strings.TrimSpace(string(b)))
		}
		return b, nil
	}
	return nil, fmt.Errorf("iiko %s: не удалось войти", path)
}

// Products — номенклатура.
func (c *Client) Products(ctx context.Context) ([]iiko.Product, error) {
	b, err := c.do(ctx, http.MethodGet, iiko.PathProducts, nil, nil)
	if err != nil {
		return nil, err
	}
	var out []iiko.Product
	return out, json.Unmarshal(b, &out)
}

// Charts — техкарты.
func (c *Client) Charts(ctx context.Context) ([]iiko.AssemblyChart, error) {
	b, err := c.do(ctx, http.MethodGet, iiko.PathAssemblyCharts, nil, nil)
	if err != nil {
		return nil, err
	}
	var out iiko.AssemblyChartsResponse
	return out.RawAssemblyCharts, json.Unmarshal(b, &out)
}

// Invoices — приходные накладные за дни from..to включительно.
func (c *Client) Invoices(ctx context.Context, from, to time.Time) ([]iiko.IncomingInvoice, error) {
	q := url.Values{"from": {from.Format(iiko.DateLayout)}, "to": {to.Format(iiko.DateLayout)}}
	b, err := c.do(ctx, http.MethodGet, iiko.PathIncomingInvoice, q, nil)
	if err != nil {
		return nil, err
	}
	var out iiko.IncomingInvoices
	return out.Documents, xml.Unmarshal(b, &out)
}

// Sales — продажи по строкам чеков (блюдо, чек, время закрытия) за [from, to): конец не входит.
func (c *Client) Sales(ctx context.Context, from, to time.Time) ([]mapping.Sale, error) {
	req := iiko.OLAPRequest{
		ReportType:       "SALES",
		GroupByRowFields: []string{iiko.OLAPDishID, iiko.OLAPDishName, iiko.OLAPOpenDate, iiko.OLAPOrderID, iiko.OLAPCloseTime},
		AggregateFields:  []string{iiko.OLAPAmount, iiko.OLAPSum},
		Filters: map[string]iiko.OLAPFilter{iiko.OLAPOpenDate: {
			FilterType: "DateRange", From: from.Format(iiko.DateLayout), To: to.Format(iiko.DateLayout)}},
	}
	b, err := c.do(ctx, http.MethodPost, iiko.PathOLAP, nil, req)
	if err != nil {
		return nil, err
	}
	var resp iiko.OLAPResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	var out []mapping.Sale
	for _, row := range resp.Data {
		id, _ := row[iiko.OLAPDishID].(string)
		date, _ := row[iiko.OLAPOpenDate].(string)
		amt, _ := row[iiko.OLAPAmount].(float64)
		check, _ := row[iiko.OLAPOrderID].(string)
		at, _ := row[iiko.OLAPCloseTime].(string)
		out = append(out, mapping.Sale{Date: date, DishID: id, Portions: int64(amt + 0.5), CheckID: check, CloseTime: at})
	}
	return out, nil
}

// StopList читает стоп-лист iiko.
func (c *Client) StopList(ctx context.Context) ([]iiko.StopItem, error) {
	b, err := c.do(ctx, http.MethodGet, iiko.PathStopLists, nil, nil)
	if err != nil {
		return nil, err
	}
	var out iiko.StopListResponse
	return out.Items, json.Unmarshal(b, &out)
}

// SetStopList заменяет стоп-лист целиком.
func (c *Client) SetStopList(ctx context.Context, items []iiko.StopItem) error {
	if items == nil {
		items = []iiko.StopItem{}
	}
	_, err := c.do(ctx, http.MethodPost, iiko.PathStopLists, nil, iiko.StopListResponse{Items: items})
	return err
}

// Balance читает остатки по складам на момент at (часовой пояс ресторана).
func (c *Client) Balance(ctx context.Context, at time.Time) ([]iiko.BalanceItem, error) {
	q := url.Values{"timestamp": {at.Format(iiko.DateTimeLayout)}}
	b, err := c.do(ctx, http.MethodGet, iiko.PathBalanceStores, q, nil)
	if err != nil {
		return nil, err
	}
	var out []iiko.BalanceItem
	return out, json.Unmarshal(b, &out)
}

// ImportPurchaseOrder передаёт в iiko документ «Заказ поставщику». Повтор с тем же id документа
// заменяет его, а не создаёт второй. Ответ iiko valid=false — ошибка. [ПРЕДПОЛОЖЕНО]: путь не сверен.
func (c *Client) ImportPurchaseOrder(ctx context.Context, doc iiko.PurchaseOrder) error {
	b, err := xml.Marshal(doc)
	if err != nil {
		return err
	}
	resp, err := c.doRaw(ctx, http.MethodPost, iiko.PathPurchaseImport, nil, b)
	if err != nil {
		return err
	}
	var res iiko.DocumentValidationResult
	if err := xml.Unmarshal(resp, &res); err != nil {
		return fmt.Errorf("iiko purchaseOrder: ответ: %w", err)
	}
	if !res.Valid {
		return fmt.Errorf("iiko purchaseOrder: документ отклонён: %s", res.ErrorMessage)
	}
	return nil
}

// PurchaseOrders читает заказы поставщику (id пустой — все). Есть у фейка; в настоящем iiko путь
// не сверен [ПРЕДПОЛОЖЕНО], используется для проверки в тестах и стенде.
func (c *Client) PurchaseOrders(ctx context.Context, id string) ([]iiko.PurchaseOrder, error) {
	q := url.Values{}
	if id != "" {
		q.Set("id", id)
	}
	b, err := c.do(ctx, http.MethodGet, iiko.PathPurchaseExport, q, nil)
	if err != nil {
		return nil, err
	}
	var out iiko.PurchaseOrders
	return out.Documents, xml.Unmarshal(b, &out)
}
