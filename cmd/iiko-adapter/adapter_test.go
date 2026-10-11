package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"umirhack/internal/iiko/client"
	"umirhack/internal/iiko/fake"
	"umirhack/internal/iiko/mapping"
	"umirhack/internal/seed"
)

type call struct {
	Method, Path, Auth string
	Body               []byte
}

// stubAPI — httptest-сервер вместо нашего API: записывает вызовы, идемпотентность — по id.
type stubAPI struct {
	mu    sync.Mutex
	calls []call
	stop  []string
	seen  map[string]bool
}

func (s *stubAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call{r.Method, r.URL.Path, r.Header.Get("Authorization"), b})
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/stop-list":
		rows := []map[string]any{}
		for _, id := range s.stop {
			rows = append(rows, map[string]any{"id": id, "available": false})
		}
		_ = json.NewEncoder(w).Encode(rows)
	case r.URL.Path == "/api/deliveries" || r.URL.Path == "/api/orders":
		var v struct{ ID string }
		_ = json.Unmarshal(b, &v)
		if s.seen[v.ID] {
			w.WriteHeader(http.StatusOK)
		} else {
			s.seen[v.ID] = true
			w.WriteHeader(http.StatusCreated)
		}
		_, _ = w.Write([]byte(`{}`))
	default:
		_, _ = w.Write([]byte(`{}`))
	}
}

func (s *stubAPI) count(method, path string) (n int) {
	for _, c := range s.calls {
		if c.Method == method && c.Path == path {
			n++
		}
	}
	return
}

func setup(t *testing.T, now time.Time) (*adapter, *stubAPI, *client.Client) {
	t.Helper()
	loc, _ := time.LoadLocation("Europe/Moscow")
	fk, err := fake.New(fake.Options{Seed: 7, Origin: time.Date(2026, 1, 1, 0, 0, 0, 0, loc), Login: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	fts := httptest.NewServer(fk.Handler())
	t.Cleanup(fts.Close)
	stub := &stubAPI{seen: map[string]bool{}, stop: []string{seed.Dishes()[0].ID}}
	ats := httptest.NewServer(stub)
	t.Cleanup(ats.Close)
	ic := client.New(fts.URL, "u", "p", nil)
	a := &adapter{
		iiko: ic, api: &apiClient{base: ats.URL, key: "secret", hc: http.DefaultClient},
		loc: loc, now: func() time.Time { return now },
		statePath: filepath.Join(t.TempDir(), "state.json"),
		start:     time.Date(2026, 1, 1, 0, 0, 0, 0, loc),
		defaults:  mapping.Defaults{PriceMinorPer1000: 10000, LeadDays: 2, PackSize: 1000, PriorDaily: 10},
		log:       log.New(io.Discard, "", 0),
	}
	return a, stub, ic
}

func TestRunDay(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, ic := setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	if err := a.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stub.count(http.MethodPut, "/api/catalog") != 1 {
		t.Fatal("каталог не передан")
	}
	nd, no := stub.count(http.MethodPost, "/api/deliveries"), stub.count(http.MethodPost, "/api/orders")
	if nd == 0 || no != 3 { // закрытые дни 1, 2, 3 января
		t.Fatalf("поставок %d, заказов %d", nd, no)
	}
	for _, c := range stub.calls {
		if c.Auth != "Bearer secret" {
			t.Fatalf("нет ключа: %+v", c)
		}
		if c.Path == "/api/orders" {
			var o mapping.Order
			_ = json.Unmarshal(c.Body, &o)
			if o.Source != "pos" || len(o.ID) != 36 || len(o.Items) == 0 {
				t.Fatalf("заказ: %s", c.Body)
			}
		}
	}
	items, err := ic.StopList(context.Background())
	if err != nil || len(items) != 1 || items[0].ProductID != seed.Dishes()[0].ID {
		t.Fatalf("стоп-лист в iiko: %v %v", items, err)
	}

	// Повторный проход: заказы за закрытые дни не повторяются (курсор), стоп-лист меняется.
	stub.stop = nil
	if err := a.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := stub.count(http.MethodPost, "/api/orders"); got != no {
		t.Fatalf("заказы повторены: %d", got)
	}
	if items, _ := ic.StopList(context.Background()); len(items) != 0 {
		t.Fatalf("стоп-лист не очищен: %v", items)
	}
	// Накладные текущего дня читаются заново; повторы — идемпотентность API.
	if st, _ := loadState(a.statePath); st.Sales != "2026-01-04" {
		t.Fatalf("курсор продаж: %+v", st)
	}
}

func TestOrderIDStable(t *testing.T) {
	if mapping.OrderID("2026-01-01") != mapping.OrderID("2026-01-01") || mapping.OrderID("2026-01-01") == mapping.OrderID("2026-01-02") {
		t.Fatal("UUID v5 нестабилен")
	}
	if got := mapping.UUIDv5(mapping.Namespace, "python.org"); got != "886313e1-3b8a-5372-9b90-0c9aee199e5d" {
		t.Fatalf("UUID v5: %s", got)
	}
}

// rejectOrder отвечает 400 на заказ с заданным id (как unknown_dish), остальное — как stubAPI.
type rejectOrder struct {
	*stubAPI
	bad string
}

func (r rejectOrder) ServeHTTP(w http.ResponseWriter, q *http.Request) {
	if q.URL.Path == "/api/orders" {
		b, _ := io.ReadAll(q.Body)
		var o mapping.Order
		_ = json.Unmarshal(b, &o)
		if o.ID == r.bad {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"unknown_dish"}}`))
			return
		}
		q.Body = io.NopCloser(bytes.NewReader(b))
	}
	r.stubAPI.ServeHTTP(w, q)
}

func TestRejectedDayDoesNotStopCursor(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, _ := setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	s := httptest.NewServer(rejectOrder{stub, mapping.OrderID("2026-01-02")})
	defer s.Close()
	a.api.base = s.URL
	if err := a.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ := loadState(a.statePath)
	if n := stub.count(http.MethodPost, "/api/orders"); n != 2 || st.Sales != "2026-01-04" {
		t.Fatalf("заказов принято %d, курсор продаж %q", n, st.Sales)
	}
}

func TestSoldDishNotInCatalogIsSkipped(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, _ := setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	if err := a.syncSalesWithout(t, stub, seed.Dishes()[0].ID); err != nil {
		t.Fatal(err)
	}
	for _, c := range stub.calls {
		if c.Path == "/api/orders" && bytes.Contains(c.Body, []byte(`"`+seed.Dishes()[0].ID+`"`)) {
			t.Fatalf("блюдо вне каталога передано: %s", c.Body)
		}
	}
}

// syncSalesWithout выкидывает блюдо из «принятого каталога» и передаёт продажи.
func (a *adapter) syncSalesWithout(t *testing.T, _ *stubAPI, dish string) error {
	t.Helper()
	a.dishes = map[string]bool{}
	for _, d := range seed.Dishes() {
		if d.ID != dish {
			a.dishes[d.ID] = true
		}
	}
	st := state{}
	return a.syncSales(context.Background(), &st, dayStart(a.now(), a.loc))
}

// noCatalog: каталог не принимается (503), заказы без каталога дали бы 400 unknown_dish.
type noCatalog struct{ *stubAPI }

func (n noCatalog) ServeHTTP(w http.ResponseWriter, q *http.Request) {
	switch q.URL.Path {
	case "/api/catalog":
		w.WriteHeader(http.StatusServiceUnavailable)
	case "/api/orders":
		w.WriteHeader(http.StatusBadRequest)
	default:
		n.stubAPI.ServeHTTP(w, q)
	}
}

func TestCatalogFailKeepsCursors(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, _ := setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	s := httptest.NewServer(noCatalog{stub})
	defer s.Close()
	a.api.base = s.URL
	if err := a.runOnce(context.Background()); err == nil {
		t.Fatal("ожидалась ошибка справочников")
	}
	if st, _ := loadState(a.statePath); st.Sales != "" || st.Invoices != "" {
		t.Fatalf("курсоры сдвинуты без каталога: %+v", st)
	}
}

func TestReconcile(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	now := time.Date(2026, 1, 4, 12, 0, 0, 0, loc)
	a, _, ic := setup(t, now)
	bal, err := ic.Balance(context.Background(), now)
	if err != nil || len(bal) == 0 {
		t.Fatalf("остатки iiko: %v %v", bal, err)
	}
	products, _ := ic.Products(context.Background())
	factor := map[string]int64{}
	for _, p := range products {
		u, _ := mapping.UnitOf(p.MainUnit)
		factor[p.ID] = u.Factor
	}
	// У нас всё как в iiko, кроме одного продукта: недостача 300 базовых единиц.
	var rows []map[string]any
	var skewed string
	for _, b := range bal {
		qty := int64(b.Amount*float64(factor[b.Product]) + 0.5)
		if skewed == "" && qty >= 300 {
			skewed, qty = b.Product, qty-300
		}
		rows = append(rows, map[string]any{"id": b.Product, "name": b.Product, "available": map[string]any{"qty": qty, "unit": "g"}})
	}
	if skewed == "" {
		t.Fatal("в фейке нет продукта с остатком")
	}
	ats := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("reconcile должен только читать: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer ats.Close()
	a.api.base = ats.URL

	var buf bytes.Buffer
	if err := a.runReconcile(context.Background(), &buf, true, false); err != nil {
		t.Fatal(err)
	}
	var got []struct {
		ProductID string `json:"product_id"`
		Diff      int64  `json:"diff"`
		DiffMinor int64  `json:"diff_minor"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ProductID != skewed || got[0].Diff != -300 || got[0].DiffMinor >= 0 {
		t.Fatalf("ждали одно расхождение по %s (−300): %s", skewed, buf.String())
	}
	buf.Reset()
	if err := a.runReconcile(context.Background(), &buf, false, false); err != nil || !bytes.Contains(buf.Bytes(), []byte(skewed)) {
		t.Fatalf("таблица: %v\n%s", err, buf.String())
	}
}
