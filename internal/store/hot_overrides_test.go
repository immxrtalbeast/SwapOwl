package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHotOverridesSetReplaceDelete(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `INSERT INTO dishes (id, name) VALUES ('soup','Суп'),('steak','Стейк')`); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if _, err := db.SetHotOverride(ctx, HotOverride{DishID: "soup", Mode: HotPin, Note: "к обеду", SetAt: at, SetBy: "anna"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetHotOverride(ctx, HotOverride{DishID: "steak", Mode: HotHide, SetAt: at.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	// Новое решение по тому же блюду заменяет прежнее.
	if _, err := db.SetHotOverride(ctx, HotOverride{DishID: "soup", Mode: HotHide, SetAt: at.Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	list, err := db.ListHotOverrides(ctx)
	if err != nil || len(list) != 2 || list[0].DishID != "soup" || list[0].Mode != HotHide || list[0].Note != "" {
		t.Fatalf("%v %+v", err, list)
	}
	if _, err := db.SetHotOverride(ctx, HotOverride{DishID: "unicorn", Mode: HotPin, SetAt: at}); !errors.Is(err, ErrHotOverrideDish) {
		t.Fatalf("unknown dish: %v", err)
	}
	if err := db.DeleteHotOverride(ctx, "soup"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteHotOverride(ctx, "soup"); err != nil {
		t.Fatalf("повтор DELETE: %v", err)
	}
	// Блюдо удалено из справочника — решение уходит вместе с ним.
	if _, err := db.Pool.Exec(ctx, `DELETE FROM dishes WHERE id = 'steak'`); err != nil {
		t.Fatal(err)
	}
	if list, _ := db.ListHotOverrides(ctx); len(list) != 0 {
		t.Fatalf("после удаления блюда: %+v", list)
	}
}
