package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

var ladderLoc = time.FixedZone("MSK", 3*3600)
var ladderAt = time.Date(2026, 10, 16, 16, 0, 0, 0, ladderLoc)

// Лосось 900 ₽/кг (90000 коп. за 1000 г). Стейк: 200 г лосося, цена 1200 ₽. Паста: 150 г, цена 700 ₽.
func ladderSnap() store.Snapshot {
	return store.Snapshot{
		Dishes: []domain.Dish{
			{ID: "steak", Name: "Стейк из лосося", Recipe: []domain.RecipeItem{{ProductID: "salmon", Quantity: 200}}},
			{ID: "pasta", Name: "Паста с лососем", Recipe: []domain.RecipeItem{{ProductID: "salmon", Quantity: 150}}},
		},
		Price:     map[string]int64{"salmon": 90000},
		DishPrice: map[string]int64{"steak": 120000, "pasta": 70000},
	}
}

func salmonAlert(grams int64) store.AlertView {
	return store.AlertView{BatchID: 17, ProductID: "salmon", ProductName: "Лосось", Status: "expiring",
		Remaining: store.QtyJSON{Qty: grams, Unit: "g"}, ExpiresAt: ladderAt}
}

func steakOffer(target int64) OfferJSON {
	return OfferJSON{DishID: "steak", DishName: "Стейк из лосося", TargetToday: target, TargetPortions: target, SellUntil: ladderAt,
		Reasons: []HotReasonJSON{{ProductID: "salmon", BatchID: 17}}}
}

func step(t *testing.T, b LadderBatchJSON, kind string) LadderStepJSON {
	t.Helper()
	for _, s := range b.Steps {
		if s.Kind == kind {
			return s
		}
	}
	t.Fatalf("нет ступени %s", kind)
	return LadderStepJSON{}
}

// Продажа целиком покрывает партию: 1 кг лосося, 5 порций стейка по 200 г → спасено 900 ₽, скидка не нужна.
func TestLadderSellCoversBatch(t *testing.T) {
	got := buildLadder(ladderIn{Alerts: []store.AlertView{salmonAlert(1000)}, Offers: []OfferJSON{steakOffer(5)}, Snap: ladderSnap(), Loc: ladderLoc})
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	b := got[0]
	sell := step(t, b, LadderSell)
	if sell.SavedMinor != 90000 || sell.CostMinor != 0 || sell.NetMinor != 90000 || sell.Status != "" {
		t.Fatalf("sell: %+v", sell)
	}
	if b.AtRiskMinor != 90000 || b.Best != LadderSell || b.CombinedMinor != 90000 {
		t.Fatalf("%+v", b)
	}
	if d := step(t, b, LadderDiscount); d.Status != "unavailable" || d.NetMinor != 0 {
		t.Fatalf("скидка при полном покрытии: %+v", d)
	}
}

// Продажа покрывает часть: 1 кг, 2 порции стейка (400 г). Остаток 600 г — витринная паста со скидкой 20 %:
// 4 порции по 150 г; цена ступени = 4 × 700 ₽ × 20 % = 560 ₽; спасено 540 ₽ (600 г × 900 ₽/кг).
func TestLadderDiscountCoversRemainder(t *testing.T) {
	in := ladderIn{Alerts: []store.AlertView{salmonAlert(1000)}, Offers: []OfferJSON{steakOffer(2)},
		Rescue: []RescueJSON{{DishID: "pasta", DishName: "Паста с лососем", ServingsLeft: 10, DiscountPercent: 20}}, Snap: ladderSnap(), Loc: ladderLoc}
	b := buildLadder(in)[0]
	sell, disc := step(t, b, LadderSell), step(t, b, LadderDiscount)
	if sell.SavedMinor != 36000 { // 400 г × 90 коп/г
		t.Fatalf("sell: %+v", sell)
	}
	if disc.SavedMinor != 54000 || disc.CostMinor != 56000 || disc.NetMinor != -2000 {
		t.Fatalf("discount: %+v", disc)
	}
	// Скидка дороже того, что она спасает: лучшая — продажа, а в итог скидка не входит.
	if b.Best != LadderSell || b.CombinedMinor != 36000 {
		t.Fatalf("%+v", b)
	}
}

func TestLadderDiscountWorthIt(t *testing.T) {
	// Дорогой продукт, дешёвое блюдо: скидка окупается. Лосось 5000 ₽/кг, паста 300 ₽.
	snap := ladderSnap()
	snap.Price["salmon"] = 500000
	snap.DishPrice["pasta"] = 30000
	in := ladderIn{Alerts: []store.AlertView{salmonAlert(600)}, Offers: nil,
		Rescue: []RescueJSON{{DishID: "pasta", DishName: "Паста", ServingsLeft: 10, DiscountPercent: 20}}, Snap: snap, Loc: ladderLoc}
	b := buildLadder(in)[0]
	disc := step(t, b, LadderDiscount)
	// 600 г → 4 порции; спасено 600 × 500 = 300000; цена 4 × 300 ₽ × 20 % = 240 ₽ = 24000.
	if disc.SavedMinor != 300000 || disc.CostMinor != 24000 || disc.NetMinor != 276000 || b.Best != LadderDiscount {
		t.Fatalf("%+v best=%s", disc, b.Best)
	}
	if sell := step(t, b, LadderSell); sell.Status != "unavailable" || sell.NetMinor != 0 {
		t.Fatalf("sell без предложения: %+v", sell)
	}
}

func TestLadderNothingAvailable(t *testing.T) {
	b := buildLadder(ladderIn{Alerts: []store.AlertView{salmonAlert(500)}, Snap: ladderSnap(), Loc: ladderLoc})[0]
	if b.Best != "" || b.CombinedMinor != 0 {
		t.Fatalf("%+v", b)
	}
}

// Заготовка и передача в точку — всегда без расчёта; не «лучшие», даже если других ступеней нет.
func TestLadderPrepAndTransferAreNotCalculated(t *testing.T) {
	b := buildLadder(ladderIn{Alerts: []store.AlertView{salmonAlert(500)}, Offers: []OfferJSON{steakOffer(2)}, Snap: ladderSnap(), Loc: ladderLoc})[0]
	prep, tr := step(t, b, LadderPrep), step(t, b, LadderTransfer)
	if prep.Status != ladderRoadmap || prep.NetMinor != 0 || tr.Status != ladderNetwork || tr.NetMinor != 0 {
		t.Fatalf("%+v %+v", prep, tr)
	}
	if b.Best == LadderPrep || b.Best == LadderTransfer {
		t.Fatalf("best %s", b.Best)
	}
}

// Только истекающие партии с остатком; порядок — по убыванию денег под риском; ответ детерминирован.
func TestLadderFiltersAndOrders(t *testing.T) {
	snap := ladderSnap()
	snap.Price["cream"] = 30000
	alerts := []store.AlertView{
		{BatchID: 1, ProductID: "cream", ProductName: "Сливки", Status: "expiring", Remaining: store.QtyJSON{Qty: 1000, Unit: "ml"}, ExpiresAt: ladderAt},
		salmonAlert(1000),
		{BatchID: 3, ProductID: "salmon", ProductName: "Лосось", Status: "expired", Remaining: store.QtyJSON{Qty: 700, Unit: "g"}, ExpiresAt: ladderAt},
		{BatchID: 4, ProductID: "salmon", ProductName: "Лосось", Status: "expiring", Remaining: store.QtyJSON{Qty: 0, Unit: "g"}, ExpiresAt: ladderAt},
	}
	in := ladderIn{Alerts: alerts, Snap: snap, Loc: ladderLoc}
	a, b := buildLadder(in), buildLadder(in)
	if len(a) != 2 || a[0].BatchID != 17 || a[1].BatchID != 1 {
		t.Fatalf("%+v", a)
	}
	if a[0].AtRiskMinor != 90000 || a[1].AtRiskMinor != 30000 || len(b) != 2 || b[0].BatchID != a[0].BatchID {
		t.Fatalf("%+v %+v", a, b)
	}
}

func TestLadderNoPriceIsNotMoney(t *testing.T) {
	snap := ladderSnap()
	delete(snap.Price, "salmon")
	b := buildLadder(ladderIn{Alerts: []store.AlertView{salmonAlert(1000)}, Offers: []OfferJSON{steakOffer(5)}, Snap: snap, Loc: ladderLoc})[0]
	if s := step(t, b, LadderSell); s.Status != "no_price" || s.NetMinor != 0 {
		t.Fatalf("%+v", s)
	}
	if b.AtRiskMinor != 0 || b.Best != "" {
		t.Fatalf("%+v", b)
	}
}

// HTTP: без источников маршрута нет; с ними — массив (пустой, не null).
func TestLadderRouteEmptyIsArray(t *testing.T) {
	rt := NewRouter(Deps{Monitor: &fakeMonitor{}, HotOffers: fakeSnap{ladderSnap()}, Now: func() time.Time { return hotNow },
		HotConfig: HotOffersConfig{DefaultAlgo: "b"}})
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/rescue-ladder", nil))
	if rec.Code != 200 || rec.Body.String() != "[]\n" && rec.Body.String() != "[]" {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
	bare := NewRouter(Deps{Now: func() time.Time { return hotNow }})
	rec = httptest.NewRecorder()
	bare.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/rescue-ladder", nil))
	if rec.Code != 404 {
		t.Fatalf("без зависимостей: %d", rec.Code)
	}
}

// Витрина строится из того же горячего предложения: порции скидки того же блюда уже входят в цель продажи
// и второй раз не засчитываются (иначе «не спишем ≈ N ₽» завышено).
func TestLadderDoesNotDoubleCountSameDish(t *testing.T) {
	// 10 кг лосося, стейк по 200 г, цель 30 порций (6 кг); витрина по тому же стейку даёт ещё 20 порций.
	in := ladderIn{Alerts: []store.AlertView{salmonAlert(10000)}, Offers: []OfferJSON{steakOffer(30)},
		Rescue: []RescueJSON{{DishID: "steak", DishName: "Стейк из лосося", ServingsLeft: 20, DiscountPercent: 20}}, Snap: ladderSnap(), Loc: ladderLoc}
	b := buildLadder(in)[0]
	sell, disc := step(t, b, LadderSell), step(t, b, LadderDiscount)
	if sell.SavedMinor != 540000 { // 6000 г × 90 коп/г
		t.Fatalf("sell: %+v", sell)
	}
	if disc.SavedMinor != 0 || disc.Status != "unavailable" {
		t.Fatalf("скидка по тому же блюду засчитана второй раз: %+v", disc)
	}
	if b.CombinedMinor != 540000 {
		t.Fatalf("combined %d, ждали 540000 (только продажа)", b.CombinedMinor)
	}
}
