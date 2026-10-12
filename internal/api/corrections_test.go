package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

type fakeCorrections struct {
	wo   store.WriteoffInput
	inv  store.InventoryInput
	can  store.CancelInput
	open store.OpenInput
	err  error
	repl bool
}

func (f *fakeCorrections) ProductUnits(_ context.Context, ids []string) (map[string]domain.Unit, error) {
	m := map[string]domain.Unit{}
	for _, id := range ids {
		if id == "salmon" {
			m[id] = domain.UnitG
		}
	}
	return m, nil
}
func (f *fakeCorrections) BatchProduct(_ context.Context, id int64) (string, domain.Unit, error) {
	if id != 7 {
		return "", "", store.ErrUnknownBatch
	}
	return "salmon", domain.UnitG, nil
}
func (f *fakeCorrections) WriteOffBatch(_ context.Context, in store.WriteoffInput, _ func() time.Time) (store.WriteoffResult, error) {
	f.wo = in
	return store.WriteoffResult{ID: in.ID, BatchID: in.BatchID, Replayed: f.repl}, f.err
}
func (f *fakeCorrections) Inventory(_ context.Context, in store.InventoryInput, _ func() time.Time) (store.InventoryResult, error) {
	f.inv = in
	return store.InventoryResult{ID: in.ID, Replayed: f.repl}, f.err
}
func (f *fakeCorrections) CancelOrder(_ context.Context, in store.CancelInput, _ func() time.Time) (store.CancelResult, error) {
	f.can = in
	return store.CancelResult{ID: in.ID, OrderID: in.OrderID, Replayed: f.repl}, f.err
}

func postCorr(t *testing.T, f *fakeCorrections, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rt := NewRouter(Deps{Now: func() time.Time { return standTime }, Corrections: f})
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, req)
	return rec
}

func TestPostWriteoff(t *testing.T) {
	f := &fakeCorrections{}
	rec := postCorr(t, f, "/api/writeoffs", `{"id":"w1","batch_id":7,"qty":"1.5","unit":"kg","reason":"spoilage"}`)
	if rec.Code != http.StatusCreated || f.wo != (store.WriteoffInput{ID: "w1", BatchID: 7, Qty: 1500, Reason: "spoilage"}) {
		t.Fatalf("%d %s %+v", rec.Code, rec.Body, f.wo)
	}
	f.repl = true
	if rec := postCorr(t, f, "/api/writeoffs", `{"id":"w1","batch_id":7,"qty":"1500","reason":"damage"}`); rec.Code != http.StatusOK {
		t.Fatalf("replay: %d", rec.Code)
	}
}

func TestPostWriteoffErrors(t *testing.T) {
	cases := []struct {
		name, body string
		err        error
		code       int
		errCode    string
	}{
		{"bad reason", `{"id":"w","batch_id":7,"qty":"1","reason":"theft"}`, nil, 400, CodeValidation},
		{"expired is not a manual reason", `{"id":"w","batch_id":7,"qty":"1","reason":"expired"}`, nil, 400, CodeValidation},
		{"no batch", `{"id":"w","qty":"1","reason":"damage"}`, nil, 400, CodeValidation},
		{"unknown batch", `{"id":"w","batch_id":8,"qty":"1","reason":"damage"}`, nil, 404, CodeUnknownBatch},
		{"zero qty", `{"id":"w","batch_id":7,"qty":"0","reason":"damage"}`, nil, 400, CodeValidation},
		{"number qty", `{"id":"w","batch_id":7,"qty":5,"reason":"damage"}`, nil, 400, CodeValidation},
		{"conflict", `{"id":"w","batch_id":7,"qty":"1","reason":"damage"}`, store.ErrCorrectionConflict, 409, CodeCorrectionConflict},
		{"too much", `{"id":"w","batch_id":7,"qty":"1","reason":"damage"}`, store.ErrInsufficientBatch, 409, CodeInsufficientBatch},
	}
	for _, c := range cases {
		rec := postCorr(t, &fakeCorrections{err: c.err}, "/api/writeoffs", c.body)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), `"`+c.errCode+`"`) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
}

func TestPostInventory(t *testing.T) {
	f := &fakeCorrections{}
	rec := postCorr(t, f, "/api/inventory", `{"id":"i1","product_id":"salmon","counted":"0"}`)
	if rec.Code != http.StatusCreated || f.inv != (store.InventoryInput{ID: "i1", ProductID: "salmon", Counted: 0}) {
		t.Fatalf("zero count must be accepted: %d %s %+v", rec.Code, rec.Body, f.inv)
	}
	rec = postCorr(t, f, "/api/inventory", `{"id":"i2","product_id":"salmon","counted":"2.5","unit":"kg"}`)
	if rec.Code != http.StatusCreated || f.inv.Counted != 2500 {
		t.Fatalf("%d %s %+v", rec.Code, rec.Body, f.inv)
	}
	if rec := postCorr(t, f, "/api/inventory", `{"id":"i3","product_id":"nope","counted":"1"}`); rec.Code != 400 || !strings.Contains(rec.Body.String(), CodeUnknownProduct) {
		t.Fatalf("unknown product: %d %s", rec.Code, rec.Body)
	}
	if rec := postCorr(t, f, "/api/inventory", `{"id":"i4","product_id":"salmon","counted":"-1"}`); rec.Code != 400 {
		t.Fatalf("negative: %d", rec.Code)
	}
	if rec := postCorr(t, &fakeCorrections{err: store.ErrCorrectionConflict}, "/api/inventory", `{"id":"i1","product_id":"salmon","counted":"5"}`); rec.Code != 409 {
		t.Fatalf("conflict: %d", rec.Code)
	}
}

func TestPostCancel(t *testing.T) {
	f := &fakeCorrections{}
	rec := postCorr(t, f, "/api/orders/"+oid+"/cancel", `{"id":"c1"}`)
	if rec.Code != http.StatusCreated || f.can != (store.CancelInput{ID: "c1", OrderID: strings.ToLower(oid)}) {
		t.Fatalf("%d %s %+v", rec.Code, rec.Body, f.can)
	}
	for err, want := range map[error][2]string{
		store.ErrUnknownOrder:        {"404", CodeUnknownOrder},
		store.ErrOrderNotCancellable: {"409", CodeOrderNotCancellable},
		store.ErrAlreadyCancelled:    {"409", CodeAlreadyCancelled},
		store.ErrCorrectionConflict:  {"409", CodeCorrectionConflict},
	} {
		rec := postCorr(t, &fakeCorrections{err: err}, "/api/orders/"+oid+"/cancel", `{"id":"c1"}`)
		if !strings.HasPrefix(rec.Result().Status, want[0]) || !strings.Contains(rec.Body.String(), want[1]) {
			t.Errorf("%v: %d %s", err, rec.Code, rec.Body)
		}
	}
	if rec := postCorr(t, f, "/api/orders/not-a-uuid/cancel", `{"id":"c1"}`); rec.Code != 400 {
		t.Fatalf("bad order id: %d", rec.Code)
	}
	if rec := postCorr(t, f, "/api/orders/"+oid+"/cancel", `{}`); rec.Code != 400 {
		t.Fatalf("missing id: %d", rec.Code)
	}
}

func TestCorrectionRoutesAbsentWithoutDep(t *testing.T) {
	rt := NewRouter(Deps{})
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/writeoffs", strings.NewReader(`{}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d", rec.Code)
	}
}
