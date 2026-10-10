package store

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"umirhack/internal/auth"
)

func authDB(t *testing.T) *DB {
	t.Helper()
	db := testDB(t)
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestUsersCRUDAndUniqueLogin(t *testing.T) {
	db, ctx := authDB(t), context.Background()
	u, err := db.CreateUser(ctx, "ann", "hash", auth.Operator)
	if err != nil || u.ID == 0 {
		t.Fatalf("create: %v %+v", err, u)
	}
	if _, err := db.CreateUser(ctx, "ann", "hash", auth.Viewer); !errors.Is(err, auth.ErrExists) {
		t.Fatalf("duplicate login: got %v", err)
	}
	got, err := db.UserByLogin(ctx, "ann")
	if err != nil || got.Role != auth.Operator || got.PasswordHash != "hash" {
		t.Fatalf("by login: %v %+v", err, got)
	}
	if _, err := db.UserByLogin(ctx, "nobody"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("missing user: %v", err)
	}
	list, err := db.ListUsers(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", err, list)
	}
	if err := db.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteUser(ctx, u.ID); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestDeleteLastAdminForbiddenEvenConcurrently(t *testing.T) {
	db, ctx := authDB(t), context.Background()
	a1, _ := db.CreateUser(ctx, "a1", "h", auth.Admin)
	a2, _ := db.CreateUser(ctx, "a2", "h", auth.Admin)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []int64{a1.ID, a2.ID} {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = db.DeleteUser(ctx, id) }()
	}
	wg.Wait()
	ok, last := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, auth.ErrLastAdmin):
			last++
		default:
			t.Fatalf("unexpected: %v", err)
		}
	}
	if ok != 1 || last != 1 {
		t.Fatalf("ok=%d last=%d, want exactly one of each", ok, last)
	}
	if n, _ := db.CountUsers(ctx); n != 1 {
		t.Fatalf("users left = %d", n)
	}
}

func TestSessionsExpiryAndCascade(t *testing.T) {
	db, ctx := authDB(t), context.Background()
	u, _ := db.CreateUser(ctx, "ann", "h", auth.Viewer)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	h := auth.HashToken("tok")
	if err := db.CreateSession(ctx, h, u.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, err := db.SessionUser(ctx, h, now); err != nil || got.ID != u.ID || got.Role != auth.Viewer {
		t.Fatalf("live session: %v %+v", err, got)
	}
	// Ровно expires_at — уже недействительна.
	if _, err := db.SessionUser(ctx, h, now.Add(time.Hour)); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("expired session: %v", err)
	}
	if err := db.DeleteExpiredSessions(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&n)
	if n != 0 {
		t.Fatalf("expired session not removed: %d", n)
	}

	_ = db.CreateSession(ctx, h, u.ID, now.Add(time.Hour))
	if err := db.DeleteSession(ctx, h); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SessionUser(ctx, h, now); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("deleted session: %v", err)
	}
	_ = db.CreateSession(ctx, h, u.ID, now.Add(time.Hour))
	_ = db.DeleteUser(ctx, u.ID)
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&n)
	if n != 0 {
		t.Fatalf("sessions of deleted user remain: %d", n)
	}
}

func TestAPIKeysStoreOnlyHash(t *testing.T) {
	db, ctx := authDB(t), context.Background()
	h := auth.HashToken("fsk_secret")
	k, err := db.CreateAPIKey(ctx, "sim", h, auth.Operator)
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.APIKeyByHash(ctx, h)
	if err != nil || got.ID != k.ID || got.Role != auth.Operator {
		t.Fatalf("by hash: %v %+v", err, got)
	}
	var stored []byte
	_ = db.Pool.QueryRow(ctx, `SELECT key_hash FROM api_keys WHERE id=$1`, k.ID).Scan(&stored)
	if !bytes.Equal(stored, h) || bytes.Contains(stored, []byte("fsk_secret")) {
		t.Fatal("key_hash must be the SHA-256 of the key")
	}
	if _, err := db.CreateAPIKey(ctx, "dup", h, auth.Viewer); !errors.Is(err, auth.ErrExists) {
		t.Fatalf("duplicate hash: %v", err)
	}
	if err := db.DeleteAPIKey(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.APIKeyByHash(ctx, h); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("revoked key: %v", err)
	}
	if err := db.DeleteAPIKey(ctx, k.ID); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
}
