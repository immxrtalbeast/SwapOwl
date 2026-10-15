package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Бэктест на БД: суп заказывают по 10 порций в сутки, априор 5 — обучение к концу окна почти догоняет, база ошибается на 50 %.
func TestForecastAccuracyOnDB(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO dishes (id, name, prior_daily) VALUES ('soup','Суп',5),('steak','Стейк',2)`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 11, 0, 0, 0, time.UTC)
	for k := 1; k <= 14; k++ {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", k)
		at := now.Add(-time.Duration(k)*24*time.Hour + 6*time.Hour)
		if _, err := db.Pool.Exec(ctx, `INSERT INTO orders (id, body_hash, status, at, response, source) VALUES ($1, '\x00', 'accepted', $2, '{}', 'api')`, id, at); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO order_items (order_id, dish_id, portions) VALUES ($1, 'soup', 10)`, id); err != nil {
			t.Fatal(err)
		}
	}
	p := SnapshotParams{WindowDays: 7, PriorDays: 2}
	rep, err := db.ForecastAccuracy(ctx, now, 7, p)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.EnoughData || rep.DaysUsed != 7 || rep.Days != 7 {
		t.Fatalf("%+v", rep)
	}
	if rep.BaselineWAPEPct <= rep.WAPEPct {
		t.Fatalf("обучение не лучше априора: %v против %v", rep.WAPEPct, rep.BaselineWAPEPct)
	}
	if len(rep.Dishes) == 0 || rep.Dishes[0].DishID != "soup" || rep.Dishes[0].DishName != "Суп" || rep.Dishes[0].ActualPerDay != 10 {
		t.Fatalf("%+v", rep.Dishes)
	}
	// Блюдо без заказов и с нулевым прогнозом в отчёт не попадает; стейк с априором 2 — попадает (прогноз > 0, факт 0).
	// Отсутствие записей и повтор запроса — тот же результат (чтение детерминировано).
	again, err := db.ForecastAccuracy(ctx, now, 7, p)
	if err != nil || again.WAPEPct != rep.WAPEPct {
		t.Fatalf("%v %+v", err, again)
	}
}

// Прыжок часов растягивает окно оценки назад: заказы до прыжка должны читаться полностью, иначе λ занижается и WAPE
// растёт на ровном спросе (регрессия: нижняя граница истории по числу суток).
func TestForecastAccuracyHistoryAcrossClockJump(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO dishes (id, name, prior_daily) VALUES ('soup','Суп',10)`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 11, 0, 0, 0, time.UTC)
	jumpFrom, jumpTo := now.Add(-10*24*time.Hour), now.Add(-5*24*time.Hour+time.Hour)
	if _, err := db.Pool.Exec(ctx, `INSERT INTO clock_jumps (from_at, to_at) VALUES ($1, $2)`, jumpFrom, jumpTo); err != nil {
		t.Fatal(err)
	}
	for k := 1; k <= 40; k++ {
		at := now.Add(-time.Duration(k)*24*time.Hour + 6*time.Hour)
		if at.After(jumpFrom) && at.Before(jumpTo) {
			continue // в пропущенном интервале заказов быть не могло
		}
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", k)
		if _, err := db.Pool.Exec(ctx, `INSERT INTO orders (id, body_hash, status, at, response, source) VALUES ($1, '\x00', 'accepted', $2, '{}', 'api')`, id, at); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO order_items (order_id, dish_id, portions) VALUES ($1, 'soup', 10)`, id); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := db.ForecastAccuracy(ctx, now, 3, SnapshotParams{WindowDays: 14, PriorDays: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.EnoughData || rep.WAPEPct != 0 {
		t.Fatalf("ровный спрос, равный априору: WAPE %v, %+v", rep.WAPEPct, rep)
	}
}
