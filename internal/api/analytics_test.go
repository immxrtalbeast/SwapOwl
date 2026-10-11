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

type fakeAnalytics struct{ from, to time.Time }

func (f *fakeAnalytics) WriteoffsReport(_ context.Context, from, to time.Time, _ *time.Location) (store.WriteoffsReport, error) {
	f.from, f.to = from, to
	return store.WriteoffsReport{From: from, To: to, TotalMinor: 1234000,
		ByProduct: []store.MoneyRow{{ProductID: "salmon", Reason: "expired", CostMinor: 1234000}}, ByDay: []store.MoneyDay{}}, nil
}

func (f *fakeAnalytics) ShortfallsReport(_ context.Context, from, to time.Time, _ *time.Location) (store.ShortfallsReport, error) {
	f.from, f.to = from, to
	return store.ShortfallsReport{From: from, To: to, ByProduct: []store.MoneyRow{}, ByDay: []store.MoneyDay{}}, nil
}

func TestAnalyticsEndpoints(t *testing.T) {
	now := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	msk := time.FixedZone("MSK", 3*3600)
	fa := &fakeAnalytics{}
	rt := NewRouter(Deps{Analytics: fa, Location: msk, Now: func() time.Time { return now }})
	get := func(url string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		return rec
	}

	rec := get("/api/analytics/writeoffs?from=2026-10-05&to=2026-10-06")
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	// Даты — сутки по часовому поясу ресторана, to включительно.
	if want := time.Date(2026, 10, 4, 21, 0, 0, 0, time.UTC); !fa.from.Equal(want) {
		t.Fatalf("from: %v", fa.from)
	}
	if want := time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC); !fa.to.Equal(want) {
		t.Fatalf("to: %v", fa.to)
	}
	var out store.WriteoffsReport
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.TotalMinor != 1234000 {
		t.Fatalf("body: %v %s", err, rec.Body)
	}

	// Без параметров — последние 7 суток до «сейчас».
	if rec = get("/api/analytics/shortfalls"); rec.Code != 200 || !fa.to.Equal(now) || !fa.from.Equal(now.AddDate(0, 0, -7)) {
		t.Fatalf("defaults: %d %v %v", rec.Code, fa.from, fa.to)
	}
	// RFC 3339 — полуинтервал как есть.
	if rec = get("/api/analytics/shortfalls?from=2026-10-05T00:00:00Z&to=2026-10-06T00:00:00Z"); rec.Code != 200 ||
		!fa.to.Equal(time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("rfc3339: %d %v", rec.Code, fa.to)
	}
	for _, bad := range []string{"?from=вчера", "?to=2026-13-40", "?from=2026-10-06&to=2026-10-05"} {
		if rec = get("/api/analytics/writeoffs" + bad); rec.Code != 400 {
			t.Fatalf("%s: %d", bad, rec.Code)
		}
	}
}
