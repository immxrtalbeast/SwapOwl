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

	stop    []string        // GET /api/stop-list
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
	case r.Method == http.MethodGet && r.URL.Path == "/api/stop-list":
		rows := []map[string]any{}
		for _, id := range s.stop {
			rows = append(rows, map[string]any{"id": id, "available": false})
		}
		_ = json.NewEncoder(w).Encode(rows)
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

// uniqueIDs — сколько разных id в запросах POST path.
func (s *stubAPI) uniqueIDs(path string) int {
	ids := map[string]bool{}
	for _, c := range s.calls {
		if c.Method == http.MethodPost && c.Path == path {
			var v struct{ ID string }
			_ = json.Unmarshal(c.Body, &v)
			ids[v.ID] = true
		}
	}
	return len(ids)
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
	if nd == 0 || no < 4 { // чеки закрытых дней 1–3 января и сегодняшнего, по заказу на чек
		t.Fatalf("поставок %d, заказов %d", nd, no)
	}
	if stub.uniqueIDs("/api/orders") != no {
		t.Fatalf("заказов %d, разных id %d: чеки склеены или продублированы", no, stub.uniqueIDs("/api/orders"))
	}
	for _, c := range stub.calls {
		if c.Auth != "Bearer secret" {
			t.Fatalf("нет ключа: %+v", c)
		}
		if c.Path == "/api/orders" {
			var o mapping.Order
			_ = json.Unmarshal(c.Body, &o)
			if o.Source != "pos" || len(o.ID) != 36 || len(o.Items) == 0 || o.OccurredAt == "" {
				t.Fatalf("заказ: %s", c.Body)
			}
		}
	}
	items, err := ic.StopList(context.Background())
	if err != nil || len(items) != 1 || items[0].ProductID != seed.Dishes()[0].ID {
		t.Fatalf("стоп-лист в iiko: %v %v", items, err)
	}

	// Повторный проход: закрытые дни не читаются (курсор), сегодняшний читается заново с теми же id.
	ids := stub.uniqueIDs("/api/orders")
	stub.outbox = []outboxEntry{stopMsg(2)}
	if err := a.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := stub.uniqueIDs("/api/orders"); got != ids {
		t.Fatalf("повторная синхронизация дала новые заказы: было %d id, стало %d", ids, got)
	}
	if got := stub.count(http.MethodPost, "/api/orders"); got >= 2*no {
		t.Fatalf("закрытые дни читаются заново: %d заказов", got)
	}
	if items, _ := ic.StopList(context.Background()); len(items) != 0 {
		t.Fatalf("стоп-лист не очищен: %v", items)
	}
	// Накладные и продажи текущего дня читаются заново; повторы — идемпотентность API.
	if st, _ := loadState(a.statePath); st.Sales != "2026-01-04" {
		t.Fatalf("курсор продаж: %+v", st)
	}
}

func TestOrderIDStable(t *testing.T) {
	if mapping.CheckOrderID("c1") != mapping.CheckOrderID("c1") || mapping.CheckOrderID("c1") == mapping.CheckOrderID("c2") {
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

func TestRejectedOrderDoesNotStopCursor(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, _ := setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	if err := a.runOnce(context.Background()); err != nil { // сколько заказов в норме
		t.Fatal(err)
	}
	all := stub.uniqueIDs("/api/orders")
	var bad string
	for _, c := range stub.calls {
		if c.Path == "/api/orders" {
			var o mapping.Order
			_ = json.Unmarshal(c.Body, &o)
			bad = o.ID
			break
		}
	}
	a, stub, _ = setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	s := httptest.NewServer(rejectOrder{stub, bad})
	defer s.Close()
	a.api.base = s.URL
	if err := a.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ := loadState(a.statePath)
	if n := stub.uniqueIDs("/api/orders"); n != all-1 || st.Sales != "2026-01-04" {
		t.Fatalf("заказов принято %d из %d, курсор продаж %q", n, all, st.Sales)
	}
}

// lateOrders отвечает 400 по occurred_at на заказы со временем до cutoff (как API за окном опоздания).
type lateOrders struct {
	*stubAPI
	cutoff time.Time
}

func (l lateOrders) ServeHTTP(w http.ResponseWriter, q *http.Request) {
	if q.URL.Path == "/api/orders" {
		b, _ := io.ReadAll(q.Body)
		var o mapping.Order
		_ = json.Unmarshal(b, &o)
		if at, err := time.Parse(time.RFC3339, o.OccurredAt); err == nil && at.Before(l.cutoff) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"validation_error","message":"occurred_at must be within the window"}}`))
			return
		}
		q.Body = io.NopCloser(bytes.NewReader(b))
	}
	l.stubAPI.ServeHTTP(w, q)
}

// Продажи старше окна API отклоняет: адаптер их пишет в счётчик один раз, не повторяет и не застревает.
func TestLateSalesCountedNotStuck(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, _ := setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	s := httptest.NewServer(lateOrders{stub, time.Date(2026, 1, 3, 0, 0, 0, 0, loc)})
	defer s.Close()
	a.api.base = s.URL
	if err := a.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.lateSeen == 0 {
		t.Fatal("отклонённые окном продажи не посчитаны")
	}
	first := a.lateSeen
	st, _ := loadState(a.statePath)
	if st.Sales != "2026-01-04" {
		t.Fatalf("курсор застрял: %q", st.Sales)
	}
	if err := a.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.lateSeen != first {
		t.Fatalf("счётчик растёт на повторе: %d → %d", first, a.lateSeen)
	}
}

// Накладная iiko от 08:00 уходит с occurred_at 08:00, а не со временем приёма.
func TestDeliveryCarriesInvoiceTime(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, _ := setup(t, time.Date(2026, 1, 2, 9, 30, 0, 0, loc))
	if err := a.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range stub.calls {
		if c.Path != "/api/deliveries" {
			continue
		}
		var d mapping.Delivery
		_ = json.Unmarshal(c.Body, &d)
		if d.OccurredAt == "" || d.OccurredAt != d.CreatedAt || !strings.Contains(d.OccurredAt, "T08:00:00+03:00") {
			t.Fatalf("occurred_at поставки: %s", c.Body)
		}
		n++
	}
	if n == 0 {
		t.Fatal("поставок нет")
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

// Переход из тени в бой: очередь пуста (в тени всё подтверждено), текущий стоп-лист всё равно уходит.
func TestShadowToLiveSendsCurrentStopList(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, ic := setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	a.mode = modeShadow
	d := seed.Dishes()
	stub.stop = []string{d[0].ID}
	stub.outbox = []outboxEntry{stopMsg(1, d[0].ID)}
	if err := a.processOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if items, _ := ic.StopList(context.Background()); len(items) != 0 {
		t.Fatalf("в тени в iiko записано: %v", items)
	}
	a.mode = modeLive // ADAPTER_MODE=live и перезапуск
	if err := a.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if items, err := ic.StopList(context.Background()); err != nil || len(items) != 1 || items[0].ProductID != d[0].ID {
		t.Fatalf("стоп-лист в iiko после перехода в live: %v %v", items, err)
	}
	// Повторно за запуск /api/stop-list не читается.
	before := stub.count(http.MethodGet, "/api/stop-list")
	if err := a.processOutbox(context.Background()); err != nil || stub.count(http.MethodGet, "/api/stop-list") != before {
		t.Fatalf("повторная выгрузка: %v", err)
	}
}

// Ошибка iiko при выгрузке текущего стоп-листа не гасит флаг: следующий проход повторит.
func TestBootstrapRetriesAfterFailure(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, ic := setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	good := a.iiko
	a.iiko = client.New("http://127.0.0.1:1", "u", "p", &http.Client{Timeout: time.Second})
	stub.stop = []string{seed.Dishes()[0].ID}
	if err := a.processOutbox(context.Background()); err == nil || a.bootstrapped {
		t.Fatalf("ждали ошибку без флага: %v", err)
	}
	a.iiko = good
	if err := a.processOutbox(context.Background()); err != nil || !a.bootstrapped {
		t.Fatal(err)
	}
	if items, _ := ic.StopList(context.Background()); len(items) != 1 {
		t.Fatalf("стоп-лист: %v", items)
	}
}

// lateDeliveries отвечает 400 по occurred_at на любую поставку, как API за окном опоздания.
type lateDeliveries struct{ *stubAPI }

func (l lateDeliveries) ServeHTTP(w http.ResponseWriter, q *http.Request) {
	if q.URL.Path == "/api/deliveries" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"validation_error","message":"occurred_at must be within the window"}}`))
		return
	}
	l.stubAPI.ServeHTTP(w, q)
}

func TestLateDeliveriesCounted(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, _ := setup(t, time.Date(2026, 1, 2, 9, 30, 0, 0, loc))
	s := httptest.NewServer(lateDeliveries{stub})
	defer s.Close()
	a.api.base = s.URL
	if err := a.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.lateSeen == 0 {
		t.Fatal("накладные за окном не посчитаны")
	}
}

func supplyMsg(id int64, orderID string) outboxEntry {
	p, _ := json.Marshal(map[string]any{
		"id": orderID, "created_at": "2026-01-04T09:00:00Z",
		"items": []map[string]any{{"product_id": seed.Products()[0].ID, "qty": 4500, "unit": "g"}, {"product_id": seed.Products()[1].ID, "qty": 3, "unit": "pcs"}},
	})
	return outboxEntry{ID: id, Kind: kindSupplyOrder, Payload: p, CreatedAt: time.Date(2026, 1, 4, 9, 0, 0, 0, time.UTC)}
}

// Боевой режим: заказ уходит документом в iiko (граммы → кг), ack ok; повторная доставка того же
// заказа (ack потерян) не создаёт второй документ.
func TestSupplyOrderLiveDeliversWithoutDuplicates(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, ic := setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	a.supplierID, a.storeID = fake.SupplierID, fake.StoreID
	const oid = "5b0c0c3e-8a51-4d0e-9d0b-1f2a3b4c5d6e"
	stub.outbox = []outboxEntry{supplyMsg(7, oid)}
	if err := a.processOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	docs, err := ic.PurchaseOrders(context.Background(), "")
	if err != nil || len(docs) != 1 || docs[0].ID != oid || len(docs[0].Items) != 2 || docs[0].Items[0].Amount != 4.5 || docs[0].Items[1].Amount != 3 {
		t.Fatalf("в iiko: %v %+v", err, docs)
	}
	if got := stub.acks[7]; len(got) != 1 || !got[0].OK || len(stub.outbox) != 0 {
		t.Fatalf("ack: %+v", got)
	}
	stub.outbox = []outboxEntry{supplyMsg(8, oid)} // тот же заказ ещё раз
	if err := a.processOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if docs, _ = ic.PurchaseOrders(context.Background(), ""); len(docs) != 1 {
		t.Fatalf("дубль в iiko: %d документов", len(docs))
	}
}

// Теневой режим: в iiko ничего не уходит (адрес недоступен), в журнале «отправил бы», сообщение подтверждено.
func TestSupplyOrderShadowOnlyLogs(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	a, stub, _ := setup(t, time.Date(2026, 1, 4, 12, 0, 0, 0, loc))
	var buf bytes.Buffer
	a.log = log.New(&buf, "", 0)
	a.mode = modeShadow
	a.iiko = client.NewWithOptions("http://127.0.0.1:1", "u", "p", &http.Client{Timeout: time.Second}, client.Options{Retries: -1})
	stub.outbox = []outboxEntry{supplyMsg(3, "o-shadow")}
	if err := a.processOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "отправил бы") || !strings.Contains(buf.String(), "o-shadow") {
		t.Fatalf("журнал: %q", buf.String())
	}
	if got := stub.acks[3]; len(got) != 1 || !got[0].OK {
		t.Fatalf("ack: %+v", got)
	}
}

// Ошибка iiko: ack с ошибкой, пауза с удвоением; когда iiko вернулся — доставлено; стоп-лист не страдает.
func TestSupplyOrderRetriesAndMissingConfig(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	now := time.Date(2026, 1, 4, 12, 0, 0, 0, loc)
	a, stub, ic := setup(t, now)
	a.now = func() time.Time { return now }
	a.supplierID, a.storeID = fake.SupplierID, fake.StoreID
	good := a.iiko
	a.iiko = client.NewWithOptions("http://127.0.0.1:1", "u", "p", &http.Client{Timeout: time.Second}, client.Options{Retries: -1})
	a.bootstrapped = true // стоп-лист здесь не при чём
	stub.outbox = []outboxEntry{supplyMsg(5, "o-retry")}
	if err := a.processOutbox(context.Background()); err == nil {
		t.Fatal("ждали ошибку iiko")
	}
	if got := stub.acks[5]; len(got) != 1 || got[0].OK || got[0].Error == "" {
		t.Fatalf("ack с ошибкой: %+v", got)
	}
	if err := a.processOutbox(context.Background()); err != nil || len(stub.acks[5]) != 1 {
		t.Fatalf("во время паузы: %v %+v", err, stub.acks[5])
	}
	a.iiko = good
	now = now.Add(time.Second)
	if err := a.processOutbox(context.Background()); err != nil || len(stub.outbox) != 0 {
		t.Fatalf("после возврата iiko: %v %+v", err, stub.outbox)
	}
	if docs, _ := ic.PurchaseOrders(context.Background(), "o-retry"); len(docs) != 1 {
		t.Fatalf("документ не доставлен: %+v", docs)
	}
	// Без поставщика и склада — ошибка доставки с понятным текстом, а не молчаливая потеря.
	a.supplierID = ""
	stub.outbox = []outboxEntry{supplyMsg(6, "o-noconf")}
	if err := a.processOutbox(context.Background()); err == nil || !strings.Contains(stub.acks[6][0].Error, "ADAPTER_SUPPLIER_ID") {
		t.Fatalf("без настроек: %v %+v", err, stub.acks[6])
	}
}
