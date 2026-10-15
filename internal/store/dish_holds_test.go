package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDishHoldsSetReplaceDelete(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO dishes (id, name) VALUES ('soup','Суп'),('steak','Стейк')`); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	if _, err := db.SetDishHold(ctx, DishHold{DishID: "soup", Note: "сломана плита", SetAt: at, SetBy: "anna"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetDishHold(ctx, DishHold{DishID: "steak", SetAt: at.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	// Повтор заменяет заметку и время.
	if _, err := db.SetDishHold(ctx, DishHold{DishID: "soup", Note: "ждём поставку", SetAt: at.Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	list, err := db.ListDishHolds(ctx)
	if err != nil || len(list) != 2 || list[0].DishID != "soup" || list[0].Note != "ждём поставку" {
		t.Fatalf("%v %+v", err, list)
	}
	if _, err := db.SetDishHold(ctx, DishHold{DishID: "unicorn", SetAt: at}); !errors.Is(err, ErrDishHoldDish) {
		t.Fatalf("неизвестное блюдо: %v", err)
	}
	if err := db.DeleteDishHold(ctx, "soup"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteDishHold(ctx, "soup"); err != nil {
		t.Fatalf("повтор DELETE: %v", err)
	}
	// Блюдо удалено из справочника — пауза уходит вместе с ним.
	if _, err := db.Pool.Exec(ctx, `DELETE FROM dishes WHERE id = 'steak'`); err != nil {
		t.Fatal(err)
	}
	if list, _ := db.ListDishHolds(ctx); len(list) != 0 {
		t.Fatalf("после удаления блюда: %+v", list)
	}
}

// Заказ на блюдо с паузой отклоняется целиком в той же транзакции: партии и движения не тронуты, ответ сохранён
// (повтор того же id отдаёт тот же отказ), продажа из кассы паузой не отклоняется, после снятия паузы заказ проходит.
func TestPlaceOrderOnHeldDish(t *testing.T) {
	db, clk := orderDB(t)
	ctx := context.Background()
	receive(t, db, clk, "D1", 1000, 72)
	if _, err := db.SetDishHold(ctx, DishHold{DishID: "soup", Note: "сломана плита", SetAt: clk.Now()}); err != nil {
		t.Fatal(err)
	}
	before, soldBefore := remainingSum(t, db)

	resp, err := db.PlaceOrder(ctx, order(o1, 2, 200), clk.Now)
	if err != nil || resp.Status != OrderRejected || len(resp.Held) != 1 || resp.Held[0].DishID != "soup" || resp.Held[0].Note != "сломана плита" {
		t.Fatalf("%v %+v", err, resp)
	}
	if len(resp.Missing) != 0 || len(resp.Writeoffs) != 0 {
		t.Fatalf("отказ по паузе не должен нести нехватку или списания: %+v", resp)
	}
	if sum, sold := remainingSum(t, db); sum != before || sold != soldBefore {
		t.Fatalf("склад изменился: %d/%d → %d/%d", before, soldBefore, sum, sold)
	}
	// Отказ по паузе — не дефицит продуктов: в отчёт «Отказов» он не попадает.
	rep, err := db.ShortagesReport(ctx, clk.Now().Add(-48*time.Hour), clk.Now().Add(48*time.Hour))
	if err != nil || rep.RejectedOrders != 0 || len(rep.ByProduct) != 0 {
		t.Fatalf("отчёт о дефиците: %v %+v", err, rep)
	}
	// Повтор с тем же id — тот же сохранённый отказ.
	again, err := db.PlaceOrder(ctx, order(o1, 2, 200), clk.Now)
	if err != nil || !again.Replayed || again.Status != OrderRejected || len(again.Held) != 1 {
		t.Fatalf("повтор: %v %+v", err, again)
	}

	// Блюдо без паузы в том же заказе не спасает: отказ целиком.
	mixed := OrderInput{ID: o2, Items: []OrderItem{{DishID: "soup", Portions: 1}, {DishID: "sauce", Portions: 1}}, Needs: order(o2, 1, 200).Needs}
	if resp, err := db.PlaceOrder(ctx, mixed, clk.Now); err != nil || resp.Status != OrderRejected || len(resp.Held) != 1 {
		t.Fatalf("смешанный: %v %+v", err, resp)
	}
	if sum, _ := remainingSum(t, db); sum != before {
		t.Fatalf("склад изменился после смешанного заказа: %d", sum)
	}

	// Продажа из кассы — факт: пауза её не отклоняет.
	pos := order("00000000-0000-4000-8000-000000000003", 1, 100)
	pos.Source = SourcePOS
	if resp, err := db.PlaceOrder(ctx, pos, clk.Now); err != nil || resp.Status != OrderAccepted {
		t.Fatalf("касса: %v %+v", err, resp)
	}

	if err := db.DeleteDishHold(ctx, "soup"); err != nil {
		t.Fatal(err)
	}
	if resp, err := db.PlaceOrder(ctx, order("00000000-0000-4000-8000-000000000004", 1, 100), clk.Now); err != nil || resp.Status != OrderAccepted {
		t.Fatalf("после снятия паузы: %v %+v", err, resp)
	}
}
