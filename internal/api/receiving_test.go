package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"umirhack/internal/store"
)

type fakeReceiving struct {
	in   store.ExpiryInput
	err  error
	list []store.ReceivingBatch
}

func (f *fakeReceiving) ConfirmExpiry(_ context.Context, in store.ExpiryInput, _ func() time.Time, _ *time.Location) (store.ExpiryResult, error) {
	f.in = in
	return store.ExpiryResult{BatchID: in.BatchID, ProductID: "salmon", ExpiresAt: standTime.Add(time.Hour),
		OldExpiresAt: standTime.Add(-time.Hour), ExpirySource: store.ExpirySourceConfirmed}, f.err
}

func (f *fakeReceiving) ListReceiving(context.Context, time.Time) ([]store.ReceivingBatch, error) {
	return f.list, f.err
}

func recvDo(f *fakeReceiving, method, path, body string) *httptest.ResponseRecorder {
	rt := NewRouter(Deps{Now: func() time.Time { return standTime }, Receiving: f})
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, req)
	return rec
}

func TestPatchExpiryOK(t *testing.T) {
	f := &fakeReceiving{}
	rec := recvDo(f, http.MethodPatch, "/api/batches/7/expiry", `{"expires_on":"2026-10-20"}`)
	if rec.Code != 200 || f.in != (store.ExpiryInput{BatchID: 7, ExpiresOn: "2026-10-20"}) ||
		!strings.Contains(rec.Body.String(), `"expiry_source":"confirmed"`) {
		t.Fatalf("%d %s %+v", rec.Code, rec.Body, f.in)
	}
	rec = recvDo(f, http.MethodPatch, "/api/batches/7/expiry", `{"shelf_life_hours":72}`)
	if rec.Code != 200 || f.in != (store.ExpiryInput{BatchID: 7, ShelfLifeHours: 72}) {
		t.Fatalf("%d %s %+v", rec.Code, rec.Body, f.in)
	}
}

func TestPatchExpiryErrors(t *testing.T) {
	cases := []struct {
		name, path, body string
		err              error
		code             int
		errCode          string
	}{
		{"both fields", "/api/batches/7/expiry", `{"expires_on":"2026-10-20","shelf_life_hours":5}`, nil, 400, CodeValidation},
		{"no fields", "/api/batches/7/expiry", `{}`, nil, 400, CodeValidation},
		{"bad date", "/api/batches/7/expiry", `{"expires_on":"20.10.2026"}`, nil, 400, CodeValidation},
		{"zero hours", "/api/batches/7/expiry", `{"shelf_life_hours":0}`, nil, 400, CodeValidation},
		{"huge hours", "/api/batches/7/expiry", `{"shelf_life_hours":99999999}`, nil, 400, CodeValidation},
		{"bad id", "/api/batches/x/expiry", `{"shelf_life_hours":5}`, nil, 400, CodeValidation},
		{"unknown field", "/api/batches/7/expiry", `{"shelf_life_hours":5,"x":1}`, nil, 400, CodeValidation},
		{"unknown batch", "/api/batches/7/expiry", `{"shelf_life_hours":5}`, store.ErrUnknownBatch, 404, CodeUnknownBatch},
		{"closed", "/api/batches/7/expiry", `{"shelf_life_hours":5}`, store.ErrBatchClosed, 409, CodeBatchClosed},
		{"past", "/api/batches/7/expiry", `{"shelf_life_hours":5}`, store.ErrExpiryNotFuture, 409, CodeExpiryNotFuture},
	}
	for _, c := range cases {
		rec := recvDo(&fakeReceiving{err: c.err}, http.MethodPatch, c.path, c.body)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.errCode) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
}

func TestGetReceiving(t *testing.T) {
	f := &fakeReceiving{list: []store.ReceivingBatch{{ID: 3, ProductID: "salmon", ExpirySource: store.ExpirySourceProductDefault}}}
	rec := recvDo(f, http.MethodGet, "/api/receiving", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":3`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := recvDo(f, http.MethodPost, "/api/receiving", "{}"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post: %d", rec.Code)
	}
}
