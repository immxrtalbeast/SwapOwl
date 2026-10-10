package store

import (
	"context"
	"os"
	"testing"
	"testing/fstest"
)

// Тесты работают с настоящим PostgreSQL: TEST_DATABASE_URL должен указывать на пустую
// одноразовую БД. Без него тесты пропускаются.
func testDB(t *testing.T) *DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := Open(ctx, url, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err := db.Pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMigrateAppliesAndIsIdempotent(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := db.Migrate(ctx); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	var n int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&n); err != nil || n < 1 {
		t.Fatalf("schema_migrations: n=%d err=%v", n, err)
	}
	var reg *string
	if err := db.Pool.QueryRow(ctx, "SELECT to_regclass('public.products')::text").Scan(&reg); err != nil || reg == nil {
		t.Fatalf("products table missing: %v", err)
	}
}

func TestMigrateRollsBackFailedMigration(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	fsys := fstest.MapFS{
		"m/0001_ok.sql":  {Data: []byte("CREATE TABLE a (id int)")},
		"m/0002_bad.sql": {Data: []byte("CREATE TABLE b (id int); SELECT broken_syntax FROM")},
	}
	if err := migrate(ctx, db, fsys, "m"); err == nil {
		t.Fatal("expected error")
	}
	var reg *string
	_ = db.Pool.QueryRow(ctx, "SELECT to_regclass('public.b')::text").Scan(&reg)
	if reg != nil {
		t.Error("table b from failed migration survived")
	}
	var n int
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&n)
	if n != 1 {
		t.Errorf("applied = %d, want 1", n)
	}
}

func TestOpenHidesPasswordOnBadURL(t *testing.T) {
	_, err := Open(context.Background(), "postgres://u:hunter2@host:notaport/db", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if s := err.Error(); len(s) == 0 || contains(s, "hunter2") {
		t.Errorf("error leaks password: %q", s)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
