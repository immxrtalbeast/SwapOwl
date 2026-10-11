package store

import (
	"context"
	"testing"

	"umirhack/internal/audit"
)

func TestAuditLogRoundTripNewestFirst(t *testing.T) {
	db, ctx := authDB(t), context.Background()
	for _, a := range []string{"login_ok", "user_created", "password_changed"} {
		if err := audit.Record(ctx, db, "user:ann", a, map[string]any{"n": 1, "who": "bob"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ListAudit(ctx, 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("list: %v %v", err, got)
	}
	if got[0].Action != "password_changed" || got[1].Action != "user_created" {
		t.Fatalf("order: %+v", got)
	}
	if got[0].Actor != "user:ann" || got[0].Details["who"] != "bob" || got[0].At.IsZero() {
		t.Fatalf("fields: %+v", got[0])
	}
}
