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

type fakeOutbox struct {
	entries  []store.OutboxEntry
	gotAfter int64
	gotLimit int
	ackID    int64
	ackOK    bool
	ackErr   string
	ackNow   time.Time
	missing  bool
}

func (f *fakeOutbox) ListOutbox(_ context.Context, after int64, limit int) ([]store.OutboxEntry, error) {
	f.gotAfter, f.gotLimit = after, limit
	return f.entries, nil
}

func (f *fakeOutbox) AckOutbox(_ context.Context, id int64, ok bool, msg string, now time.Time) error {
	if f.missing {
		return store.ErrOutboxNotFound
	}
	f.ackID, f.ackOK, f.ackErr, f.ackNow = id, ok, msg, now
	return nil
}

func outboxRouter(o Outbox) http.Handler {
	return NewRouter(Deps{Now: func() time.Time { return standTime }, Outbox: o}).Handler()
}

func TestOutboxListParsesParams(t *testing.T) {
	f := &fakeOutbox{entries: []store.OutboxEntry{{ID: 5, Kind: "stoplist", Payload: json.RawMessage(`[]`)}}}
	h := outboxRouter(f)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/outbox?after=4&limit=10", nil))
	if rec.Code != 200 || f.gotAfter != 4 || f.gotLimit != 10 || !strings.Contains(rec.Body.String(), `"id":5`) {
		t.Fatalf("code=%d %+v %s", rec.Code, f, rec.Body.String())
	}
	for _, bad := range []string{"after=-1", "after=x", "limit=0", "limit=1001", "limit=x"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/outbox?"+bad, nil))
		if rec.Code != 400 {
			t.Errorf("%s: code=%d", bad, rec.Code)
		}
	}
}

func TestOutboxAck(t *testing.T) {
	f := &fakeOutbox{}
	h := outboxRouter(f)
	post := func(path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := post("/api/outbox/7/ack", `{"ok":false,"error":"boom"}`); rec.Code != 200 ||
		f.ackID != 7 || f.ackOK || f.ackErr != "boom" || !f.ackNow.Equal(standTime) {
		t.Fatalf("fail ack: %d %s %+v", rec.Code, rec.Body.String(), f)
	}
	if rec := post("/api/outbox/7/ack", `{"ok":true}`); rec.Code != 200 || !f.ackOK {
		t.Fatalf("ok ack: %d %+v", rec.Code, f)
	}
	for path, body := range map[string]string{
		"/api/outbox/7/ack": `{}`, "/api/outbox/x/ack": `{"ok":true}`, "/api/outbox/7/ack?": `not json`,
	} {
		if rec := post(path, body); rec.Code != 400 && rec.Code != 404 {
			t.Errorf("%s %s: code=%d", path, body, rec.Code)
		}
	}
	f.missing = true
	if rec := post("/api/outbox/99/ack", `{"ok":true}`); rec.Code != 404 {
		t.Fatalf("missing: %d", rec.Code)
	}
}

func TestOutboxNotRegisteredWithoutDep(t *testing.T) {
	rec := httptest.NewRecorder()
	NewRouter(Deps{}).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/outbox", nil))
	if rec.Code != 404 {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestStopListPayloadOnlyStopped(t *testing.T) {
	snap := store.MenuSnapshot{
		Dishes: []domain.Dish{
			{ID: "d1", Name: "A", Recipe: []domain.RecipeItem{{ProductID: "p1", Quantity: 10}}},
			{ID: "d2", Name: "B", Recipe: []domain.RecipeItem{{ProductID: "p2", Quantity: 1}}},
		},
		Available: map[string]domain.Qty{"p1": 0, "p2": 5},
	}
	got := StopListPayload(snap)
	if len(got) != 1 || got[0].ID != "d1" || len(got[0].Missing) != 1 {
		t.Fatalf("%+v", got)
	}
}
