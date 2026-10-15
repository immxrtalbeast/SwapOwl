package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"umirhack/internal/auth"
	"umirhack/internal/clock"
	"umirhack/internal/domain"
	"umirhack/internal/events"
	"umirhack/internal/janitor"
	"umirhack/internal/stand"
	"umirhack/internal/store"
)

// Аудит валидации (.ai/solution.md, 7.4, 5): любой некорректный запрос к изменяющему маршруту
// даёт 4xx в едином формате {"error":{"code","message","details"}}, а не 500, и ничего не записывает.
// Тест идёт по настоящему роутеру и настоящей БД (TEST_DATABASE_URL); набор маршрутов берётся
// из самого роутера, поэтому новый изменяющий маршрут без строки в auditBodies валит тест.

const vDemoToken = "audit-demo-token"

type validationEnv struct {
	h      http.Handler
	rt     *Router
	db     *store.DB
	cookie string
	// Идентификаторы из сида.
	gProduct, mlProduct, pcsProduct string
	dish                            string
}

func newValidationEnv(t *testing.T) *validationEnv {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := store.Open(ctx, base, 1)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("audit_%d_%d", os.Getpid(), time.Now().UnixNano())
	if _, err := admin.Pool.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	db, err := store.Open(ctx, u.String(), 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Pool.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		admin.Close()
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	t0 := stand.T0(time.Date(2026, 10, 10, 9, 30, 0, 0, time.UTC), loc)
	if loaded, err := stand.Load(ctx, db, t0); err != nil || !loaded {
		t.Fatalf("seed: loaded=%v err=%v", loaded, err)
	}
	clk, err := clock.NewAccelerated(ctx, db.ClockStore(), t0, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	gate := &stand.Gate{}
	jan := janitor.New(stand.GatedJanitorStore{Inner: db, Gate: gate}, clk.Now, nil)
	st := &stand.Stand{DB: db, Clock: clk, Gate: gate, Sweep: jan, Bus: events.New(clk.Now), Loc: loc}

	svc := auth.New(db, auth.Options{Required: true, SessionTTL: 24 * 365 * time.Hour, Cost: 4, Now: clk.Now})
	if err := svc.Bootstrap(ctx, "root", "root-password"); err != nil {
		t.Fatal(err)
	}
	rt := NewRouter(Deps{
		DB: db, Now: clk.Now, Auth: svc, Audit: db, Stock: db, Receiving: db, Catalog: db, Orders: db,
		Corrections: db, Menu: db, Outbox: db, Analytics: db, Location: loc, Events: events.New(clk.Now),
		Demo: st, DemoToken: vDemoToken, NotSimulated: stand.ErrNotSimulated, Gate: gate,
		HotOffers: store.Snapshotter{DB: db, Params: store.SnapshotParams{WindowDays: 14, PriorDays: 28,
			Schedule: domain.Schedule{Location: time.UTC, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour}}},
	})
	env := &validationEnv{h: rt.Handler(), rt: rt, db: db}

	rec := env.do("POST", "/api/auth/login", `{"login":"root","password":"root-password"}`, "application/json", nil)
	if rec.Code != 200 {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" {
			env.cookie = c.Value
		}
	}
	if env.cookie == "" {
		t.Fatal("no session cookie")
	}

	// Продукты и блюдо из сида: единицы нужны, чтобы подбирать «не ту размерность».
	rows, err := db.Pool.Query(ctx, "SELECT id, unit FROM products ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		var unit domain.Unit
		if err := rows.Scan(&id, &unit); err != nil {
			t.Fatal(err)
		}
		switch {
		case unit == "g" && env.gProduct == "":
			env.gProduct = id
		case unit == "ml" && env.mlProduct == "":
			env.mlProduct = id
		case unit == "pcs" && env.pcsProduct == "":
			env.pcsProduct = id
		}
	}
	rows.Close()
	if err := db.Pool.QueryRow(ctx, "SELECT id FROM dishes ORDER BY id LIMIT 1").Scan(&env.dish); err != nil {
		t.Fatal(err)
	}
	if env.gProduct == "" || env.mlProduct == "" || env.pcsProduct == "" {
		t.Fatalf("seed has no g/ml/pcs product: %q %q %q", env.gProduct, env.mlProduct, env.pcsProduct)
	}
	return env
}

func (e *validationEnv) do(method, path, body, contentType string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if e.cookie != "" {
		r.AddCookie(&http.Cookie{Name: "session", Value: e.cookie})
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	return rec
}

// rowCounts — число строк во всех таблицах, которые меняют бизнес-запросы.
func (e *validationEnv) rowCounts(t *testing.T) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, tbl := range []string{"products", "dishes", "recipe_items", "deliveries", "batches", "orders",
		"order_items", "movements", "shortfalls", "corrections", "outbox", "users", "api_keys", "clock_jumps"} {
		var n int
		if err := e.db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM "+tbl).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		out[tbl] = n
	}
	return out
}

// requireErrorFormat проверяет статус 4xx и тело {"error":{"code","message","details"}}.
// field != "" — message и details.field называют поле.
func requireErrorFormat(t *testing.T, rec *httptest.ResponseRecorder, field string) {
	t.Helper()
	if rec.Code < 400 || rec.Code > 499 {
		t.Errorf("status %d, want 4xx; body: %s", rec.Code, rec.Body)
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || len(raw) != 1 || raw["error"] == nil {
		t.Errorf("body is not {\"error\":{...}}: %s", rec.Body)
		return
	}
	var e struct {
		Code    *string         `json:"code"`
		Message *string         `json:"message"`
		Details *map[string]any `json:"details"`
	}
	if err := json.Unmarshal(raw["error"], &e); err != nil || e.Code == nil || e.Message == nil || e.Details == nil {
		t.Errorf("error needs code, message and details (object): %s", rec.Body)
		return
	}
	if *e.Code == "" || *e.Message == "" {
		t.Errorf("empty code or message: %s", rec.Body)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Errorf("Content-Type %q, want JSON", rec.Header().Get("Content-Type"))
	}
	if field == "" {
		return
	}
	// message называет поле и причину: поле — последний сегмент пути (items[0].quantity → quantity).
	name := field[strings.LastIndex(field, ".")+1:]
	if !strings.Contains(*e.Message, name) {
		t.Errorf("message %q does not name field %q", *e.Message, field)
	}
	if got, _ := (*e.Details)["field"].(string); got != field {
		t.Errorf("details.field = %v, want %q; body: %s", (*e.Details)["field"], field, rec.Body)
	}
}

type vCase struct {
	name  string
	body  string
	field string // ожидаемое details.field; "" — только формат и статус
	code  string // ожидаемый error.code; "" — любой
}

func vjs(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// with возвращает копию m с заменой полей (nil-значение убирает поле).
func vwith(m map[string]any, kv ...any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	for i := 0; i < len(kv); i += 2 {
		if kv[i+1] == nil {
			delete(out, kv[i].(string))
		} else {
			out[kv[i].(string)] = kv[i+1]
		}
	}
	return out
}

const vUUID = "5b0c0c3e-8a51-4d0e-9d0b-1f2a3b4c5d6e"

// vRoute описывает изменяющий маршрут: путь с подставленными параметрами, годное тело
// (для проверки Content-Type) и специфичные случаи. Годное тело не должно ничего ломать повторно.
type vRoute struct {
	path    string
	noBody  bool // маршрут не читает тело: пустое тело для него не ошибка
	valid   string
	cases   []vCase
	headers map[string]string
}

func (e *validationEnv) routes() map[string]vRoute {
	long := strings.Repeat("я", 129)
	delivery := func(item map[string]any) map[string]any {
		return map[string]any{"id": "D-AUDIT", "number": "1", "created_at": "2026-10-10T08:00:00+03:00",
			"items": []any{item}}
	}
	item := func(product, qty string, kv ...any) map[string]any {
		return vwith(map[string]any{"product_id": product, "quantity": qty, "shelf_life_hours": 24}, kv...)
	}

	// Поставка.
	var dCases []vCase
	for _, q := range []string{"-1", "+1", "0", "0.0", "1e3", "0x10", "1_0", "1,5", ".5", "1.", "", " 1", "NaN", "Inf", "1/3",
		"9223372036854775808", "1000000000001", strings.Repeat("1", 5000)} {
		dCases = append(dCases, vCase{"quantity " + q, vjs(delivery(item(e.gProduct, q))), "items[0].quantity", ""})
	}
	dCases = append(dCases,
		vCase{"0.0005 kg", vjs(delivery(item(e.gProduct, "0.0005", "unit", "kg"))), "items[0].quantity", ""},
		vCase{"fractional pcs", vjs(delivery(item(e.pcsProduct, "1.5"))), "items[0].quantity", ""},
		vCase{"kg for ml product", vjs(delivery(item(e.mlProduct, "1", "unit", "kg"))), "items[0].quantity", ""},
		vCase{"pcs for g product", vjs(delivery(item(e.gProduct, "1", "unit", "pcs"))), "items[0].quantity", ""},
		vCase{"1e3 kg", vjs(delivery(item(e.gProduct, "1e3", "unit", "kg"))), "items[0].quantity", ""},
		vCase{"1000000001 kg", vjs(delivery(item(e.gProduct, "1000000001", "unit", "kg"))), "items[0].quantity", ""},
		vCase{"quantity JSON number", vjs(delivery(item(e.gProduct, "x", "quantity", 1.5))), "", ""},
		vCase{"quantity JSON negative number", vjs(delivery(item(e.gProduct, "x", "quantity", -1))), "", ""},
		vCase{"quantity missing", vjs(delivery(item(e.gProduct, "1", "quantity", nil))), "items[0].quantity", ""},
		vCase{"unknown unit", vjs(delivery(item(e.gProduct, "1", "unit", "oz"))), "items[0].unit", ""},
		vCase{"unknown product without unit", vjs(delivery(item("no-such-product", "1"))), "items[0].unit", CodeUnknownProduct},
		vCase{"unknown product, bad unit", vjs(delivery(item("no-such-product", "1", "unit", "oz"))), "items[0].unit", ""},
		vCase{"empty items", vjs(vwith(delivery(nil), "items", []any{})), "items", ""},
		vCase{"items null", vjs(vwith(delivery(nil), "items", nil)), "items", ""},
		vCase{"items not array", vjs(vwith(delivery(nil), "items", "x")), "", ""},
		vCase{"item not object", vjs(vwith(delivery(nil), "items", []any{1})), "", ""},
		vCase{"id missing", vjs(vwith(delivery(item(e.gProduct, "1")), "id", nil)), "id", ""},
		vCase{"id too long", vjs(vwith(delivery(item(e.gProduct, "1")), "id", long)), "id", ""},
		vCase{"id control char", vjs(vwith(delivery(item(e.gProduct, "1")), "id", "a\u0000b")), "id", ""},
		vCase{"id JSON number", vjs(vwith(delivery(item(e.gProduct, "1")), "id", 5)), "", ""},
		vCase{"number too long", vjs(vwith(delivery(item(e.gProduct, "1")), "number", long)), "number", ""},
		vCase{"product_id too long", vjs(delivery(item(long, "1"))), "items[0].product_id", ""},
		vCase{"name too long", vjs(delivery(item(e.gProduct, "1", "name", strings.Repeat("x", 100000)))), "", ""},
		vCase{"created_at garbage", vjs(vwith(delivery(item(e.gProduct, "1")), "created_at", "yesterday")), "created_at", ""},
		vCase{"created_at missing", vjs(vwith(delivery(item(e.gProduct, "1")), "created_at", nil)), "created_at", ""},
		vCase{"shelf_life 0", vjs(delivery(item(e.gProduct, "1", "shelf_life_hours", 0))), "items[0].shelf_life_hours", ""},
		vCase{"shelf_life negative", vjs(delivery(item(e.gProduct, "1", "shelf_life_hours", -5))), "items[0].shelf_life_hours", ""},
		vCase{"shelf_life huge", vjs(delivery(item(e.gProduct, "1", "shelf_life_hours", 99999999))), "items[0].shelf_life_hours", ""},
		vCase{"shelf_life fractional", vjs(delivery(item(e.gProduct, "1", "shelf_life_hours", 1.5))), "", ""},
		vCase{"shelf_life overflow", `{"id":"D","number":"1","created_at":"2026-10-10T08:00:00+03:00","items":[{"product_id":"p","quantity":"1","shelf_life_hours":99999999999999999999}]}`, "", ""},
		vCase{"expires_on garbage", vjs(delivery(item(e.gProduct, "1", "shelf_life_hours", nil, "expires_on", "12.10.2026"))), "items[0].expires_on", ""},
		vCase{"both shelf_life and expires_on", vjs(delivery(item(e.gProduct, "1", "expires_on", "2026-10-12"))), "items[0].shelf_life_hours", ""},
		vCase{"occurred_at garbage", vjs(vwith(delivery(item(e.gProduct, "1")), "occurred_at", "later")), "occurred_at", ""},
		vCase{"unknown field", vjs(vwith(delivery(item(e.gProduct, "1")), "bogus", 1)), "", ""},
		vCase{"unknown item field", vjs(delivery(item(e.gProduct, "1", "bogus", 1))), "", ""},
	)

	// Заказ.
	order := func(items ...any) map[string]any { return map[string]any{"id": vUUID, "items": items} }
	line := func(dish string, portions any) map[string]any {
		return map[string]any{"dish_id": dish, "portions": portions}
	}
	oCases := []vCase{
		{"empty items", vjs(order()), "items", ""},
		{"items null", vjs(vwith(order(), "items", nil)), "items", ""},
		{"no items key", vjs(map[string]any{"id": vUUID}), "items", ""},
		{"unknown dish", vjs(order(line("no-such-dish", 1))), "items[0].dish_id", CodeUnknownDish},
		{"unknown dish second line", vjs(order(line(e.dish, 1), line("no-such-dish", 1))), "items[1].dish_id", CodeUnknownDish},
		{"portions negative", vjs(order(line(e.dish, -1))), "items[0].portions", ""},
		{"portions zero", vjs(order(line(e.dish, 0))), "items[0].portions", ""},
		{"portions missing", vjs(order(map[string]any{"dish_id": e.dish})), "items[0].portions", ""},
		{"portions null", vjs(order(line(e.dish, nil))), "items[0].portions", ""},
		{"portions over limit", vjs(order(line(e.dish, 10001))), "items[0].portions", ""},
		{"portions huge", vjs(order(line(e.dish, 9223372036854775807))), "items[0].portions", ""},
		{"portions overflow", `{"id":"` + vUUID + `","items":[{"dish_id":"x","portions":99999999999999999999}]}`, "", ""},
		{"portions fractional", vjs(order(line(e.dish, 1.5))), "", ""},
		{"portions 1e3", `{"id":"` + vUUID + `","items":[{"dish_id":"x","portions":1e3}]}`, "", ""},
		{"portions string", vjs(order(line(e.dish, "1"))), "", ""},
		{"portions string 1,5", vjs(order(line(e.dish, "1,5"))), "", ""},
		{"duplicate dish", vjs(order(line(e.dish, 1), line(e.dish, 1))), "items[1].dish_id", ""},
		{"dish_id empty", vjs(order(line("", 1))), "items[0].dish_id", ""},
		{"dish_id too long", vjs(order(line(strings.Repeat("d", 129), 1))), "items[0].dish_id", ""},
		{"dish_id number", vjs(order(map[string]any{"dish_id": 7, "portions": 1})), "", ""},
		{"too many lines", vjs(order(func() []any {
			out := make([]any, 101)
			for i := range out {
				out[i] = line(fmt.Sprintf("d%d", i), 1)
			}
			return out
		}()...)), "items", ""},
		{"id not uuid", vjs(vwith(order(line(e.dish, 1)), "id", "42")), "id", ""},
		{"id missing", vjs(vwith(order(line(e.dish, 1)), "id", nil)), "id", ""},
		{"id too long", vjs(vwith(order(line(e.dish, 1)), "id", strings.Repeat("a", 100000))), "id", ""},
		{"source unknown", vjs(vwith(order(line(e.dish, 1)), "source", "web")), "source", ""},
		{"occurred_at garbage", vjs(vwith(order(line(e.dish, 1)), "occurred_at", "later")), "occurred_at", ""},
		{"unknown field", vjs(vwith(order(line(e.dish, 1)), "bogus", true)), "", ""},
		{"unknown line field", vjs(order(map[string]any{"dish_id": e.dish, "portions": 1, "bogus": 1})), "", ""},
	}

	// Списание брака.
	wo := map[string]any{"id": "W-AUDIT", "batch_id": 1, "qty": "1", "reason": "spoilage"}
	woCases := []vCase{
		{"id missing", vjs(vwith(wo, "id", nil)), "id", ""},
		{"id too long", vjs(vwith(wo, "id", long)), "id", ""},
		{"batch_id missing", vjs(vwith(wo, "batch_id", nil)), "batch_id", ""},
		{"batch_id negative", vjs(vwith(wo, "batch_id", -1)), "batch_id", ""},
		{"batch_id zero", vjs(vwith(wo, "batch_id", 0)), "batch_id", ""},
		{"batch_id string", vjs(vwith(wo, "batch_id", "1")), "", ""},
		{"batch_id fractional", vjs(vwith(wo, "batch_id", 1.5)), "", ""},
		{"unknown batch", vjs(vwith(wo, "batch_id", 999999999)), "", CodeUnknownBatch},
		{"reason missing", vjs(vwith(wo, "reason", nil)), "reason", ""},
		{"reason unknown", vjs(vwith(wo, "reason", "theft")), "reason", ""},
		{"unknown field", vjs(vwith(wo, "bogus", 1)), "", ""},
	}
	for _, q := range []string{"-1", "+1", "0", "0.0", "1e3", "1,5", ".5", "", " 1", "NaN", "1000000000001", strings.Repeat("9", 5000)} {
		woCases = append(woCases, vCase{"qty " + q, vjs(vwith(wo, "qty", q)), "qty", ""})
	}
	woCases = append(woCases,
		vCase{"qty JSON number", vjs(vwith(wo, "qty", 1.5)), "", ""},
		vCase{"qty missing", vjs(vwith(wo, "qty", nil)), "qty", ""},
		vCase{"qty with unknown unit", vjs(vwith(wo, "unit", "oz")), "qty", ""},
	)

	// Пересчёт.
	inv := map[string]any{"id": "I-AUDIT", "product_id": e.gProduct, "counted": "1"}
	invCases := []vCase{
		{"id missing", vjs(vwith(inv, "id", nil)), "id", ""},
		{"product_id missing", vjs(vwith(inv, "product_id", nil)), "product_id", ""},
		{"product_id too long", vjs(vwith(inv, "product_id", long)), "product_id", ""},
		{"unknown product", vjs(vwith(inv, "product_id", "no-such-product")), "product_id", CodeUnknownProduct},
		{"counted missing", vjs(vwith(inv, "counted", nil)), "counted", ""},
		{"counted JSON number", vjs(vwith(inv, "counted", 1.5)), "", ""},
		{"unknown field", vjs(vwith(inv, "bogus", 1)), "", ""},
	}
	for _, q := range []string{"-1", "+1", "1e3", "1,5", ".5", "", " 1", "NaN", "1000000000001"} {
		invCases = append(invCases, vCase{"counted " + q, vjs(vwith(inv, "counted", q)), "counted", ""})
	}
	invCases = append(invCases,
		vCase{"counted 0.0005 kg", vjs(vwith(inv, "counted", "0.0005", "unit", "kg")), "counted", ""},
		vCase{"counted ml for g product", vjs(vwith(inv, "counted", "1", "unit", "ml")), "counted", ""},
		vCase{"counted fractional pcs", vjs(vwith(inv, "product_id", e.pcsProduct, "counted", "1.5")), "counted", ""},
	)

	// Отмена заказа.
	cancel := map[string]any{"id": "C-AUDIT"}
	cancelCases := []vCase{
		{"id missing", vjs(vwith(cancel, "id", nil)), "id", ""},
		{"id too long", vjs(vwith(cancel, "id", long)), "id", ""},
		{"unknown field", vjs(vwith(cancel, "bogus", 1)), "", ""},
	}

	// Срок партии.
	expiry := map[string]any{"shelf_life_hours": 24}
	expiryCases := []vCase{
		{"empty object", `{}`, "expires_on", ""},
		{"both", vjs(map[string]any{"shelf_life_hours": 24, "expires_on": "2026-12-01"}), "expires_on", ""},
		{"shelf_life 0", `{"shelf_life_hours":0}`, "shelf_life_hours", ""},
		{"shelf_life negative", `{"shelf_life_hours":-3}`, "shelf_life_hours", ""},
		{"shelf_life huge", `{"shelf_life_hours":99999999}`, "shelf_life_hours", ""},
		{"shelf_life fractional", `{"shelf_life_hours":1.5}`, "", ""},
		{"shelf_life string", `{"shelf_life_hours":"24"}`, "", ""},
		{"expires_on garbage", `{"expires_on":"12.10.2026"}`, "expires_on", ""},
		{"expires_on empty", `{"expires_on":""}`, "expires_on", ""},
		{"unknown field", `{"shelf_life_hours":24,"bogus":1}`, "", ""},
		{"unknown batch", `{"shelf_life_hours":24}`, "", CodeUnknownBatch}, // путь 999999999, см. ниже
	}
	_ = expiry

	// Импорт справочника.
	cat := map[string]any{"products": []any{map[string]any{"id": "audit-p", "name": "Тест", "unit": "g",
		"price_minor_per_1000": 100, "lead_days": 1, "pack_size": 1000}}}
	prod := func(kv ...any) string {
		p := vwith(cat["products"].([]any)[0].(map[string]any), kv...)
		return vjs(map[string]any{"products": []any{p}})
	}
	catCases := []vCase{
		{"empty object", `{}`, "products", ""},
		{"empty lists", `{"products":[],"dishes":[]}`, "products", ""},
		{"unit unknown", prod("unit", "kg"), "products[0].unit", ""},
		{"price negative", prod("price_minor_per_1000", -1), "products[0].price_minor_per_1000", ""},
		{"price missing", prod("price_minor_per_1000", nil), "products[0].price_minor_per_1000", ""},
		{"price fractional", prod("price_minor_per_1000", 1.5), "", ""},
		{"lead_days negative", prod("lead_days", -1), "products[0].lead_days", ""},
		{"pack_size zero", prod("pack_size", 0), "products[0].pack_size", ""},
		{"pack_size negative", prod("pack_size", -5), "products[0].pack_size", ""},
		{"shelf_life zero", prod("shelf_life_hours", 0), "products[0].shelf_life_hours", ""},
		{"id empty", prod("id", ""), "products[0].id", ""},
		{"id too long", prod("id", long), "products[0].id", ""},
		{"name too long", prod("name", long), "products[0].name", ""},
		{"unknown field", prod("bogus", 1), "", ""},
		{"dish unknown product", vjs(map[string]any{"dishes": []any{map[string]any{"id": "audit-d", "name": "Тест",
			"recipe": []any{map[string]any{"product_id": "no-such-product", "quantity": "1"}}}}}), "", ""},
		{"dish recipe qty negative", vjs(map[string]any{"products": cat["products"], "dishes": []any{map[string]any{"id": "audit-d", "name": "Тест",
			"recipe": []any{map[string]any{"product_id": "audit-p", "quantity": "-1"}}}}}), "", ""},
		{"dish recipe qty 1e3", vjs(map[string]any{"products": cat["products"], "dishes": []any{map[string]any{"id": "audit-d", "name": "Тест",
			"recipe": []any{map[string]any{"product_id": "audit-p", "quantity": "1e3"}}}}}), "", ""},
		{"dish recipe qty 0", vjs(map[string]any{"products": cat["products"], "dishes": []any{map[string]any{"id": "audit-d", "name": "Тест",
			"recipe": []any{map[string]any{"product_id": "audit-p", "quantity": "0"}}}}}), "", ""},
		{"dish recipe qty 1,5", vjs(map[string]any{"products": cat["products"], "dishes": []any{map[string]any{"id": "audit-d", "name": "Тест",
			"recipe": []any{map[string]any{"product_id": "audit-p", "quantity": "1,5"}}}}}), "", ""},
		{"dish recipe qty JSON number", vjs(map[string]any{"products": cat["products"], "dishes": []any{map[string]any{"id": "audit-d", "name": "Тест",
			"recipe": []any{map[string]any{"product_id": "audit-p", "quantity": 1.5}}}}}), "", ""},
		{"dish empty recipe", vjs(map[string]any{"dishes": []any{map[string]any{"id": "audit-d", "name": "Тест", "recipe": []any{}}}}), "", ""},
		{"dish price negative", vjs(map[string]any{"products": cat["products"], "dishes": []any{map[string]any{"id": "audit-d", "name": "Тест", "price_minor": -1,
			"recipe": []any{map[string]any{"product_id": "audit-p", "quantity": "1"}}}}}), "", ""},
	}

	user := map[string]any{"login": "audit-user", "password": "audit-password", "role": "viewer"}
	userCases := []vCase{
		{"login missing", vjs(vwith(user, "login", nil)), "login", ""},
		{"login too long", vjs(vwith(user, "login", long)), "login", ""},
		{"password short", vjs(vwith(user, "password", "short")), "password", ""},
		{"password too long", vjs(vwith(user, "password", strings.Repeat("p", 73))), "password", ""},
		{"role unknown", vjs(vwith(user, "role", "root")), "role", ""},
		{"role missing", vjs(vwith(user, "role", nil)), "role", ""},
		{"unknown field", vjs(vwith(user, "bogus", 1)), "", ""},
	}
	// Вскрытие упаковки (партия 1 — из сида окружения).
	open := map[string]any{"id": "OPEN-AUDIT", "qty": "1"}
	openCases := []vCase{
		{"id missing", vjs(vwith(open, "id", nil)), "id", ""},
		{"id too long", vjs(vwith(open, "id", long)), "id", ""},
		{"unit without qty", vjs(map[string]any{"id": "OPEN-AUDIT", "unit": "g"}), "unit", ""},
		{"qty number", vjs(vwith(open, "qty", 5)), "", ""},
		{"unknown field", vjs(vwith(open, "bogus", 1)), "", ""},
	}
	for _, q := range []string{"-1", "+1", "0", "0.0", "1e3", "1,5", ".5", "", " 1", "NaN", "1000000000001", strings.Repeat("9", 5000)} {
		openCases = append(openCases, vCase{"qty " + q, vjs(vwith(open, "qty", q)), "qty", ""})
	}

	key := map[string]any{"name": "audit-key", "role": "operator"}
	keyCases := []vCase{
		{"name missing", vjs(vwith(key, "name", nil)), "name", ""},
		{"name too long", vjs(vwith(key, "name", long)), "name", ""},
		{"role admin", vjs(vwith(key, "role", "admin")), "role", ""},
		{"role missing", vjs(vwith(key, "role", nil)), "role", ""},
		{"unknown field", vjs(vwith(key, "bogus", 1)), "", ""},
	}

	return map[string]vRoute{
		"POST /api/deliveries":           {path: "/api/deliveries", valid: vjs(delivery(item(e.gProduct, "1"))), cases: dCases},
		"POST /api/orders":               {path: "/api/orders", valid: vjs(order(line(e.dish, 1))), cases: oCases},
		"POST /api/writeoffs":            {path: "/api/writeoffs", valid: vjs(wo), cases: woCases},
		"POST /api/inventory":            {path: "/api/inventory", valid: vjs(inv), cases: invCases},
		"POST /api/orders/{id}/cancel":   {path: "/api/orders/" + vUUID + "/cancel", valid: vjs(cancel), cases: cancelCases},
		"PATCH /api/batches/{id}/expiry": {path: "/api/batches/999999999/expiry", valid: vjs(expiry), cases: expiryCases},
		"POST /api/batches/{id}/open":    {path: "/api/batches/1/open", valid: vjs(open), cases: openCases},
		"PUT /api/catalog":               {path: "/api/catalog", valid: vjs(cat), cases: catCases},
		"POST /api/users":                {path: "/api/users", valid: vjs(user), cases: userCases},
		"POST /api/api-keys":             {path: "/api/api-keys", valid: vjs(key), cases: keyCases},
		"DELETE /api/users/{id}":         {path: "/api/users/not-a-number", noBody: true},
		"DELETE /api/api-keys/{id}":      {path: "/api/api-keys/not-a-number", noBody: true},
		"POST /api/outbox/{id}/ack": {path: "/api/outbox/999999999/ack", valid: `{"ok":true}`, cases: []vCase{
			{"id not a number", `{}`, "", ""},
		}},
		"POST /api/auth/login": {path: "/api/auth/login", valid: `{"login":"root","password":"wrong"}`, cases: []vCase{
			{"login missing", `{"password":"x"}`, "login", ""},
			{"password missing", `{"login":"x"}`, "login", ""},
			{"login too long", vjs(map[string]any{"login": strings.Repeat("l", 1000), "password": "x"}), "login", ""},
			{"login JSON number", `{"login":1,"password":"x"}`, "", ""},
			{"unknown field", `{"login":"x","password":"x","bogus":1}`, "", ""},
		}},
		"POST /api/auth/logout": {path: "/api/auth/logout", noBody: true},
		"POST /api/auth/password": {path: "/api/auth/password", valid: `{"old":"wrong-password","new":"another-password"}`, cases: []vCase{
			{"old missing", `{"new":"another-password"}`, "old", ""},
			{"new short", `{"old":"x","new":"short"}`, "new", ""},
			{"new too long", vjs(map[string]any{"old": "x", "new": strings.Repeat("n", 73)}), "new", ""},
			{"new equals old", `{"old":"same-password","new":"same-password"}`, "new", ""},
			{"unknown field", `{"old":"x","new":"another-password","bogus":1}`, "", ""},
		}},
		"POST /api/demo/clock/speed": {path: "/api/demo/clock/speed", valid: `{"speed":0}`, headers: bearer(), cases: []vCase{
			{"negative", `{"speed":-1}`, "", ""},
			{"over limit", `{"speed":3601}`, "", ""},
			{"missing", `{}`, "", ""},
			{"string", `{"speed":"60"}`, "", ""},
			{"1e999", `{"speed":1e999}`, "", ""},
			{"unknown field", `{"speed":1,"bogus":1}`, "", ""},
		}},
		"POST /api/demo/clock/jump": {path: "/api/demo/clock/jump", valid: `{"hours":1}`, headers: bearer(), cases: []vCase{
			{"negative", `{"hours":-1}`, "", ""},
			{"zero", `{"hours":0}`, "", ""},
			{"over limit", `{"hours":721}`, "", ""},
			{"missing", `{}`, "", ""},
			{"string", `{"hours":"1"}`, "", ""},
			{"unknown field", `{"hours":1,"bogus":1}`, "", ""},
		}},
		"POST /api/what-if": {path: "/api/what-if", valid: `{"horizon_days":3,"scenarios":[{"type":"demand","dish_id":"` + e.dish + `","factor":1}]}`, cases: []vCase{
			{"no horizon", `{"scenarios":[]}`, "horizon_days", ""},
			{"horizon 8", `{"horizon_days":8}`, "horizon_days", ""},
			{"bad type", `{"horizon_days":1,"scenarios":[{"type":"x"}]}`, "scenarios[0].type", ""},
			{"unknown field", `{"horizon_days":1,"bogus":1}`, "", ""},
		}},
		"POST /api/demo/reset": {path: "/api/demo/reset", noBody: true, headers: bearer()},
	}
}

func bearer() map[string]string { return map[string]string{"Authorization": "Bearer " + vDemoToken} }

// Маршруты, которые подключаются только при Sim (генератор): в этом стенде их нет.
// Их тело — те же decodeBody/validationError, что у остальных; отдельный стенд не нужен.
var vSkipped = map[string]string{
	"POST /api/demo/simulator": "нужен Sim; тело {\"running\": bool} разбирает decodeBody",
	"POST /api/demo/burst":     "нужен Sim; тело разбирает decodeBody",
}

// mutatingRoutes — «МЕТОД шаблон» всех POST/PUT/PATCH/DELETE из самого роутера.
func mutatingRoutes(t *testing.T, env *validationEnv) []string {
	t.Helper()
	var out []string
	for pattern, byMethod := range env.rt.routes {
		for m := range byMethod {
			if m == "POST" || m == "PUT" || m == "PATCH" || m == "DELETE" {
				out = append(out, m+" "+pattern)
			}
		}
	}
	sort.Strings(out)
	return out
}

func TestValidationAudit(t *testing.T) {
	env := newValidationEnv(t)
	routes := env.routes()

	// Каждый изменяющий маршрут роутера описан в аудите (или явно пропущен с причиной).
	pairs := mutatingRoutes(t, env)
	if len(pairs) == 0 {
		t.Fatal("роутер не отдал ни одного изменяющего маршрута")
	}
	for _, p := range pairs {
		if _, ok := routes[p]; !ok {
			if _, skip := vSkipped[p]; !skip {
				t.Errorf("изменяющий маршрут %s не описан в аудите валидации", p)
			}
		}
	}
	for p := range routes {
		found := false
		for _, q := range pairs {
			found = found || p == q
		}
		if !found {
			t.Errorf("аудит описывает несуществующий маршрут %s", p)
		}
	}

	for _, p := range pairs {
		rt, ok := routes[p]
		if !ok {
			continue
		}
		method := strings.SplitN(p, " ", 2)[0]
		t.Run(p, func(t *testing.T) {
			before := env.rowCounts(t)
			check := func(name, body, ctype, field string, hdr map[string]string) {
				t.Run(name, func(t *testing.T) {
					rec := env.do(method, rt.path, body, ctype, hdr)
					requireErrorFormat(t, rec, field)
				})
			}
			hdr := rt.headers
			if !rt.noBody {
				// Общие случаи: годны для любого маршрута с телом.
				check("empty body", "", "application/json", "", hdr)
				check("whitespace body", "  \n", "application/json", "", hdr)
				check("not JSON", "this is not json", "application/json", "", hdr)
				check("truncated JSON", `{"id":`, "application/json", "", hdr)
				check("JSON array", `[]`, "application/json", "", hdr)
				check("JSON null", `null`, "application/json", "", hdr)
				check("JSON string", `"x"`, "application/json", "", hdr)
				check("JSON number", `42`, "application/json", "", hdr)
				check("empty object", `{}`, "application/json", "", hdr)
				check("unknown field only", `{"bogus":1}`, "application/json", "", hdr)
				check("trailing garbage", rt.valid+` {"x":1}`, "application/json", "", hdr)
				check("two documents", rt.valid+rt.valid, "application/json", "", hdr)
				check("body over limit", `{"id":"`+strings.Repeat("a", 2<<20)+`"}`, "application/json", "", hdr)
				check("invalid UTF-8", "{\"id\":\"\xff\xfe\"}", "application/json", "", hdr)
				// Годное тело с Content-Type не JSON.
				check("Content-Type text/plain", rt.valid, "text/plain", "", hdr)
				check("Content-Type form", rt.valid, "application/x-www-form-urlencoded", "", hdr)
				for _, c := range rt.cases {
					check(c.name, c.body, "application/json", c.field, hdr)
				}
			}
			// Параметры пути: чужой id не должен давать 500.
			if got := env.rowCounts(t); fmt.Sprint(got) != fmt.Sprint(before) {
				t.Errorf("некорректные запросы что-то записали:\nбыло  %v\nстало %v", before, got)
			}
		})
	}
}

// Неверные параметры пути в едином формате.
func TestValidationAuditPathParams(t *testing.T) {
	env := newValidationEnv(t)
	for _, c := range []struct{ method, path, body, field string }{
		{"POST", "/api/orders/not-a-uuid/cancel", `{"id":"C1"}`, "id"},
		{"POST", "/api/batches/abc/open", `{"id":"o"}`, "id"},
		{"POST", "/api/batches/0/open", `{"id":"o"}`, "id"},
		{"POST", "/api/batches/-1/open", `{"id":"o"}`, "id"},
		{"PATCH", "/api/batches/abc/expiry", `{"shelf_life_hours":24}`, "id"},
		{"PATCH", "/api/batches/0/expiry", `{"shelf_life_hours":24}`, "id"},
		{"PATCH", "/api/batches/-1/expiry", `{"shelf_life_hours":24}`, "id"},
		{"PATCH", "/api/batches/1.5/expiry", `{"shelf_life_hours":24}`, "id"},
		{"PATCH", "/api/batches/99999999999999999999/expiry", `{"shelf_life_hours":24}`, "id"},
		{"POST", "/api/outbox/abc/ack", `{}`, ""},
		{"POST", "/api/outbox/-1/ack", `{}`, ""},
		{"DELETE", "/api/users/abc", ``, ""},
		{"DELETE", "/api/users/0", ``, ""},
		{"DELETE", "/api/api-keys/abc", ``, ""},
		{"DELETE", "/api/api-keys/99999999999999999999", ``, ""},
	} {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			rec := env.do(c.method, c.path, c.body, "application/json", nil)
			requireErrorFormat(t, rec, c.field)
		})
	}
}
