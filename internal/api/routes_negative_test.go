package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/events"
	"umirhack/internal/store"
)

// ---- Переопределения горячих предложений (FS-159) ----

// memOverrides хранит решения как настоящее хранилище: известные блюда, замена прежнего решения, удаление без ошибки.
type memOverrides struct {
	known map[string]bool
	byDis map[string]store.HotOverride
	sets  int
}

func (m *memOverrides) ListHotOverrides(context.Context) ([]store.HotOverride, error) {
	out := []store.HotOverride{}
	for _, o := range m.byDis {
		out = append(out, o)
	}
	return out, nil
}
func (m *memOverrides) SetHotOverride(_ context.Context, o store.HotOverride) (store.HotOverride, error) {
	if !m.known[o.DishID] {
		return store.HotOverride{}, store.ErrHotOverrideDish
	}
	m.sets++
	m.byDis[o.DishID] = o
	return o, nil
}
func (m *memOverrides) DeleteHotOverride(_ context.Context, dish string) error {
	delete(m.byDis, dish)
	return nil
}

func newMemOverrides() *memOverrides {
	return &memOverrides{known: map[string]bool{"soup": true, "steak": true}, byDis: map[string]store.HotOverride{}}
}

func TestPutHotOverrideNegative(t *testing.T) {
	mem := newMemOverrides()
	rt := overridesRouter(&fakeOverrides{}, nil)
	rt.deps.HotOverrides = mem
	cyr200 := strings.Repeat("я", 200) // 400 байт, но 200 символов
	cases := []struct {
		name, dish, body string
		want             int
		code, field      string
	}{
		{"заметка 201 символ", "soup", `{"mode":"pin","note":"` + strings.Repeat("a", 201) + `"}`, 400, CodeValidation, "note"},
		{"заметка 201 кириллический символ", "soup", `{"mode":"pin","note":"` + strings.Repeat("я", 201) + `"}`, 400, CodeValidation, "note"},
		{"заметка 200 символов — граница", "soup", `{"mode":"pin","note":"` + strings.Repeat("a", 200) + `"}`, 200, "", ""},
		{"заметка 200 кириллических — граница", "soup", `{"mode":"hide","note":"` + cyr200 + `"}`, 200, "", ""},
		{"пробелы по краям не считаются", "soup", `{"mode":"pin","note":"  ` + strings.Repeat("a", 200) + `  "}`, 200, "", ""},
		{"режима нет", "soup", `{}`, 400, CodeValidation, "mode"},
		{"режим в другом регистре", "soup", `{"mode":"PIN"}`, 400, CodeValidation, "mode"},
		{"режим числом", "soup", `{"mode":1}`, 400, CodeValidation, ""},
		{"неизвестное блюдо", "unicorn", `{"mode":"hide"}`, 404, CodeUnknownDish, ""},
		{"не JSON", "soup", `mode=pin`, 400, CodeValidation, ""},
		{"пустое тело", "soup", ``, 400, CodeValidation, ""},
	}
	for _, c := range cases {
		rec := putOverride(rt, c.dish, c.body)
		if rec.Code != c.want {
			t.Errorf("%s: %d, ожидали %d (%s)", c.name, rec.Code, c.want, rec.Body)
			continue
		}
		if c.code != "" {
			e := decodeErr(t, rec)
			if e.Error.Code != c.code || (c.field != "" && e.Error.Details["field"] != c.field) {
				t.Errorf("%s: %s", c.name, rec.Body)
			}
		}
	}
}

// Повтор того же решения идемпотентен: в списке одна запись; новое решение заменяет прежнее; DELETE повторяем.
func TestHotOverrideRepeatIsIdempotent(t *testing.T) {
	mem := newMemOverrides()
	rt := overridesRouter(&fakeOverrides{}, nil)
	rt.deps.HotOverrides = mem
	for i := 0; i < 3; i++ {
		if rec := putOverride(rt, "soup", `{"mode":"pin","note":"к обеду"}`); rec.Code != 200 {
			t.Fatalf("повтор %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	if len(mem.byDis) != 1 || mem.byDis["soup"].Mode != store.HotPin {
		t.Fatalf("после трёх одинаковых PUT: %+v", mem.byDis)
	}
	if rec := putOverride(rt, "soup", `{"mode":"hide"}`); rec.Code != 200 || len(mem.byDis) != 1 || mem.byDis["soup"].Mode != store.HotHide {
		t.Fatalf("смена решения: %d %+v", rec.Code, mem.byDis)
	}
	del := func(dish string) int {
		req := httptest.NewRequest(http.MethodDelete, "/api/hot-offers/overrides/"+dish, nil)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		rt.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < 2; i++ {
		if c := del("soup"); c != 204 {
			t.Fatalf("DELETE #%d: %d", i, c)
		}
	}
	if len(mem.byDis) != 0 {
		t.Fatalf("осталось %+v", mem.byDis)
	}
}

// Отказ не должен публиковать hotoffers.changed: подписчик не перечитывает список зря.
func TestRejectedHotOverrideDoesNotPublish(t *testing.T) {
	bus := events.New(func() time.Time { return hotNow })
	sub := bus.Subscribe()
	defer sub.Unsubscribe()
	rt := overridesRouter(&fakeOverrides{}, bus)
	rt.deps.HotOverrides = newMemOverrides()
	for _, c := range []struct{ dish, body string }{
		{"soup", `{"mode":"nope"}`},
		{"unicorn", `{"mode":"pin"}`},
		{"soup", `{"mode":"pin","note":"` + strings.Repeat("a", 201) + `"}`},
	} {
		if rec := putOverride(rt, c.dish, c.body); rec.Code < 400 {
			t.Fatalf("%s %s: %d", c.dish, c.body, rec.Code)
		}
	}
	select {
	case ev := <-sub.C:
		t.Fatalf("событие после отказа: %s", ev.Type)
	case <-time.After(50 * time.Millisecond):
	}
}

// ---- Отмена заказа ----

// memCancel повторяет правила store.CancelOrder: заказ в accepted отменяется один раз; тот же id корректировки —
// replay с тем же результатом; другой id на уже отменённый заказ — already_cancelled; чужой/неизвестный заказ — unknown_order.
// Просроченная часть остаётся в expired и в returned не попадает.
type memCancel struct {
	fakeCorrections
	orders    map[string]string // заказ → статус
	byID      map[string]store.CancelResult
	byIDOrder map[string]string
	result    store.CancelResult // что вернуть при первой отмене
}

func (m *memCancel) CancelOrder(_ context.Context, in store.CancelInput, _ func() time.Time) (store.CancelResult, error) {
	if r, ok := m.byID[in.ID]; ok {
		if m.byIDOrder[in.ID] != in.OrderID {
			return store.CancelResult{}, store.ErrCorrectionConflict
		}
		r.Replayed = true
		return r, nil
	}
	st, ok := m.orders[in.OrderID]
	switch {
	case !ok:
		return store.CancelResult{}, store.ErrUnknownOrder
	case st == "cancelled":
		return store.CancelResult{}, store.ErrAlreadyCancelled
	case st != store.OrderAccepted:
		return store.CancelResult{}, store.ErrOrderNotCancellable
	}
	m.orders[in.OrderID] = "cancelled"
	r := m.result
	r.ID, r.OrderID = in.ID, in.OrderID
	m.byID[in.ID], m.byIDOrder[in.ID] = r, in.OrderID
	return r, nil
}

func TestCancelOrderNegative(t *testing.T) {
	order := strings.ToLower(oid)
	rejected := "5b0c0c3e-8a51-4d0e-9d0b-000000000001"
	other := "5b0c0c3e-8a51-4d0e-9d0b-000000000002"
	m := &memCancel{
		orders: map[string]string{order: store.OrderAccepted, rejected: store.OrderRejected},
		byID:   map[string]store.CancelResult{}, byIDOrder: map[string]string{},
		result: store.CancelResult{
			Returned: []store.AdjustmentJSON{{BatchID: 1, ProductID: "cream", Qty: store.QtyJSON{Qty: 50, Unit: domain.UnitMl}}},
			// Партия лосося просрочена: часть в expired, в returned её нет (остатка не воскрешаем).
			Expired: []store.AdjustmentJSON{{BatchID: 2, ProductID: "salmon", Qty: store.QtyJSON{Qty: 100, Unit: domain.UnitG}}},
		},
	}
	rt := NewRouter(Deps{Now: func() time.Time { return standTime }, Corrections: m})
	cancel := func(orderID, id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/orders/"+orderID+"/cancel", strings.NewReader(`{"id":"`+id+`"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		rt.Handler().ServeHTTP(rec, req)
		return rec
	}
	code := func(rec *httptest.ResponseRecorder) string { return decodeErr(t, rec).Error.Code }

	// Первая отмена: 201, просроченное — в expired, а не в returned.
	rec := cancel(order, "c1")
	if rec.Code != 201 {
		t.Fatalf("первая отмена: %d %s", rec.Code, rec.Body)
	}
	var res store.CancelResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if len(res.Expired) != 1 || res.Expired[0].ProductID != "salmon" || len(res.Returned) != 1 || res.Returned[0].ProductID != "cream" || res.Replayed {
		t.Fatalf("returned/expired: %s", rec.Body)
	}
	for _, r := range res.Returned {
		if r.ProductID == "salmon" {
			t.Fatal("просроченный продукт вернулся на склад")
		}
	}
	// Тот же id — replay (200, replayed:true, тот же состав).
	rec = cancel(order, "c1")
	var again store.CancelResult
	_ = json.Unmarshal(rec.Body.Bytes(), &again)
	if rec.Code != 200 || !again.Replayed || len(again.Expired) != 1 || len(again.Returned) != 1 {
		t.Fatalf("replay: %d %s", rec.Code, rec.Body)
	}
	// Другой id на отменённый заказ — отказ.
	if rec = cancel(order, "c2"); rec.Code != 409 || code(rec) != CodeAlreadyCancelled {
		t.Fatalf("другой id: %d %s", rec.Code, rec.Body)
	}
	// Тот же id, но другой заказ — конфликт содержимого.
	if rec = cancel(other, "c1"); rec.Code != 409 || code(rec) != CodeCorrectionConflict {
		t.Fatalf("тот же id на другой заказ: %d %s", rec.Code, rec.Body)
	}
	// Несуществующий заказ — 404; в деталях id заказа, чужие данные не утекают.
	rec = cancel(other, "c3")
	if rec.Code != 404 || code(rec) != CodeUnknownOrder || decodeErr(t, rec).Error.Details["order_id"] != other {
		t.Fatalf("несуществующий: %d %s", rec.Code, rec.Body)
	}
	// Отклонённый заказ не отменяется.
	if rec = cancel(rejected, "c4"); rec.Code != 409 || code(rec) != CodeOrderNotCancellable {
		t.Fatalf("rejected: %d %s", rec.Code, rec.Body)
	}
	// Некорректные входы не доходят до хранилища.
	for name, c := range map[string]struct{ order, id string }{
		"не UUID": {"42", "c5"}, "пустой id": {order, ""}, "id с пробелом по краю": {order, " c5"},
		"id длиннее 128": {order, strings.Repeat("x", 129)},
	} {
		if rec = cancel(c.order, c.id); rec.Code != 400 || code(rec) != CodeValidation {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if len(m.byID) != 1 {
		t.Fatalf("отказы оставили следы: %+v", m.byID)
	}
}

// ---- Поставка без срока (FS-160) ----

// memDeliveries повторяет идемпотентность store.ReceiveDelivery: тот же id и то же тело — replay, иное тело — конфликт.
type memDeliveries struct {
	fakeStock
	byID map[string]store.DeliveryInput
}

func (m *memDeliveries) ReceiveDelivery(_ context.Context, in store.DeliveryInput, _ func() time.Time, _ *time.Location) (store.DeliveryResponse, error) {
	if prev, ok := m.byID[in.ID]; ok {
		a, _ := json.Marshal(prev)
		b, _ := json.Marshal(in)
		if !bytes.Equal(a, b) {
			return store.DeliveryResponse{}, store.ErrDeliveryConflict
		}
		return store.DeliveryResponse{ID: in.ID, Replayed: true, Batches: []store.BatchJSON{}, Warnings: []store.WarningJSON{}}, nil
	}
	m.byID[in.ID] = in
	return store.DeliveryResponse{ID: in.ID, Batches: []store.BatchJSON{}, Warnings: []store.WarningJSON{}}, nil
}

func TestPostDeliveryNegative(t *testing.T) {
	const ok = `{"product_id":"salmon","quantity":"1","unit":"kg","shelf_life_hours":72}`
	cases := []struct {
		name, items, field string
	}{
		{"нет срока в единственной позиции", `{"product_id":"salmon","quantity":"1","unit":"kg"}`, "items[0].expires_on"},
		{"нет срока во второй позиции", ok + `,{"product_id":"salmon","quantity":"1","unit":"kg"}`, "items[1].expires_on"},
		{"нет срока у нового продукта", `{"product_id":"new","name":"Новый","quantity":"1","unit":"kg"}`, "items[0].expires_on"},
		{"пустой expires_on", `{"product_id":"salmon","quantity":"1","unit":"kg","expires_on":""}`, "items[0].expires_on"},
		{"оба срока сразу", `{"product_id":"salmon","quantity":"1","unit":"kg","shelf_life_hours":72,"expires_on":"2026-10-12"}`, "items[0].shelf_life_hours"},
		{"shelf_life_hours = 0", `{"product_id":"salmon","quantity":"1","unit":"kg","shelf_life_hours":0}`, "items[0].shelf_life_hours"},
		{"shelf_life_hours отрицательный", `{"product_id":"salmon","quantity":"1","unit":"kg","shelf_life_hours":-5}`, "items[0].shelf_life_hours"},
		{"shelf_life_hours больше предела", `{"product_id":"salmon","quantity":"1","unit":"kg","shelf_life_hours":87601}`, "items[0].shelf_life_hours"},
		{"expires_on не дата", `{"product_id":"salmon","quantity":"1","unit":"kg","expires_on":"завтра"}`, "items[0].expires_on"},
		{"expires_on несуществующий день", `{"product_id":"salmon","quantity":"1","unit":"kg","expires_on":"2026-02-30"}`, "items[0].expires_on"},
		{"expires_on со временем", `{"product_id":"salmon","quantity":"1","unit":"kg","expires_on":"2026-10-12T10:00:00Z"}`, "items[0].expires_on"},
		{"дробные штуки", `{"product_id":"eggs","name":"Яйца","quantity":"1.5","unit":"pcs","shelf_life_hours":72}`, "items[0].quantity"},
		{"точность выше базовой единицы", `{"product_id":"salmon","quantity":"0.0001","unit":"kg","shelf_life_hours":72}`, "items[0].quantity"},
		{"нулевое количество", `{"product_id":"salmon","quantity":"0","unit":"kg","shelf_life_hours":72}`, "items[0].quantity"},
		{"отрицательное количество", `{"product_id":"salmon","quantity":"-1","unit":"kg","shelf_life_hours":72}`, "items[0].quantity"},
		{"количество числом", `{"product_id":"salmon","quantity":1,"unit":"kg","shelf_life_hours":72}`, ""},
	}
	for _, c := range cases {
		fs := &fakeStock{}
		rec := post(t, fs, item(c.items))
		if rec.Code != 400 {
			t.Errorf("%s: %d, ожидали 400 (%s)", c.name, rec.Code, rec.Body)
			continue
		}
		e := decodeErr(t, rec)
		if e.Error.Code != CodeValidation || (c.field != "" && e.Error.Details["field"] != c.field) {
			t.Errorf("%s: %s", c.name, rec.Body)
		}
		if fs.in.ID != "" {
			t.Errorf("%s: отказ дошёл до хранилища", c.name)
		}
	}
}

// Срок «вчера» принимается: партия попадает на склад уже просроченной, а оператор получает предупреждение
// (expired_on_arrival строит хранилище). API срок в прошлом не отвергает — это фиксируем, а не молчим.
func TestPostDeliveryPastExpiryReachesStore(t *testing.T) {
	fs := &fakeStock{}
	rec := post(t, fs, item(`{"product_id":"salmon","quantity":"1","unit":"kg","expires_on":"2020-01-01"}`))
	if rec.Code != 201 || len(fs.in.Items) != 1 || fs.in.Items[0].ExpiresOn != "2020-01-01" {
		t.Fatalf("%d %s %+v", rec.Code, rec.Body, fs.in.Items)
	}
}

// Нет срока, пойманного на уровне хранилища (позиция без срока и без срока в карточке), — тоже 400 с индексом позиции.
func TestPostDeliveryStoreNoShelfLifeIsValidationError(t *testing.T) {
	fs := &fakeStock{err: &store.NoShelfLifeError{Index: 2, ProductID: "salmon"}}
	rec := post(t, fs, item(`{"product_id":"salmon","quantity":"1","unit":"kg","shelf_life_hours":72}`))
	e := decodeErr(t, rec)
	if rec.Code != 400 || e.Error.Code != CodeValidation || e.Error.Details["field"] != "items[2].expires_on" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestPostDeliveryReplayAndConflict(t *testing.T) {
	ms := &memDeliveries{byID: map[string]store.DeliveryInput{}}
	send := func(items string) *httptest.ResponseRecorder {
		rt := NewRouter(Deps{Now: func() time.Time { return standTime }, Stock: ms})
		req := httptest.NewRequest(http.MethodPost, "/api/deliveries", strings.NewReader(item(items)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		rt.Handler().ServeHTTP(rec, req)
		return rec
	}
	const body = `{"product_id":"salmon","quantity":"1","unit":"kg","shelf_life_hours":72}`
	if rec := send(body); rec.Code != 201 {
		t.Fatalf("первая: %d %s", rec.Code, rec.Body)
	}
	if rec := send(body); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"replayed":true`) {
		t.Fatalf("повтор: %d %s", rec.Code, rec.Body)
	}
	for name, other := range map[string]string{
		"другое количество": `{"product_id":"salmon","quantity":"2","unit":"kg","shelf_life_hours":72}`,
		"другой срок":       `{"product_id":"salmon","quantity":"1","unit":"kg","shelf_life_hours":96}`,
		"срок датой":        `{"product_id":"salmon","quantity":"1","unit":"kg","expires_on":"2026-10-12"}`,
	} {
		rec := send(other)
		if rec.Code != 409 || decodeErr(t, rec).Error.Code != CodeDeliveryConf {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}

// ---- GET /api/supply-suggestions: shelf_life_hours ----

// Поле есть в каждой строке (ключ присутствует, а не опущен): число — у продукта со сроком, null — у продукта без срока.
func TestSupplyShelfLifeHoursPresentAndNull(t *testing.T) {
	now := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	shelf := 48
	data := store.SupplyData{
		Products: map[string]store.ProductRef{"salmon": {Name: "Лосось", Unit: domain.UnitG}, "rice": {Name: "Рис", Unit: domain.UnitG}},
		Input: domain.SupplyInput{Now: now, CoverDays: 3, MinPortions: 10, Products: []domain.SupplyProduct{
			{ID: "salmon", Name: "Лосось", LeadDays: 1, PackSize: 500, UsableStock: 400, RatePerDay: 1000, MaxRecipeNorm: 200, BlocksDishes: 1},
			{ID: "rice", Name: "Рис", LeadDays: 1, PackSize: 500, UsableStock: 100, RatePerDay: 1000, MaxRecipeNorm: 100, BlocksDishes: 1},
		}},
		Meta: map[string]store.SupplyMeta{"salmon": {ShelfLifeHours: &shelf}, "rice": {}},
	}
	rt := NewRouter(Deps{Supply: fakeSupply{data}, Now: func() time.Time { return now }})
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/supply-suggestions", nil))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range rows {
		raw, present := r["shelf_life_hours"]
		if !present {
			t.Fatalf("в строке нет ключа shelf_life_hours: %s", rec.Body)
		}
		var id string
		_ = json.Unmarshal(r["product_id"], &id)
		got[id] = string(raw)
	}
	if got["salmon"] != "48" || got["rice"] != "null" || len(got) != 2 {
		t.Fatalf("shelf_life_hours по продуктам: %v", got)
	}
}

// ---- Telegram: токен бота не попадает ни в ответы, ни в журнал ----

func TestTelegramBotTokenNeverLeaks(t *testing.T) {
	e := newTgEnv(t, true)
	e.links.byID[777] = "waiter"
	good := signInitData(tgToken, 777, standTime.Add(-time.Minute))
	attempts := map[string]string{
		"успех":               good,
		"повтор":              good,
		"подделанная подпись": signInitData("999:other-token", 777, standTime),
		"подпись с токеном в данных": strings.Replace(good, "Ivan", tgToken, 1),
		"просроченный auth_date":     signInitData(tgToken, 777, standTime.Add(-25*time.Hour)),
		"auth_date из будущего":      signInitData(tgToken, 777, standTime.Add(time.Hour)),
		"нет привязки":               signInitData(tgToken, 31337, standTime),
		"мусор":                      "hash=zz&user=" + tgToken,
		"токен как init_data":        tgToken,
	}
	for name, data := range attempts {
		rec := e.tgLogin(data)
		hdr := rec.Result().Header
		if strings.Contains(rec.Body.String(), tgToken) || strings.Contains(strings.Join(hdr["Set-Cookie"], ";"), tgToken) {
			t.Errorf("%s: токен в ответе: %s", name, rec.Body)
		}
	}
	// Отказы имеют ожидаемые коды.
	for name, want := range map[string]int{
		"подделанная подпись": 401, "просроченный auth_date": 401, "auth_date из будущего": 401, "мусор": 401, "токен как init_data": 401, "нет привязки": 403,
	} {
		if rec := e.tgLogin(attempts[name]); rec.Code != want {
			t.Errorf("%s: %d, ожидали %d (%s)", name, rec.Code, want, rec.Body)
		}
	}
	if rec := e.tgLogin(attempts["нет привязки"]); !strings.Contains(rec.Body.String(), codeTgNotLinked) {
		t.Errorf("нет привязки: %s", rec.Body)
	}
	logged := e.log.String()
	if logged == "" {
		t.Fatal("журнал теста пуст: проверять нечего")
	}
	if strings.Contains(logged, tgToken) || strings.Contains(logged, "TEST-bot-token-secret") {
		t.Fatalf("токен бота в журнале:\n%s", logged)
	}
	// Подписанные данные пользователя (hash) тоже не пишем: по ним можно войти повторно в течение суток.
	if h := signedHash(good); h != "" && strings.Contains(logged, h) {
		t.Fatalf("hash initData в журнале:\n%s", logged)
	}
}

func signedHash(initData string) string {
	for _, kv := range strings.Split(initData, "&") {
		if strings.HasPrefix(kv, "hash=") {
			return strings.TrimPrefix(kv, "hash=")
		}
	}
	return ""
}

// Вход под чужой ролью невозможен: повтор initData после снятия привязки — 403, а не вход.
func TestTelegramReplayAfterUnlinkIsRefused(t *testing.T) {
	e := newTgEnv(t, true)
	e.links.byID[777] = "waiter"
	d := signInitData(tgToken, 777, standTime.Add(-time.Minute))
	if rec := e.tgLogin(d); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	delete(e.links.byID, 777)
	rec := e.tgLogin(d)
	if rec.Code != 403 || sessionOf(rec) != nil || !strings.Contains(rec.Body.String(), codeTgNotLinked) {
		t.Fatalf("после снятия привязки: %d %s", rec.Code, rec.Body)
	}
}
