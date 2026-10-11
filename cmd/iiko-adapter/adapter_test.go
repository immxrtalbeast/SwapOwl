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
	"strconv"
	"strings"
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
	seen  map[string]bool

	outbox  []outboxEntry   // недоставленные
	acks    map[int64][]ack // id → поступившие ack
	failAck bool
}

type ack struct {
	OK    bool
	Error string
}

func (s *stubAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call{r.Method, r.URL.Path, r.Header.Get("Authorization"), b})
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/outbox":
		if s.outbox == nil {
			s.outbox = []outboxEntry{}
		}
		_ = json.NewEncoder(w).Encode(s.outbox)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/outbox/"):
		id, _ := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/outbox/"), "/ack"), 10, 64)
		var in struct {
			OK    bool
			Error string
		}
		_ = json.Unmarshal(b, &in)
		if s.acks == nil {
			s.acks = map[int64][]ack{}
		}
		s.acks[id] = append(s.acks[id], ack{in.OK, in.Error})
		rest := s.outbox[:0:0]
		for _, e := range s.outbox {
			switch {
			case e.ID != id:
				rest = append(rest, e)
			case !in.OK:
				e.Attempts++
				rest = append(rest, e)
			}
		}
		s.outbox = rest
		_, _ = w.Write([]byte(`{"ok":true}`))
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
	stub := &stubAPI{seen: map[string]bool{}}
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
		mode:      modeLive, shadowPath: filepath.Join(t.TempDir(), "shadow.json"),
		retryBase: time.Second, retryMax: 8 * time.Second, retryAt: map[int64]time.Time{},
	}
	return a, stub, ic
}

func TestRunDay(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, ic := setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	stub.outbox = []outboxEntry{stopMsg(1, seed.Dishes()[0].ID)}
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
	stub.outbox = []outboxEntry{stopMsg(2)}
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

func stopMsg(id int64, dishes ...string) outboxEntry {
	rows := []map[string]any{}
	for _, d := range dishes {
		rows = append(rows, map[string]any{"id": d, "name": d, "available": false})
	}
	p, _ := json.Marshal(rows)
	return outboxEntry{ID: id, Kind: kindStoplist, Payload: p, CreatedAt: time.Date(2026, 1, 4, 9, 0, int(id), 0, time.UTC)}
}

func TestBackoff(t *testing.T) {
	for n, want := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 4: 8 * time.Second, 9: 8 * time.Second} {
		if got := backoff(time.Second, 8*time.Second, n); got != want {
			t.Fatalf("попытка %d: %v, ждали %v", n, got, want)
		}
	}
}

// Из нескольких накопившихся сообщений в iiko уходит последнее, все подтверждаются.
func TestOutboxLiveSendsLatestAndAcksAll(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, ic := setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	d := seed.Dishes()
	stub.outbox = []outboxEntry{stopMsg(1, d[0].ID), stopMsg(2, d[0].ID, d[1].ID)}
	if err := a.processOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, err := ic.StopList(context.Background())
	if err != nil || len(items) != 2 {
		t.Fatalf("стоп-лист в iiko: %v %v", items, err)
	}
	if len(stub.outbox) != 0 || len(stub.acks[1]) != 1 || !stub.acks[1][0].OK || !stub.acks[2][0].OK {
		t.Fatalf("ack: %+v, осталось %v", stub.acks, stub.outbox)
	}
}

// Ошибка iiko: ack с ошибкой, пауза с нарастанием, после паузы повтор доставляет.
func TestOutboxLiveRetriesWithBackoff(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, ic := setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	now := time.Date(2026, 1, 4, 12, 0, 0, 0, loc)
	a.now = func() time.Time { return now }
	good := a.iiko
	a.iiko = client.New("http://127.0.0.1:1", "u", "p", &http.Client{Timeout: time.Second}) // iiko недоступен
	stub.outbox = []outboxEntry{stopMsg(1, seed.Dishes()[0].ID)}

	if err := a.processOutbox(context.Background()); err == nil {
		t.Fatal("ждали ошибку iiko")
	}
	if got := stub.acks[1]; len(got) != 1 || got[0].OK || got[0].Error == "" {
		t.Fatalf("ack с ошибкой: %+v", got)
	}
	if want := now.Add(time.Second); !a.retryAt[1].Equal(want) {
		t.Fatalf("пауза до %v, ждали %v", a.retryAt[1], want)
	}
	// Пауза ещё идёт: ни iiko, ни ack.
	now = now.Add(500 * time.Millisecond)
	if err := a.processOutbox(context.Background()); err != nil || len(stub.acks[1]) != 1 {
		t.Fatalf("во время паузы: %v %+v", err, stub.acks)
	}
	// Вторая неудача удваивает паузу (attempts=1 уже в очереди).
	now = now.Add(time.Second)
	if err := a.processOutbox(context.Background()); err == nil {
		t.Fatal("ждали ошибку iiko")
	}
	if want := now.Add(2 * time.Second); !a.retryAt[1].Equal(want) {
		t.Fatalf("пауза до %v, ждали %v", a.retryAt[1], want)
	}
	// iiko вернулся.
	a.iiko = good
	now = now.Add(2 * time.Second)
	if err := a.processOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if items, _ := ic.StopList(context.Background()); len(items) != 1 || len(stub.outbox) != 0 {
		t.Fatalf("не доставлено: %v, в очереди %v", items, stub.outbox)
	}
}

// Теневой режим: iiko не трогаем (адрес недоступен), сообщение подтверждено, событие в журнале.
func TestOutboxShadowWritesNothingToIiko(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, _ := setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	a.mode = modeShadow
	a.iiko = client.New("http://127.0.0.1:1", "u", "p", &http.Client{Timeout: time.Second})
	d := seed.Dishes()
	stub.outbox = []outboxEntry{stopMsg(1, d[0].ID), stopMsg(2, d[0].ID, d[1].ID)}
	if err := a.processOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(stub.outbox) != 0 || !stub.acks[1][0].OK || !stub.acks[2][0].OK {
		t.Fatalf("ack: %+v", stub.acks)
	}
	// Повторная запись той же строки (ack потерялся) дубля не даёт.
	if err := a.recordShadow(stopMsg(2, d[0].ID, d[1].ID), []string{d[0].ID, d[1].ID}, 1); err != nil {
		t.Fatal(err)
	}
	ev, err := loadShadow(a.shadowPath)
	if err != nil || len(ev) != 1 || ev[0].OutboxID != 2 || len(ev[0].WouldSend) != 2 || ev[0].Superseded != 1 || len(ev[0].Added) != 2 {
		t.Fatalf("журнал: %+v %v", ev, err)
	}
}

func TestShadowReport(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	now := time.Date(2026, 1, 4, 12, 0, 0, 0, loc)
	a, stub, ic := setup(t, now)
	a.mode = modeShadow
	d := seed.Dishes()
	stub.outbox = []outboxEntry{stopMsg(1, d[0].ID)}
	if err := a.processOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	stub.outbox = []outboxEntry{stopMsg(2, d[1].ID)}
	if err := a.processOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	bal, _ := ic.Balance(context.Background(), now)
	products, _ := ic.Products(context.Background())
	factor := map[string]int64{}
	for _, p := range products {
		u, _ := mapping.UnitOf(p.MainUnit)
		factor[p.ID] = u.Factor
	}
	rows := []map[string]any{}
	for _, b := range bal {
		rows = append(rows, map[string]any{"id": b.Product, "name": b.Product, "available": map[string]any{"qty": int64(b.Amount*float64(factor[b.Product]) + 0.5)}})
	}
	ats := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(rows) }))
	defer ats.Close()
	a.api.base = ats.URL

	var buf bytes.Buffer
	if err := a.runShadowReport(context.Background(), &buf, true, false, ""); err != nil {
		t.Fatal(err)
	}
	var rep ShadowReport
	if err := json.Unmarshal(buf.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Date != "2026-01-04" || rep.Mode != modeShadow || len(rep.Events) != 2 || len(rep.Final) != 1 || rep.Final[0] != d[1].ID ||
		len(rep.Events[1].Added) != 1 || len(rep.Events[1].Removed) != 1 || len(rep.Reconcile) != 0 {
		t.Fatalf("отчёт: %s", buf.String())
	}
	buf.Reset()
	if err := a.runShadowReport(context.Background(), &buf, false, false, "2026-01-04"); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, d[1].ID) || !strings.Contains(out, "OUTBOX") || !strings.Contains(out, "расхождений нет") {
		t.Fatalf("таблица:\n%s", out)
	}
	buf.Reset()
	if err := a.runShadowReport(context.Background(), &buf, false, false, "2026-01-03"); err != nil || !strings.Contains(buf.String(), "изменений за день не было") {
		t.Fatalf("пустой день: %v\n%s", err, buf.String())
	}
}
