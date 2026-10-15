package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"umirhack/internal/store"
)

type fakeCodes struct {
	list    []store.SupplierCode
	set     store.SupplierCode
	deleted string
	err     error
}

func (f *fakeCodes) ListSupplierCodes(context.Context) ([]store.SupplierCode, error) {
	return f.list, nil
}
func (f *fakeCodes) SetSupplierCode(_ context.Context, c store.SupplierCode) (store.SupplierCode, error) {
	f.set = c
	return c, f.err
}
func (f *fakeCodes) DeleteSupplierCode(_ context.Context, code string) error {
	f.deleted = code
	return nil
}

func codesDo(f *fakeCodes, method, path, body string) *httptest.ResponseRecorder {
	rt := NewRouter(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), SupplierCodes: f, Now: func() time.Time { return standTime }})
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, req)
	return rec
}

func TestSupplierCodePutListDelete(t *testing.T) {
	f := &fakeCodes{list: []store.SupplierCode{{Code: "04607001234567", ProductID: "milk", SetAt: standTime}}}
	rec := codesDo(f, "PUT", "/api/supplier-codes/04607001234567", `{"product_id":"milk"}`)
	if rec.Code != 200 || f.set.Code != "04607001234567" || f.set.ProductID != "milk" || !f.set.SetAt.Equal(standTime.UTC()) {
		t.Fatalf("PUT: %d %s %+v", rec.Code, rec.Body, f.set)
	}
	var got []store.SupplierCode
	rec = codesDo(f, "GET", "/api/supplier-codes", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); rec.Code != 200 || err != nil || len(got) != 1 || got[0].ProductID != "milk" {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body)
	}
	if rec := codesDo(f, "DELETE", "/api/supplier-codes/04607001234567", ""); rec.Code != 204 || f.deleted != "04607001234567" {
		t.Fatalf("DELETE: %d", rec.Code)
	}
}

func TestSupplierCodeValidation(t *testing.T) {
	f := &fakeCodes{}
	for name, path := range map[string]string{
		"кириллица":   "/api/supplier-codes/%D0%BA%D0%BE%D0%B4",
		"пробел":      "/api/supplier-codes/a%20b",
		"65 символов": "/api/supplier-codes/" + strings.Repeat("a", 65),
		"слэш":        "/api/supplier-codes/a%2Fb",
	} {
		if rec := codesDo(f, "PUT", path, `{"product_id":"milk"}`); rec.Code != 400 && rec.Code != 404 {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if f.set.Code != "" {
		t.Fatalf("невалидный код дошёл до хранилища: %+v", f.set)
	}
	if rec := codesDo(f, "PUT", "/api/supplier-codes/ok", `{}`); rec.Code != 400 {
		t.Fatalf("без product_id: %d %s", rec.Code, rec.Body)
	}
	if rec := codesDo(f, "PUT", "/api/supplier-codes/ok", `{"product_id":`); rec.Code != 400 {
		t.Fatalf("битый JSON: %d", rec.Code)
	}
	f.err = store.ErrSupplierCodeProduct
	rec := codesDo(f, "PUT", "/api/supplier-codes/ok", `{"product_id":"unicorn"}`)
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), CodeUnknownProduct) {
		t.Fatalf("неизвестный продукт: %d %s", rec.Code, rec.Body)
	}
}

func TestSupplierCodeRoutesAbsentWithoutDeps(t *testing.T) {
	rt := NewRouter(Deps{Now: func() time.Time { return standTime }})
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/supplier-codes", nil))
	if rec.Code != 404 {
		t.Fatalf("%d", rec.Code)
	}
}
