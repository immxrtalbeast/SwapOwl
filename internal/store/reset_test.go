package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestDeleteStandDataClearsCatalogKeepsUsers(t *testing.T) {
	db, clk := orderDB(t)
	ctx := context.Background()
	receive(t, db, clk, "D1", 500, 72)
	if _, err := db.PlaceOrder(ctx, order(o1, 1, 100), clk.Now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PlaceOrder(ctx, OrderInput{ID: o2, Source: SourcePOS, Items: order(o2, 1, 0).Items, Needs: order(o2, 1, 9999).Needs}, clk.Now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO products (id, name, unit, auto_created) VALUES ('auto','Авто','g',true);
		INSERT INTO clock_jumps VALUES (now(), now());
		INSERT INTO users (login, password_hash, role) VALUES ('admin','x','admin')`); err != nil {
		t.Fatal(err)
	}
	if err := db.InTx(ctx, func(tx pgx.Tx) error { return DeleteStandData(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"movements": 0, "shortfalls": 0, "order_items": 0, "orders": 0, "batches": 0,
		"deliveries": 0, "clock_jumps": 0, "dishes": 0, "recipe_items": 0, "products": 0, "users": 1} {
		var n int
		if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != want {
			t.Errorf("%s: %d rows (err %v), want %d", table, n, err, want)
		}
	}
}
