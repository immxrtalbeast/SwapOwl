package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSupplierCodesSetReplaceDeleteCascade(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO products (id, name, unit, price_minor_per_1000) VALUES ('milk','Молоко','ml',9000),('cream','Сливки','ml',30000)`); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	if _, err := db.SetSupplierCode(ctx, SupplierCode{Code: "04607001234567", ProductID: "milk", SetAt: at, SetBy: "anna"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetSupplierCode(ctx, SupplierCode{Code: "ART-77", ProductID: "cream", SetAt: at.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	// Тот же код → другой продукт: заменяется, время обновляется (один код — один продукт).
	if _, err := db.SetSupplierCode(ctx, SupplierCode{Code: "04607001234567", ProductID: "cream", SetAt: at.Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	list, err := db.ListSupplierCodes(ctx)
	if err != nil || len(list) != 2 || list[0].Code != "04607001234567" || list[0].ProductID != "cream" || !list[0].SetAt.Equal(at.Add(2*time.Minute)) {
		t.Fatalf("%v %+v", err, list)
	}
	if _, err := db.SetSupplierCode(ctx, SupplierCode{Code: "X", ProductID: "unicorn", SetAt: at}); !errors.Is(err, ErrSupplierCodeProduct) {
		t.Fatalf("неизвестный продукт: %v", err)
	}
	if err := db.DeleteSupplierCode(ctx, "ART-77"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteSupplierCode(ctx, "ART-77"); err != nil {
		t.Fatalf("повтор DELETE: %v", err)
	}
	// Продукт удалён из справочника — сопоставление уходит вместе с ним.
	if _, err := db.Pool.Exec(ctx, `DELETE FROM products WHERE id = 'cream'`); err != nil {
		t.Fatal(err)
	}
	if list, _ := db.ListSupplierCodes(ctx); len(list) != 0 {
		t.Fatalf("после удаления продукта: %+v", list)
	}
}
