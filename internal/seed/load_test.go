package seed

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

func freshDB(t *testing.T) *store.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, url, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err := db.Pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}

func loadSeed(t *testing.T, db *store.DB) {
	t.Helper()
	if err := db.InTx(context.Background(), func(tx pgx.Tx) error { return Load(context.Background(), tx, testT0) }); err != nil {
		t.Fatal(err)
	}
}

func snapshot(t *testing.T, db *store.DB) string {
	t.Helper()
	var out string
	for _, q := range []string{
		`SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id), '[]') FROM products x`,
		`SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id), '[]') FROM dishes x`,
		`SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY dish_id, product_id), '[]') FROM recipe_items x`,
		`SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id), '[]') FROM deliveries x`,
		`SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id), '[]') FROM batches x`,
		`SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id), '[]') FROM orders x`,
		`SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY order_id, dish_id), '[]') FROM order_items x`,
		`SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id), '[]') FROM movements x`,
	} {
		var s string
		if err := db.Pool.QueryRow(context.Background(), q).Scan(&s); err != nil {
			t.Fatal(err)
		}
		out += s + "\n"
	}
	return out
}

func TestIsEmptyAndLoad(t *testing.T) {
	db := freshDB(t)
	ctx := context.Background()
	if empty, err := IsEmpty(ctx, db.Pool); err != nil || !empty {
		t.Fatalf("пустая БД: %v %v", empty, err)
	}
	loadSeed(t, db)
	if empty, err := IsEmpty(ctx, db.Pool); err != nil || empty {
		t.Fatalf("после загрузки: %v %v", empty, err)
	}
}

// Инвариант 1: остаток партии = поступило − списано по журналу, остаток не отрицателен.
func TestLoadConservation(t *testing.T) {
	db := freshDB(t)
	loadSeed(t, db)
	var bad int
	err := db.Pool.QueryRow(context.Background(), `
		SELECT count(*) FROM batches b WHERE b.remaining < 0 OR b.remaining <> (
			SELECT coalesce(sum(CASE WHEN m.kind = 'receipt' THEN m.quantity ELSE -m.quantity END), 0)
			FROM movements m WHERE m.batch_id = b.id)`).Scan(&bad)
	if err != nil || bad != 0 {
		t.Fatalf("партий с расхождением: %d (%v)", bad, err)
	}
	var expiredLeft int
	_ = db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM batches WHERE expires_at <= $1 AND remaining > 0`, testT0).Scan(&expiredLeft)
	if expiredLeft != 0 {
		t.Fatalf("просроченных партий с остатком: %d", expiredLeft)
	}
}

func TestLoadTwiceSameSnapshot(t *testing.T) {
	db := freshDB(t)
	loadSeed(t, db)
	first := snapshot(t, db)
	if _, err := db.Pool.Exec(context.Background(), "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	loadSeed(t, db)
	if second := snapshot(t, db); first != second {
		t.Fatal("две загрузки на пустую БД дали разные снимки")
	}
}

func TestLoadStopListNotEverything(t *testing.T) {
	db := freshDB(t)
	loadSeed(t, db)
	rows, err := db.Pool.Query(context.Background(),
		`SELECT product_id, sum(remaining) FROM batches WHERE remaining > 0 AND expires_at > $1 GROUP BY product_id`, testT0)
	if err != nil {
		t.Fatal(err)
	}
	avail := map[string]domain.Qty{}
	for rows.Next() {
		var id string
		var q int64
		if err := rows.Scan(&id, &q); err != nil {
			t.Fatal(err)
		}
		avail[id] = domain.Qty(q)
	}
	rows.Close()
	var dishes []domain.Dish
	for _, d := range Dishes() {
		dishes = append(dishes, domain.Dish{ID: d.ID, Name: d.Name, Recipe: d.Recipe})
	}
	if stopped := domain.StopList(dishes, avail); len(stopped) >= len(dishes) {
		t.Fatalf("в стоп-листе все блюда: %d из %d", len(stopped), len(dishes))
	}
}
