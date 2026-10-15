package store

import (
	"context"
	"errors"
	"testing"

	"umirhack/internal/auth"
)

func TestTelegramLinks(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	a, err := db.CreateUser(ctx, "alice", "h", auth.Operator)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateUser(ctx, "bob", "h", auth.Viewer); err != nil {
		t.Fatal(err)
	}

	if _, err := db.TelegramLogin(ctx, 1); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("нет привязки: %v", err)
	}
	if err := db.LinkTelegram(ctx, "ghost", 1); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("нет пользователя: %v", err)
	}
	if err := db.LinkTelegram(ctx, "alice", 1); err != nil {
		t.Fatal(err)
	}
	if err := db.LinkTelegram(ctx, "alice", 1); err != nil {
		t.Fatalf("повтор той же привязки: %v", err)
	}
	if err := db.LinkTelegram(ctx, "bob", 1); !errors.Is(err, auth.ErrExists) {
		t.Fatalf("чужой аккаунт: %v", err)
	}
	if err := db.LinkTelegram(ctx, "alice", 2); err != nil { // замена
		t.Fatal(err)
	}
	if _, err := db.TelegramLogin(ctx, 1); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("старая привязка должна исчезнуть: %v", err)
	}
	if l, err := db.TelegramLogin(ctx, 2); err != nil || l != "alice" {
		t.Fatalf("%q %v", l, err)
	}

	// Удаление пользователя снимает привязку каскадом.
	if err := db.DeleteUser(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.TelegramLogin(ctx, 2); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("каскад: %v", err)
	}
	if err := db.UnlinkTelegram(ctx, "bob"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("нечего снимать: %v", err)
	}
	if err := db.LinkTelegram(ctx, "bob", 3); err != nil {
		t.Fatal(err)
	}
	if err := db.UnlinkTelegram(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
}
