package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

func (f *fakeCorrections) OpenBatch(_ context.Context, in store.OpenInput, _ func() time.Time) (store.OpenResult, error) {
	f.open = in
	return store.OpenResult{ID: in.ID, ParentBatchID: in.BatchID, Replayed: f.repl}, f.err
}

func TestPostOpen(t *testing.T) {
	f := &fakeCorrections{}
	rec := postCorr(t, f, "/api/batches/7/open", `{"id":"o1"}`)
	if rec.Code != http.StatusCreated || f.open.ID != "o1" || f.open.BatchID != 7 || f.open.Qty != nil {
		t.Fatalf("default qty: %d %s %+v", rec.Code, rec.Body, f.open)
	}
	rec = postCorr(t, f, "/api/batches/7/open", `{"id":"o2","qty":"0.5","unit":"kg"}`)
	if rec.Code != http.StatusCreated || f.open.Qty == nil || *f.open.Qty != 500 {
		t.Fatalf("explicit qty: %d %s %+v", rec.Code, rec.Body, f.open)
	}
	f.repl = true
	if rec := postCorr(t, f, "/api/batches/7/open", `{"id":"o2","qty":"500"}`); rec.Code != http.StatusOK {
		t.Fatalf("replay: %d", rec.Code)
	}
}

func TestPostOpenErrors(t *testing.T) {
	cases := []struct {
		name, path, body string
		err              error
		code             int
		errCode          string
	}{
		{"bad path id", "/api/batches/x/open", `{"id":"o"}`, nil, 400, CodeValidation},
		{"no id", "/api/batches/7/open", `{}`, nil, 400, CodeValidation},
		{"unit without qty", "/api/batches/7/open", `{"id":"o","unit":"g"}`, nil, 400, CodeValidation},
		{"number qty", "/api/batches/7/open", `{"id":"o","qty":5}`, nil, 400, CodeValidation},
		{"zero qty", "/api/batches/7/open", `{"id":"o","qty":"0"}`, nil, 400, CodeValidation},
		{"wrong dimension", "/api/batches/7/open", `{"id":"o","qty":"1","unit":"l"}`, nil, 400, CodeValidation},
		{"unknown batch", "/api/batches/8/open", `{"id":"o","qty":"1"}`, nil, 404, CodeUnknownBatch},
		{"unknown batch, default qty", "/api/batches/8/open", `{"id":"o"}`, store.ErrUnknownBatch, 404, CodeUnknownBatch},
		{"qty above remainder", "/api/batches/7/open", `{"id":"o","qty":"9"}`, domain.ErrOpenQty, 400, CodeValidation},
		{"no shelf after open", "/api/batches/7/open", `{"id":"o"}`, domain.ErrOpenNoShelf, 400, CodeValidation},
		{"expired or empty", "/api/batches/7/open", `{"id":"o"}`, domain.ErrOpenClosed, 409, CodeBatchClosed},
		{"already opened", "/api/batches/7/open", `{"id":"o"}`, store.ErrAlreadyOpened, 409, CodeAlreadyOpened},
		{"id reused", "/api/batches/7/open", `{"id":"o"}`, store.ErrCorrectionConflict, 409, CodeCorrectionConflict},
	}
	for _, c := range cases {
		rec := postCorr(t, &fakeCorrections{err: c.err}, c.path, c.body)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), `"`+c.errCode+`"`) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
	rec := postCorr(t, &fakeCorrections{err: domain.ErrOpenNoShelf}, "/api/batches/7/open", `{"id":"o"}`)
	if !strings.Contains(rec.Body.String(), "вскрытие не меняет срок") {
		t.Errorf("message: %s", rec.Body)
	}
}
