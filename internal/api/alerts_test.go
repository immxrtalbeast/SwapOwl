package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"umirhack/internal/store"
)

type fakeMonitor struct {
	alerts              []store.AlertView
	gotProduct, gotKind string
	gotLimit            int
	gotNow              time.Time
}

func (f *fakeMonitor) ListAlerts(_ context.Context, now time.Time) ([]store.AlertView, error) {
	f.gotNow = now
	return f.alerts, nil
}

func (f *fakeMonitor) ListMovements(_ context.Context, p, k string, l int) ([]store.MovementView, error) {
	f.gotProduct, f.gotKind, f.gotLimit = p, k, l
	return []store.MovementView{}, nil
}

func monitorRouter(m Monitor) *Router {
	return NewRouter(Deps{Now: func() time.Time { return standTime }, Monitor: m})
}

func TestAlertsReturnsFeedAtStandTime(t *testing.T) {
	m := &fakeMonitor{alerts: []store.AlertView{{BatchID: 7, ProductID: "salmon", Status: "expiring", Doomed: true,
		Remaining: store.QtyJSON{Qty: 500, Unit: "g"}}}}
	rec := httptest.NewRecorder()
	monitorRouter(m).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/alerts", nil))
	if rec.Code != 200 || !m.gotNow.Equal(standTime) {
		t.Fatalf("code=%d now=%v", rec.Code, m.gotNow)
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got) != 1 ||
		got[0]["batch_id"] != float64(7) || got[0]["doomed"] != true || got[0]["status"] != "expiring" {
		t.Fatalf("body: %s %v", rec.Body.String(), err)
	}
}

func TestMovementsParsesFilters(t *testing.T) {
	m := &fakeMonitor{}
	h := monitorRouter(m).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/movements?product_id=salmon&kind=sale&limit=5", nil))
	if rec.Code != 200 || m.gotProduct != "salmon" || m.gotKind != "sale" || m.gotLimit != 5 {
		t.Fatalf("code=%d %+v", rec.Code, m)
	}
	for _, bad := range []string{"limit=0", "limit=abc", "limit=1001", "limit=-1"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/movements?"+bad, nil))
		if rec.Code != 400 || decodeErr(t, rec).Error.Code != "validation_error" {
			t.Fatalf("%s: %d %s", bad, rec.Code, rec.Body.String())
		}
	}
}

func TestMonitorRoutesAbsentWithoutDep(t *testing.T) {
	rec := httptest.NewRecorder()
	monitorRouter(nil).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/alerts", nil))
	if rec.Code != 404 {
		t.Fatalf("code=%d", rec.Code)
	}
}
