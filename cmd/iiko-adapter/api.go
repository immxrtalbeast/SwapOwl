package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"umirhack/internal/iiko/reconcile"
)

// apiClient ходит в наш API по ключу Authorization: Bearer.
type apiClient struct {
	base, key string
	hc        *http.Client
}

// apiError — ответ API с неуспешным статусом.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string { return fmt.Sprintf("api: статус %d: %s", e.Status, e.Body) }

func (a *apiClient) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.base, "/")+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api %s: %w", path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, &apiError{Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	return b, nil
}

// stoppedDishes — id блюд из GET /api/stop-list.
func (a *apiClient) stoppedDishes(ctx context.Context) ([]string, error) {
	b, err := a.do(ctx, http.MethodGet, "/api/stop-list", nil)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// productStock — остатки из GET /api/products (непросроченные партии).
func (a *apiClient) productStock(ctx context.Context) ([]reconcile.Ours, error) {
	b, err := a.do(ctx, http.MethodGet, "/api/products", nil)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Available struct {
			Qty int64 `json:"qty"`
		} `json:"available"`
	}
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, err
	}
	out := make([]reconcile.Ours, 0, len(rows))
	for _, r := range rows {
		out = append(out, reconcile.Ours{ID: r.ID, Name: r.Name, Qty: r.Available.Qty})
	}
	return out, nil
}
