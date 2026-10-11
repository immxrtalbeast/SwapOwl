// Package client — HTTP-клиент iikoServer API (подмножество, см. docs/iiko.md).
package client

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // iikoServer требует SHA-1 пароля
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

// Client — клиент iiko с токеном; при 401 один раз входит заново.
type Client struct {
	base, login, pass string
	hc                *http.Client
	mu                sync.Mutex
	token             string
}

// New создаёт клиента. base — например http://localhost:8081.
func New(base, login, pass string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{base: strings.TrimRight(base, "/"), login: login, pass: pass, hc: hc}
}

func (c *Client) auth(ctx context.Context) error {
	sum := sha1.Sum([]byte(c.pass)) //nolint:gosec
	q := url.Values{"login": {c.login}, "pass": {hex.EncodeToString(sum[:])}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+iiko.PathAuth+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("iiko auth: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK || len(b) == 0 {
		return fmt.Errorf("iiko auth: статус %d", resp.StatusCode)
	}
	c.mu.Lock()
	c.token = strings.TrimSpace(string(b))
	c.mu.Unlock()
	return nil
}

func (c *Client) key() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
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
		req, err := http.NewRequestWithContext(ctx, method, c.base+path+"?"+qq.Encode(), bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.hc.Do(req)
		if err != nil {
			return nil, fmt.Errorf("iiko %s: %w", path, err)
		}
		b, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			c.mu.Lock()
			c.token = ""
			c.mu.Unlock()
			continue
		}
		if rerr != nil {
			return nil, rerr
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("iiko %s: статус %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
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

// Sales — продажи по блюдам и дням за [from, to): конец не входит.
func (c *Client) Sales(ctx context.Context, from, to time.Time) ([]mapping.Sale, error) {
	req := iiko.OLAPRequest{
		ReportType:       "SALES",
		GroupByRowFields: []string{iiko.OLAPDishID, iiko.OLAPDishName, iiko.OLAPOpenDate},
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
		out = append(out, mapping.Sale{Date: date, DishID: id, Portions: int64(amt + 0.5)})
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
