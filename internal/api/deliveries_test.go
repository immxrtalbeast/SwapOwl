package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

type fakeStock struct {
	in      store.DeliveryInput
	err     error
	nowSeen time.Time
}

func (f *fakeStock) ProductUnits(_ context.Context, ids []string) (map[string]domain.Unit, error) {
	m := map[string]domain.Unit{}
	for _, id := range ids {
		if id == "salmon" {
			m[id] = domain.UnitG
		}
	}
	return m, nil
}
func (f *fakeStock) ReceiveDelivery(_ context.Context, in store.DeliveryInput, _ func() time.Time, _ *time.Location) (store.DeliveryResponse, error) {
	f.in = in
	return store.DeliveryResponse{ID: in.ID, Batches: []store.BatchJSON{}, Warnings: []store.WarningJSON{}}, f.err
}
func (f *fakeStock) ListDeliveries(context.Context) ([]store.DeliveryResponse, error) {
	return nil, nil
}
func (f *fakeStock) GetDelivery(context.Context, string) (store.DeliveryResponse, error) {
	return store.DeliveryResponse{}, store.ErrNotFound
}
func (f *fakeStock) ListProductStock(_ context.Context, now time.Time) ([]store.ProductStock, error) {
	f.nowSeen = now
	return []store.ProductStock{}, nil
}
func (f *fakeStock) ListBatches(_ context.Context, now time.Time, _, _ string) ([]store.BatchView, error) {
	f.nowSeen = now
	return []store.BatchView{}, nil
}

func post(t *testing.T, fs *fakeStock, body string) *httptest.ResponseRecorder {
	t.Helper()
	rt := NewRouter(Deps{Now: func() time.Time { return standTime }, Stock: fs})
	req := httptest.NewRequest(http.MethodPost, "/api/deliveries", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, req)
	return rec
}

func item(extra string) string {
	return `{"id":"D1","number":"1","created_at":"2026-10-09T08:00:00+03:00","items":[` + extra + `]}`
}

func TestPostDeliveryNormalizesUnits(t *testing.T) {
	fs := &fakeStock{}
	rec := post(t, fs, item(`{"product_id":"salmon","quantity":"4","unit":"kg","shelf_life_hours":72},{"product_id":"cream","name":"Сливки","quantity":"0.6","unit":"l","expires_on":"2026-10-12"}`))
	if rec.Code != 201 || len(fs.in.Items) != 2 {
		t.Fatalf("code %d body %s", rec.Code, rec.Body)
	}
	if a, b := fs.in.Items[0], fs.in.Items[1]; a.Qty != 4000 || a.Unit != domain.UnitG || b.Qty != 600 || b.Unit != domain.UnitMl || b.ExpiresOn != "2026-10-12" {
		t.Fatalf("items: %+v", fs.in.Items)
	}
}

func TestPostDeliveryValidation(t *testing.T) {
	cases := map[string]struct{ body, code string }{
		"number quantity": {item(`{"product_id":"salmon","quantity":4,"shelf_life_hours":1}`), CodeValidation},
		"comma":           {item(`{"product_id":"salmon","quantity":"1,5","shelf_life_hours":1}`), CodeValidation},
		"zero":            {item(`{"product_id":"salmon","quantity":"0","shelf_life_hours":1}`), CodeValidation},
		"dimension":       {item(`{"product_id":"salmon","quantity":"1","unit":"l","shelf_life_hours":1}`), CodeValidation},
		"both expiries":   {item(`{"product_id":"salmon","quantity":"1","shelf_life_hours":1,"expires_on":"2026-10-12"}`), CodeValidation},
		"hours range":     {item(`{"product_id":"salmon","quantity":"1","shelf_life_hours":87601}`), CodeValidation},
		"bad date":        {item(`{"product_id":"salmon","quantity":"1","expires_on":"12.10.2026"}`), CodeValidation},
		"no items":        {item(``), CodeValidation},
		"unknown no unit": {item(`{"product_id":"ghost","quantity":"1","shelf_life_hours":1}`), CodeUnknownProduct},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := post(t, &fakeStock{}, c.body)
			if rec.Code != http.StatusBadRequest || decodeErr(t, rec).Error.Code != c.code {
				t.Fatalf("code %d body %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestPostDeliveryStatusMapping(t *testing.T) {
	ok := item(`{"product_id":"salmon","quantity":"1","shelf_life_hours":5}`)
	if rec := post(t, &fakeStock{err: store.ErrDeliveryConflict}, ok); rec.Code != 409 || decodeErr(t, rec).Error.Code != CodeDeliveryConf {
		t.Fatalf("conflict: %d %s", rec.Code, rec.Body)
	}
	if rec := post(t, &fakeStock{}, ok); rec.Code != 201 {
		t.Fatalf("ok: %d", rec.Code)
	}
}

func TestPostDeliveryNoExpiryUsesProductDefault(t *testing.T) {
	// Срок не задан: API пропускает позицию, срок продукта подставляет хранилище.
	fs := &fakeStock{}
	rec := post(t, fs, item(`{"product_id":"salmon","quantity":"1"}`))
	if rec.Code != 201 || fs.in.Items[0].ShelfLifeHours != 0 || fs.in.Items[0].ExpiresOn != "" {
		t.Fatalf("code %d body %s items %+v", rec.Code, rec.Body, fs.in.Items)
	}
	// У продукта срока нет — 400, как раньше.
	rec = post(t, &fakeStock{err: &store.NoShelfLifeError{Index: 0, ProductID: "salmon"}}, item(`{"product_id":"salmon","quantity":"1"}`))
	if rec.Code != 400 || decodeErr(t, rec).Error.Code != CodeValidation || !strings.Contains(rec.Body.String(), "items[0].shelf_life_hours") {
		t.Fatalf("no default: %d %s", rec.Code, rec.Body)
	}
}

func TestStockReadsUseStandClock(t *testing.T) {
	fs := &fakeStock{}
	rt := NewRouter(Deps{Now: func() time.Time { return standTime }, Stock: fs})
	for _, p := range []string{"/api/products", "/api/batches?status=ok"} {
		rec := httptest.NewRecorder()
		rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		var v []any
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &v) != nil || !fs.nowSeen.Equal(standTime) {
			t.Fatalf("%s: %d %s now=%v", p, rec.Code, rec.Body, fs.nowSeen)
		}
	}
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/batches?status=zzz", nil))
	if rec.Code != 400 {
		t.Fatalf("bad status: %d", rec.Code)
	}
}
