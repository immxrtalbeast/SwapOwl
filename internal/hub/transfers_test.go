package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// transferPoint — фейковая точка с заданными hot-offers и supply-suggestions; часы стенда 12:00 +03:00.
func transferPoint(t *testing.T, offers, suggestions string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	j := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+goodKey {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(body))
		}
	}
	mux.HandleFunc("/api/clock", j(`{"now":"2026-10-09T12:00:00+03:00","timezone":"Europe/Moscow"}`))
	mux.HandleFunc("/api/alerts", j(`[]`))
	mux.HandleFunc("/api/stop-list", j(`[]`))
	mux.HandleFunc("/api/hot-offers", j(offers))
	mux.HandleFunc("/api/analytics/writeoffs", j(`{"total_minor":0}`))
	if suggestions != "" {
		mux.HandleFunc("/api/supply-suggestions", j(suggestions))
	}
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

const (
	// A: 1200 г лосося под риском, срок через 8 ч; расход 240 г/сут (10 г/ч).
	offersA = `{"offers":[{"dish_id":"steak","dish_name":"Стейк","reasons":[
		{"product_id":"salmon","product_name":"Лосось","batch_id":17,"at_risk":{"qty":1200,"unit":"g"},"expires_at":"2026-10-09T20:00:00+03:00","working_hours_left":5}]}]}`
	suggA = `[{"product_id":"salmon","product_name":"Лосось","rate_per_day":{"qty":240,"unit":"g"},"usable_stock":{"qty":1200,"unit":"g"}}]`
	// B: расход 4800 г/сут (200 г/ч), остаток 100 г.
	suggB = `[{"product_id":"salmon","product_name":"Лосось","rate_per_day":{"qty":4800,"unit":"g"},"usable_stock":{"qty":100,"unit":"g"}}]`
)

func twoPoints(t *testing.T, suggB string) *Collector {
	t.Helper()
	a := transferPoint(t, offersA, suggA)
	b := transferPoint(t, `{"offers":[]}`, suggB)
	col := NewCollector(Config{Points: []Point{
		{ID: "a", Name: "Арбат", URL: a.URL, APIKey: goodKey},
		{ID: "b", Name: "Тверская", URL: b.URL, APIKey: goodKey},
	}}, 2*time.Second, nil)
	col.PollAll(context.Background())
	return col
}

func TestTransferSuggested(t *testing.T) {
	col := twoPoints(t, suggB)
	col.SetTransfers(2, map[string]int64{"salmon": 200000}) // 2000 ₽/кг
	got := col.Transfers()
	if len(got) != 1 {
		t.Fatalf("transfers: %+v", got)
	}
	tr := got[0]
	// Окно: 8 ч − 2 ч = 6 ч; B продаст 200×6 = 1200 г, минус свой остаток 100 г → 1100 г.
	if tr.FromID != "a" || tr.ToID != "b" || tr.Qty.Qty != 1100 || tr.BatchID != 17 {
		t.Errorf("transfer: %+v", tr)
	}
	if tr.SavesMinor == nil || *tr.SavesMinor != 220000 {
		t.Errorf("saves: %v", tr.SavesMinor)
	}
	if want := "Лосось 1,1 кг: Арбат → Тверская, не спишем ≈ 2 200 ₽"; tr.Text != want {
		t.Errorf("text %q, want %q", tr.Text, want)
	}
	if !strings.Contains(tr.Invoice, "Откуда: Арбат") || !strings.Contains(tr.Invoice, "Куда: Тверская") {
		t.Errorf("invoice: %s", tr.Invoice)
	}
}

func TestTransferNotSuggested(t *testing.T) {
	// B расходует медленнее A.
	slow := `[{"product_id":"salmon","rate_per_day":{"qty":100,"unit":"g"},"usable_stock":{"qty":0,"unit":"g"}}]`
	if got := twoPoints(t, slow).Transfers(); len(got) != 0 {
		t.Errorf("slower B: %+v", got)
	}
	// У B нет сведений о расходе (старая точка без маршрута) — подсказки нет, точка не пропала.
	col := twoPoints(t, "")
	if got := col.Transfers(); len(got) != 0 {
		t.Errorf("no demand: %+v", got)
	}
	if s := col.Snapshot(); s[1].Status != StatusOK {
		t.Errorf("point without supply-suggestions: %+v", s[1])
	}
	// Перевозка съедает окно: 8 ч срока < 9 ч запаса.
	col = twoPoints(t, suggB)
	col.SetTransfers(9, nil)
	if got := col.Transfers(); len(got) != 0 {
		t.Errorf("no time: %+v", got)
	}
	// Остатка B хватает до срока на собственные продажи: ёмкости нет.
	full := `[{"product_id":"salmon","rate_per_day":{"qty":4800,"unit":"g"},"usable_stock":{"qty":5000,"unit":"g"}}]`
	if got := twoPoints(t, full).Transfers(); len(got) != 0 {
		t.Errorf("no capacity: %+v", got)
	}
}

func TestTransferWithoutPriceAndAPI(t *testing.T) {
	col := twoPoints(t, suggB)
	got := col.Transfers()
	if len(got) != 1 || got[0].SavesMinor != nil || strings.Contains(got[0].Text, "не спишем") {
		t.Fatalf("no price: %+v", got)
	}
	h := NewServer(col, "hub-token")
	if rr := do(h, "/api/network/transfers", ""); rr.Code != 401 {
		t.Errorf("unauth: %d", rr.Code)
	}
	rr := do(h, "/api/network/transfers", "hub-token")
	var body struct {
		Transfers []Transfer `json:"transfers"`
	}
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &body) != nil || len(body.Transfers) != 1 {
		t.Fatalf("api: %d %s", rr.Code, rr.Body)
	}
	if strings.Contains(rr.Body.String(), goodKey) || strings.Contains(rr.Body.String(), "127.0.0.1") {
		t.Error("leak")
	}
}

func TestSuggestTransfersTopFiveAndSplit(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	exp := now.Add(10 * time.Hour)
	a := PointState{ID: "a", Name: "A", Status: StatusOK, StandTime: &now}
	for i := int64(1); i <= 7; i++ {
		a.RiskBatches = append(a.RiskBatches, RiskBatch{BatchID: i, ProductID: "p" + string(rune('0'+i)), ProductName: "П",
			AtRisk: Qty{Qty: 1000 * i, Unit: "g"}, ExpiresAt: exp})
	}
	b := PointState{ID: "b", Name: "B", Status: StatusOK, StandTime: &now}
	prices := map[string]int64{}
	for i := int64(1); i <= 7; i++ {
		b.Demand = append(b.Demand, Demand{ProductID: "p" + string(rune('0'+i)), RatePerDay: Qty{Qty: 24000, Unit: "g"}})
		prices["p"+string(rune('0'+i))] = 1000
	}
	got := SuggestTransfers([]PointState{a, b}, 2, prices)
	if len(got) != MaxTransfers {
		t.Fatalf("len %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if *got[i-1].SavesMinor < *got[i].SavesMinor {
			t.Errorf("order: %v", got)
		}
	}
	// Одна партия и две принимающие точки: суммарно не больше «под риском».
	c := b
	c.ID, c.Name = "c", "C"
	a.RiskBatches = a.RiskBatches[:1]
	got = SuggestTransfers([]PointState{a, b, c}, 2, prices)
	var sum int64
	for _, tr := range got {
		sum += tr.Qty.Qty
	}
	if sum > 1000 {
		t.Errorf("batch moved twice: %+v", got)
	}
}

func TestTransferConfig(t *testing.T) {
	if h, err := TransferHours(""); err != nil || h != 2 {
		t.Errorf("default: %v %v", h, err)
	}
	if h, err := TransferHours("1.5"); err != nil || h != 1.5 {
		t.Errorf("1.5: %v %v", h, err)
	}
	if _, err := TransferHours("-1"); err == nil {
		t.Error("negative")
	}
	if _, err := ParsePrices(`{"x":-5}`); err == nil {
		t.Error("negative price")
	}
	if m, err := ParsePrices(`{"salmon":200000}`); err != nil || m["salmon"] != 200000 {
		t.Errorf("prices: %v %v", m, err)
	}
	if got := FormatQty(Qty{Qty: 1200, Unit: "g"}); got != "1,2 кг" {
		t.Errorf("fmt: %s", got)
	}
}

// algoPoint — точка A: по умолчанию отвечает алгоритмом a (at_risk суммарный, 1200 г в «партии 17»), с ?algo=b — честно 700 г.
func algoPoint(t *testing.T, algo string, withForecast bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	j := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+goodKey {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(body))
		}
	}
	offers := func(algo string, atRisk int) string {
		return `{"algo":"` + algo + `","offers":[{"dish_id":"steak","dish_name":"Стейк","reasons":[{"product_id":"salmon","product_name":"Лосось",` +
			`"batch_id":17,"at_risk":{"qty":` + itoa(atRisk) + `,"unit":"g"},"expires_at":"2026-10-09T20:00:00+03:00"}]}]}`
	}
	mux.HandleFunc("/api/clock", j(`{"now":"2026-10-09T12:00:00+03:00","timezone":"Europe/Moscow"}`))
	mux.HandleFunc("/api/alerts", j(`[]`))
	mux.HandleFunc("/api/stop-list", j(`[]`))
	mux.HandleFunc("/api/hot-offers", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("algo") == "b" {
			j(offers("b", 700))(w, r)
			return
		}
		j(offers(algo, 1200))(w, r)
	})
	mux.HandleFunc("/api/analytics/writeoffs", j(`{"total_minor":0}`))
	mux.HandleFunc("/api/supply-suggestions", j(suggA))
	if withForecast {
		// 700 г под риском на 1400 ₽ → 2000 ₽/кг.
		mux.HandleFunc("/api/forecast/writeoffs", j(`{"total_minor":140000,"products":[{"product_id":"salmon","at_risk":{"qty":700,"unit":"g"},"total_minor":140000}]}`))
	}
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestTransferAlgoAUsesPerBatchRisk(t *testing.T) {
	a := algoPoint(t, "a", false)
	b := transferPoint(t, `{"offers":[]}`, suggB)
	col := NewCollector(Config{Points: []Point{
		{ID: "a", Name: "Арбат", URL: a.URL, APIKey: goodKey},
		{ID: "b", Name: "Тверская", URL: b.URL, APIKey: goodKey},
	}}, 2*time.Second, nil)
	col.PollAll(context.Background())
	got := col.Transfers()
	if len(got) != 1 || got[0].Qty.Qty != 700 {
		t.Fatalf("algo a must not inflate the batch: %+v", got)
	}
}

func TestTransferPriceFromForecast(t *testing.T) {
	a := algoPoint(t, "b", true)
	b := transferPoint(t, `{"offers":[]}`, suggB)
	col := NewCollector(Config{Points: []Point{
		{ID: "a", Name: "Арбат", URL: a.URL, APIKey: goodKey},
		{ID: "b", Name: "Тверская", URL: b.URL, APIKey: goodKey},
	}}, 2*time.Second, nil)
	col.PollAll(context.Background())
	// HUB_PRICES пуст: цена берётся из каталога точки A. Объём ограничен ёмкостью B (1100 г) → 2200 ₽.
	got := col.Transfers()
	if len(got) != 1 || got[0].SavesMinor == nil || *got[0].SavesMinor != 220000 {
		t.Fatalf("saves from forecast: %+v", got)
	}
	// HUB_PRICES — только запасной вариант: цена точки главнее.
	col.SetTransfers(2, map[string]int64{"salmon": 1})
	if got = col.Transfers(); *got[0].SavesMinor != 220000 {
		t.Errorf("point price must win: %+v", got[0])
	}
}
