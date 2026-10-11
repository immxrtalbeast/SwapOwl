package pilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"umirhack/internal/iiko/client"
	"umirhack/internal/iiko/fake"
	"umirhack/internal/iiko/mapping"
	"umirhack/internal/iiko/reconcile"
)

// Прогон перед пилотом (.ai/plan.md, 3б.11; .ai/solution.md, 8.0а): 30 симулированных дней ресторана
// через настоящий адаптер. Цепочка: фейковый iiko → cmd/iiko-adapter → cmd/api на PostgreSQL, и обратно —
// стоп-лист через outbox. Часы стенда — ускоренные и стоят на паузе: время двигает только прогон.
//
// Суточный цикл (время ресторана, Europe/Moscow):
//
//	00:30  проход адаптера: продажи закрытого вчерашнего дня; сверка с iiko за вчера
//	09:30  проход адаптера: накладные дня (пришли в 08:00, опоздание 1,5 ч), стоп-лист
//	14:00  заказы через API, отмена заказа, брак
//	22:30  инвентаризация одного продукта с небольшим расхождением
//	23:30  проход адаптера (стоп-лист), проверки дня
const (
	days     = 30
	iikoSeed = 20261012
)

var moscow = func() *time.Location {
	l, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		panic(err)
	}
	return l
}()

type product struct {
	ID, Unit string
}

// pilot — состояние прогона.
type pilot struct {
	t       *testing.T
	s       *stand
	iiko    *client.Client
	adapter string
	env     []string
	nowFile string
	rng     *rand.Rand
	origin  time.Time // начало суток дня 0

	units  map[string]string // product id → базовая единица
	dishes []string

	// delta[p] — ожидаемое расхождение «наш остаток − остаток iiko», заданное сценарием:
	// заказы через API и их отмены, брак, инвентаризация. Продажи кассы, поставки и сроки годности
	// iiko видит так же, как мы, поэтому сюда не входят (срок и недостача кассы — из журнала БД).
	delta map[string]int64
	// gain[p] — накопленная сумма положительных отклонений (отмены, излишки): граница того, сколько
	// отклонения могла «съесть» остановка склада iiko на нуле.
	gain map[string]int64

	apiOrders []string // принятые заказы через API, ещё не отменённые
	sum       summary
	stopMsgs  int64
}

// summary — итоговые цифры за 30 дней (docs/pilot.md).
type summary struct {
	Days               int
	AdapterPasses      int
	DeliveriesIn       int64
	DeliveredUnits     int64
	PosOrders          int64
	PosPortions        int64
	PosShortfalls      int64
	PosShortfallQty    int64
	APIOrdersAccepted  int
	APIOrdersRejected  int
	Cancellations      int
	ReturnedQty        int64
	Writeoffs          int
	WriteoffQty        int64
	Inventories        int
	InventoryShortage  int64
	InventorySurplus   int64
	ExpiredWriteoffs   int64
	ExpiredQty         int64
	Movements          int64
	StopMessages       int64
	StopMessagesFailed int64
	MaxStopList        int
	LateDeliveries     int
	ReconcileRows      int
	ReconcileMaxAbs    int64
	ReconcileExact     int
	ReconcileBounded   int
	HTTPRequests       int
	Server5xx          int
}

func TestPilotThirtyDays(t *testing.T) {
	if testing.Short() {
		t.Skip("прогон на 30 дней — длинный")
	}
	adminURL(t)
	work := t.TempDir()
	adapterBin := buildAdapter(t, work)

	// Фейковый iiko: история начинается с начала суток запуска; наш стенд стартует в 11:00 того же дня.
	y, m, d := time.Now().In(moscow).Date()
	origin := time.Date(y, m, d, 0, 0, 0, 0, moscow)
	fk, err := fake.New(fake.Options{Seed: iikoSeed, Origin: origin, Login: "pilot", Password: "pilot"})
	if err != nil {
		t.Fatal(err)
	}
	rawSrv := httptest.NewServer(fk.Handler())
	t.Cleanup(rawSrv.Close)
	nowFile := filepath.Join(work, "now")
	fakeSrv := httptest.NewServer(pastOnly(rawSrv.URL, func() time.Time {
		b, err := os.ReadFile(nowFile)
		if err != nil {
			return time.Time{}
		}
		at, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b)))
		return at
	}, moscow))
	t.Cleanup(fakeSrv.Close)
	ik := client.New(fakeSrv.URL, "pilot", "pilot", &http.Client{Timeout: 60 * time.Second})

	p := &pilot{t: t, iiko: ik, adapter: adapterBin, rng: rand.New(rand.NewSource(7)),
		nowFile: nowFile, delta: map[string]int64{}, gain: map[string]int64{}, units: map[string]string{}}

	p.s = newStand(t, startOpts{Phase1: p.loadCatalog, Env: []string{"LATE_EVENTS_HOURS=2"}})
	p.s.demo("/api/demo/clock/speed", map[string]any{"speed": 0})

	p.origin = origin
	start := p.s.now().In(moscow)
	if start.Day() != d {
		t.Skipf("запуск пересёк полночь (%s): прогон надо начинать в тот же день, что и история iiko", start)
	}
	p.env = []string{
		"IIKO_URL=" + fakeSrv.URL, "IIKO_LOGIN=pilot", "IIKO_PASSWORD=pilot",
		"API_URL=" + p.s.base, "API_KEY=" + p.s.key, "ADAPTER_TZ=Europe/Moscow",
		"ADAPTER_MODE=live", "ADAPTER_START_DATE=" + origin.Format("2006-01-02"),
		"ADAPTER_STATE_FILE=" + filepath.Join(work, "state.json"),
		"ADAPTER_SHADOW_FILE=" + filepath.Join(work, "shadow.json"),
		"PILOT_NOW_FILE=" + p.nowFile,
	}
	var prods []struct {
		ID   string `json:"id"`
		Unit string `json:"unit"`
	}
	p.s.get("/api/products", &prods)
	for _, pr := range prods {
		p.units[pr.ID] = pr.Unit
	}
	var ds []struct {
		ID string `json:"id"`
	}
	p.s.get("/api/dishes", &ds)
	for _, x := range ds {
		p.dishes = append(p.dishes, x.ID)
	}
	sort.Strings(p.dishes)

	at := func(day int, h, min int) time.Time {
		return time.Date(origin.Year(), origin.Month(), origin.Day()+day, h, min, 0, 0, moscow)
	}

	for day := 0; day < days; day++ {
		if day == 0 {
			// Стенд стартует в 11:00 дня 0: накладная 08:00 приходит на 3 ч позже обычного.
			p.pass(at(day, 11, 0))
		} else {
			// После полуночи день закрыт: адаптер передаёт продажи вчерашнего дня, пока не пришла
			// сегодняшняя поставка (как при штатном интервале 5 минут). Сверка — на этот момент.
			p.jumpTo(at(day, 0, 30))
			p.pass(at(day, 0, 30))
			p.reconcile(day - 1)
			p.jumpTo(at(day, 9, 30))
			p.pass(at(day, 9, 30)) // накладные 08:00 приходят с опозданием 1,5 ч
		}
		p.jumpTo(at(day, 14, 0))
		p.daytime(day)
		p.jumpTo(at(day, 22, 30))
		p.inventory(day)
		p.jumpTo(at(day, 23, 30))
		p.pass(at(day, 23, 30))
		p.endOfDay(day)
		p.sum.Days++
	}
	// Закрываем последний день: утренний проход следующих суток и сверка.
	p.jumpTo(at(days, 0, 30))
	p.pass(at(days, 0, 30))
	p.reconcile(days - 1)
	p.finish()
}

// loadCatalog загружает справочник так же, как адаптер: каталог iiko → PUT /api/catalog.
func (p *pilot) loadCatalog(s *stand) {
	ctx := context.Background()
	products, err := p.iiko.Products(ctx)
	if err != nil {
		p.t.Fatal(err)
	}
	charts, err := p.iiko.Charts(ctx)
	if err != nil {
		p.t.Fatal(err)
	}
	cat, _, _ := mapping.BuildCatalog(products, charts, nil,
		mapping.Defaults{PriceMinorPer1000: 10000, LeadDays: 2, PackSize: 1000, PriorDaily: 10})
	if st, b := s.do("PUT", "/api/catalog", cat); st != 200 {
		p.t.Fatalf("PUT /api/catalog: %d %s", st, b)
	}
}

func (p *pilot) jumpTo(at time.Time) { p.s.jumpTo(at) }

// writeNow кладёт время стенда туда, откуда его берёт адаптер.
func (p *pilot) writeNow() {
	if err := os.WriteFile(p.nowFile, []byte(p.s.now().Format(time.RFC3339Nano)), 0o600); err != nil {
		p.t.Fatal(err)
	}
}

// adapterCmd запускает адаптер и возвращает вывод; ненулевой код выхода — провал.
func (p *pilot) adapterCmd(args ...string) string {
	p.t.Helper()
	p.writeNow()
	cmd := exec.Command(p.adapter, args...)
	cmd.Env = append(os.Environ(), p.env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		p.t.Fatalf("iiko-adapter %v: %v\n%s\n%s", args, err, errb.String(), out.String())
	}
	return out.String()
}

// pass — один полный проход адаптера (справочники, накладные, продажи, outbox). Стоп-лист меняется
// не только заказами, но и ходом времени (срок годности), поэтому проход повторяется, пока в outbox
// появляются новые сообщения: «дворник» и публикация стоп-листа работают в реальном времени.
func (p *pilot) pass(at time.Time) {
	p.t.Helper()
	p.sum.AdapterPasses++
	p.adapterCmd("--once")
	deadline := time.Now().Add(20 * time.Second)
	for {
		if p.stopSynced() {
			return
		}
		if time.Now().After(deadline) {
			return // итог проверки — в endOfDay
		}
		time.Sleep(300 * time.Millisecond)
		p.sum.AdapterPasses++
		p.adapterCmd("--once")
	}
}

// apiStopList — id блюд из GET /api/stop-list.
func (p *pilot) apiStopList() []string {
	var list []struct {
		ID        string `json:"id"`
		Available bool   `json:"available"`
	}
	p.s.get("/api/stop-list", &list)
	ids := []string{}
	for _, d := range list {
		if d.Available {
			p.t.Fatalf("в стоп-листе блюдо %s с available=true", d.ID)
		}
		ids = append(ids, d.ID)
	}
	sort.Strings(ids)
	return ids
}

func (p *pilot) iikoStopList() []string {
	items, err := p.iiko.StopList(context.Background())
	if err != nil {
		p.t.Fatal(err)
	}
	ids := []string{}
	for _, it := range items {
		ids = append(ids, it.ProductID)
	}
	sort.Strings(ids)
	return ids
}

func (p *pilot) undelivered() int64 {
	return p.s.queryInt(`SELECT COUNT(*) FROM outbox WHERE delivered_at IS NULL`)
}

func (p *pilot) stopSynced() bool {
	return strings.Join(p.apiStopList(), ",") == strings.Join(p.iikoStopList(), ",") && p.undelivered() == 0
}

// qtyStr — количество в базовых единицах продукта строкой для API.
func (p *pilot) qtyStr(product string, q int64) (string, string) {
	return fmt.Sprint(q), p.units[product]
}

// qty — количество из ответов API: {"qty": 6000, "unit": "g"}; в базовых единицах оно целое.
type qty int64

func (q *qty) UnmarshalJSON(b []byte) error {
	var o struct {
		Qty json.Number `json:"qty"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	f, err := o.Qty.Float64()
	if err != nil {
		return err
	}
	*q = qty(math.Round(f))
	return nil
}

type writeoffLine struct {
	ProductID string `json:"product_id"`
	Qty       qty    `json:"qty"`
}

// daytime — дневные операции оператора: заказы через API, отмена, брак.
func (p *pilot) daytime(day int) {
	t, s := p.t, p.s
	// 1. Заказы через API (зал/доставка не через кассу): часть принимается, часть может быть отклонена.
	for i := 0; i < 4; i++ {
		id := newUUID(p.rng)
		dish := p.dishes[p.rng.Intn(len(p.dishes))]
		portions := 1 + p.rng.Intn(3)
		st, b := s.do("POST", "/api/orders", map[string]any{"id": id,
			"items": []map[string]any{{"dish_id": dish, "portions": portions}}})
		switch st {
		case 201:
			var r struct {
				Writeoffs []writeoffLine `json:"writeoffs"`
			}
			mustJSON(t, b, &r)
			p.addDelta(r.Writeoffs, -1)
			p.apiOrders = append(p.apiOrders, id)
			p.sum.APIOrdersAccepted++
		case 409:
			p.sum.APIOrdersRejected++ // штатный отказ: не хватает продуктов
		default:
			t.Fatalf("день %d: POST /api/orders: %d %s", day, st, b)
		}
	}
	// 2. Отмена принятого заказа через API (каждый день, если есть что отменять).
	if len(p.apiOrders) > 0 {
		id := p.apiOrders[0]
		p.apiOrders = p.apiOrders[1:]
		p.cancel(id, "c-api-"+id)
	}
	// 3. Раз в 10 дней отменяется и вчерашний заказ кассы целиком (iiko об этом не знает — заданное расхождение).
	if day%10 == 5 {
		prev := time.Date(s.now().Year(), s.now().Month(), s.now().Day()-1, 12, 0, 0, 0, moscow).Format("2006-01-02")
		p.cancel(mapping.OrderID(prev), "c-pos-"+prev)
	}
	// 4. Брак: списание с партии, которая закончится первой.
	p.writeoff(day)
	// 5. Поздняя накладная вручную (опоздание в окне принимается, за окном — отказ без записи).
	if day%5 == 2 {
		p.lateDelivery(day)
	}
}

// lateDelivery — накладная, принесённая с опозданием: 90 минут (в окне LATE_EVENTS_HOURS=2) принимается
// и считается от фактического времени прихода, 3 часа — 400 validation_error, ничего не записано.
func (p *pilot) lateDelivery(day int) {
	t, s := p.t, p.s
	now := s.now()
	send := func(id string, late time.Duration) (int, []byte) {
		return s.do("POST", "/api/deliveries", map[string]any{
			"id": id, "number": id, "created_at": now.Add(-late).Format(time.RFC3339),
			"occurred_at": now.Add(-late).Format(time.RFC3339),
			"items":       []map[string]any{{"product_id": "potato", "quantity": "500", "unit": "g", "shelf_life_hours": 48}},
		})
	}
	before := s.queryInt(`SELECT COUNT(*) FROM deliveries`)
	if st, b := send(fmt.Sprintf("late-out-%d", day), 3*time.Hour); st != 400 {
		t.Fatalf("день %d: поставка за окном опоздания: %d %s, ожидалось 400", day, st, b)
	}
	if s.queryInt(`SELECT COUNT(*) FROM deliveries`) != before {
		t.Fatalf("день %d: отклонённая поздняя поставка оставила запись", day)
	}
	id := fmt.Sprintf("late-in-%d", day)
	if st, b := send(id, 90*time.Minute); st != 201 {
		t.Fatalf("день %d: поставка с опозданием в окне: %d %s", day, st, b)
	}
	var recv time.Time
	if err := s.pool.QueryRow(context.Background(), `SELECT received_at FROM batches WHERE delivery_id = $1`, id).Scan(&recv); err != nil {
		t.Fatal(err)
	}
	if want := now.Add(-90 * time.Minute); recv.Sub(want).Abs() > time.Second {
		t.Fatalf("день %d: received_at = %s, ожидалось %s (occurred_at)", day, recv, want)
	}
	p.apply("potato", 500)
	p.sum.LateDeliveries++
}

func (p *pilot) cancel(orderID, opID string) {
	t := p.t
	st, b := p.s.do("POST", "/api/orders/"+orderID+"/cancel", map[string]any{"id": opID})
	if st != 201 {
		t.Fatalf("отмена %s: %d %s", orderID, st, b)
	}
	var r struct {
		Returned []writeoffLine `json:"returned"`
	}
	mustJSON(t, b, &r)
	p.addDelta(r.Returned, +1)
	for _, l := range r.Returned {
		p.sum.ReturnedQty += int64(l.Qty)
	}
	p.sum.Cancellations++
}

func (p *pilot) addDelta(lines []writeoffLine, sign int64) {
	// Количества в ответах — строки в основных единицах API; в базовых они целые.
	for _, l := range lines {
		p.apply(l.ProductID, sign*int64(l.Qty))
	}
}

// apply учитывает заданное сценарием отклонение остатка продукта.
func (p *pilot) apply(product string, amount int64) {
	p.delta[product] += amount
	if amount > 0 {
		p.gain[product] += amount
	}
}

func (p *pilot) writeoff(day int) {
	t, s := p.t, p.s
	var batches []struct {
		ID        int64  `json:"id"`
		ProductID string `json:"product_id"`
		Remaining qty    `json:"remaining"`
		Status    string `json:"status"`
	}
	s.get("/api/batches", &batches)
	var cand []int
	for i, b := range batches {
		if b.Status != "expired" && int64(b.Remaining) >= 20 {
			cand = append(cand, i)
		}
	}
	if len(cand) == 0 {
		return
	}
	b := batches[cand[p.rng.Intn(len(cand))]]
	q := int64(b.Remaining) / 20 // 5% партии
	reason := []string{"spoilage", "damage"}[day%2]
	qs, unit := p.qtyStr(b.ProductID, q)
	id := fmt.Sprintf("wo-%d", day)
	st, body := s.do("POST", "/api/writeoffs", map[string]any{"id": id, "batch_id": b.ID, "qty": qs, "unit": unit, "reason": reason})
	if st != 201 {
		t.Fatalf("день %d: брак: %d %s", day, st, body)
	}
	p.apply(b.ProductID, -q)
	p.sum.Writeoffs++
	p.sum.WriteoffQty += q
}

// inventory — ежедневная инвентаризация одного продукта (по кругу) с небольшим расхождением:
// чаще недостача 1–3 %, раз в четыре дня излишек.
func (p *pilot) inventory(day int) {
	t, s := p.t, p.s
	ids := make([]string, 0, len(p.units))
	for id := range p.units {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	pid := ids[day%len(ids)]
	var prods []struct {
		ID        string `json:"id"`
		Available struct {
			Qty int64 `json:"qty"`
		} `json:"available"`
	}
	s.get("/api/products", &prods)
	var have int64
	for _, x := range prods {
		if x.ID == pid {
			have = x.Available.Qty
		}
	}
	if have < 100 {
		return
	}
	var counted int64
	if day%4 == 3 {
		counted = have + have*int64(1+p.rng.Intn(2))/100
	} else {
		counted = have - have*int64(1+p.rng.Intn(3))/100
	}
	if counted == have {
		counted = have - 1
	}
	qs, unit := p.qtyStr(pid, counted)
	st, b := s.do("POST", "/api/inventory", map[string]any{"id": fmt.Sprintf("inv-%d", day), "product_id": pid, "counted": qs, "unit": unit})
	if st != 201 {
		t.Fatalf("день %d: инвентаризация %s: %d %s", day, pid, st, b)
	}
	p.apply(pid, counted-have)
	p.sum.Inventories++
	if counted < have {
		p.sum.InventoryShortage += have - counted
	} else {
		p.sum.InventorySurplus += counted - have
	}
}

// endOfDay — проверки в конце суток (23:30), после прохода адаптера.
func (p *pilot) endOfDay(day int) {
	t, s := p.t, p.s
	label := fmt.Sprintf("день %d", day)
	// 1. Ни одного 5xx — ни на запросах прогона, ни на запросах адаптера (по журналу сервиса).
	if len(s.status5) > 0 {
		t.Fatalf("%s: ответы 5xx: %v", label, s.status5)
	}
	if errs := s.serverErrors(); len(errs) > 0 {
		p.sum.Server5xx += len(errs)
		t.Fatalf("%s: в журнале сервиса 5xx/panic:\n%s", label, strings.Join(errs, "\n"))
	}
	// 2. Журнал движений сходится с остатками по каждой партии.
	p.checkConservation(label)
	// 3. Стоп-лист в iiko совпадает с /api/stop-list, в outbox нет недоставленного.
	api, ik := p.apiStopList(), p.iikoStopList()
	if strings.Join(api, ",") != strings.Join(ik, ",") {
		t.Fatalf("%s: стоп-лист iiko %v ≠ /api/stop-list %v", label, ik, api)
	}
	if n := p.undelivered(); n != 0 {
		t.Fatalf("%s: в outbox недоставлено %d сообщений", label, n)
	}
	if len(api) > p.sum.MaxStopList {
		p.sum.MaxStopList = len(api)
	}
}

// checkConservation — receipt + return − sale − writeoff − adjustment = remaining ≥ 0 по каждой партии.
func (p *pilot) checkConservation(label string) {
	rows := p.s.queryRows(`
		SELECT b.id, b.quantity, b.remaining,
		  COALESCE(SUM(CASE WHEN m.kind IN ('receipt','return') THEN m.quantity
		                    WHEN m.kind IN ('sale','writeoff','adjustment') THEN -m.quantity END), 0)
		FROM batches b LEFT JOIN movements m ON m.batch_id = b.id
		GROUP BY b.id ORDER BY b.id`)
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, q, rem, bal int64
		if err := rows.Scan(&id, &q, &rem, &bal); err != nil {
			p.t.Fatal(err)
		}
		n++
		if rem < 0 || rem > q || bal != rem {
			p.t.Fatalf("%s: партия %d: журнал даёт %d, remaining %d, поступило %d", label, id, bal, rem, q)
		}
	}
	if n == 0 {
		p.t.Fatalf("%s: в базе нет партий", label)
	}
}

// reconcile — сверка с iiko (подкоманда reconcile адаптера) утром, когда продажи вчерашнего дня уже у нас.
// Расхождение по продукту должно равняться заданному сценарием: заказы через API и отмены, брак,
// инвентаризация (всё накоплено в p.delta) плюс то, что iiko не знает по определению: срок годности
// (iiko не учитывает) и недостача продаж кассы (iiko списывает по рецепту всё).
func (p *pilot) reconcile(day int) {
	t := p.t
	label := fmt.Sprintf("сверка за день %d", day)
	p.checkSources(day)
	out := p.adapterCmd("reconcile", "-json", "-all")
	var rows []reconcile.Row
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("%s: разбор вывода: %v\n%s", label, err, out)
	}
	now := p.s.now()
	expired := p.expiredLoss(now)
	short := p.shortfalls()
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.ProductID] = true
		p.sum.ReconcileRows++
		// D — отклонение, заданное сценарием. Продажа кассы, на которую у одной из сторон не хватает
		// продукта, «съедает» часть отклонения: у нас это недостача (shortfalls), iiko списывает по
		// рецепту, но не ниже нуля. Поэтому без недостач и положительных отклонений сверка обязана
		// совпасть с D точно, а иначе — отличаться не больше чем на недостачи плюс положительные отклонения.
		D := p.delta[r.ProductID] - expired[r.ProductID]
		slack := short[r.ProductID] + p.gain[r.ProductID]
		if abs(r.Diff) > p.sum.ReconcileMaxAbs {
			p.sum.ReconcileMaxAbs = abs(r.Diff)
		}
		if slack == 0 {
			p.sum.ReconcileExact++
		} else {
			p.sum.ReconcileBounded++
		}
		// Допуск 2 базовые единицы: iiko округляет остаток до 3 знаков основной единицы.
		if abs(r.Diff-D) > slack+2 {
			t.Logf("%s: движения %s: %s", label, r.ProductID, p.movementSummary(r.ProductID))
			t.Errorf("%s: %s: расхождение ours−iiko = %d, по сценарию ожидалось %d±%d (брак, инвентаризация, заказы API и отмены %d; истёк срок −%d; недостачи кассы %d); ours=%d iiko=%d",
				label, r.ProductID, r.Diff, D, slack, p.delta[r.ProductID], expired[r.ProductID], short[r.ProductID], r.Ours, r.Iiko)
		}
	}
	if len(rows) == 0 {
		t.Fatalf("%s: пустая сверка", label)
	}
	if t.Failed() {
		t.FailNow()
	}
}

// checkSources — то, что iiko знает о дне, дошло до нас целиком: продажи (порции по блюдам) лежат
// в заказе кассы за этот день, а каждая накладная iiko — поставкой с тем же id.
func (p *pilot) checkSources(day int) {
	t := p.t
	ctx := context.Background()
	d := time.Date(p.origin.Year(), p.origin.Month(), p.origin.Day()+day, 0, 0, 0, 0, moscow)
	label := fmt.Sprintf("день %d", day)
	sales, err := p.iiko.Sales(ctx, d, d.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{}
	for _, sl := range sales {
		want[sl.DishID] += sl.Portions
	}
	got := map[string]int64{}
	rows := p.s.queryRows(`SELECT dish_id, portions FROM order_items WHERE order_id = $1`, mapping.OrderID(d.Format("2006-01-02")))
	for rows.Next() {
		var id string
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			t.Fatal(err)
		}
		got[id] = n
	}
	rows.Close()
	if fmt.Sprint(want) != fmt.Sprint(got) {
		t.Fatalf("%s: продажи iiko %v ≠ заказ кассы у нас %v", label, want, got)
	}
	invs, err := p.iiko.Invoices(ctx, d, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(invs) == 0 {
		t.Fatalf("%s: в iiko нет накладных за день", label)
	}
	for _, inv := range invs {
		if p.s.queryInt(`SELECT COUNT(*) FROM deliveries WHERE id = $1`, inv.ID) != 1 {
			t.Fatalf("%s: накладная iiko %s не принята как поставка", label, inv.ID)
		}
	}
}

// movementSummary — суммы журнала движений по продукту в разрезе вида и причины (для сообщений об ошибках).
func (p *pilot) movementSummary(product string) string {
	rows := p.s.queryRows(`SELECT m.kind, COALESCE(m.reason, ''), SUM(m.quantity) FROM movements m
		JOIN batches b ON b.id = m.batch_id WHERE b.product_id = $1 GROUP BY 1, 2 ORDER BY 1, 2`, product)
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var k, r string
		var q int64
		if err := rows.Scan(&k, &r, &q); err != nil {
			p.t.Fatal(err)
		}
		parts = append(parts, fmt.Sprintf("%s/%s=%d", k, r, q))
	}
	return strings.Join(parts, " ")
}

// expiredLoss — сколько единиц продукта пропало по сроку годности: списано «дворником» и ещё лежит в
// просроченных партиях. Наш остаток считает только непросроченное, iiko сроков не знает.
func (p *pilot) expiredLoss(now time.Time) map[string]int64 {
	m := map[string]int64{}
	rows := p.s.queryRows(`
		SELECT product_id, SUM(q) FROM (
		  SELECT b.product_id, m.quantity AS q FROM movements m JOIN batches b ON b.id = m.batch_id
		   WHERE m.kind = 'writeoff' AND m.reason = 'expired'
		  UNION ALL
		  SELECT product_id, remaining FROM batches WHERE expires_at <= $1 AND remaining > 0
		) x GROUP BY product_id`, now)
	defer rows.Close()
	for rows.Next() {
		var id string
		var q int64
		if err := rows.Scan(&id, &q); err != nil {
			p.t.Fatal(err)
		}
		m[id] = q
	}
	return m
}

// shortfalls — недостача продаж кассы: касса продала, а у нас не хватило; iiko списал по рецепту.
func (p *pilot) shortfalls() map[string]int64 {
	m := map[string]int64{}
	rows := p.s.queryRows(`SELECT product_id, SUM(quantity) FROM shortfalls GROUP BY product_id`)
	defer rows.Close()
	for rows.Next() {
		var id string
		var q int64
		if err := rows.Scan(&id, &q); err != nil {
			p.t.Fatal(err)
		}
		m[id] = q
	}
	return m
}

// finish считает итоговые цифры и выводит их JSON-ом (из них собран docs/pilot.md).
func (p *pilot) finish() {
	s, t := p.s, p.t
	q := s.queryInt
	p.sum.DeliveriesIn = q(`SELECT COUNT(*) FROM deliveries`)
	p.sum.DeliveredUnits = q(`SELECT COALESCE(SUM(quantity),0) FROM movements WHERE kind='receipt' AND reason IS DISTINCT FROM 'inventory_surplus'`)
	p.sum.PosOrders = q(`SELECT COUNT(*) FROM orders WHERE source='pos' AND status='accepted'`)
	p.sum.PosPortions = q(`SELECT COALESCE(SUM(oi.portions),0) FROM order_items oi JOIN orders o ON o.id=oi.order_id WHERE o.source='pos' AND o.status='accepted'`)
	p.sum.PosShortfalls = q(`SELECT COUNT(*) FROM shortfalls`)
	p.sum.PosShortfallQty = q(`SELECT COALESCE(SUM(quantity),0) FROM shortfalls`)
	p.sum.ExpiredWriteoffs = q(`SELECT COUNT(*) FROM movements WHERE kind='writeoff' AND reason='expired'`)
	p.sum.ExpiredQty = q(`SELECT COALESCE(SUM(quantity),0) FROM movements WHERE kind='writeoff' AND reason='expired'`)
	p.sum.Movements = q(`SELECT COUNT(*) FROM movements`)
	p.sum.StopMessages = q(`SELECT COUNT(*) FROM outbox`)
	p.sum.StopMessagesFailed = q(`SELECT COUNT(*) FROM outbox WHERE attempts > 1 OR last_error IS NOT NULL`)
	p.sum.HTTPRequests = s.reqs
	if len(s.status5) > 0 {
		t.Fatalf("ответы 5xx: %v", s.status5)
	}
	b, _ := json.MarshalIndent(p.sum, "", "  ")
	t.Logf("итоги прогона:\n%s", b)
	if p.sum.Days != days || p.sum.PosOrders < days-1 {
		t.Errorf("прогон неполный: дней %d, заказов кассы %d", p.sum.Days, p.sum.PosOrders)
	}
}

func mustJSON(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("разбор ответа: %v\n%s", err, b)
	}
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func newUUID(r *rand.Rand) string {
	var b [16]byte
	for i := range b {
		b[i] = byte(r.Intn(256))
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
