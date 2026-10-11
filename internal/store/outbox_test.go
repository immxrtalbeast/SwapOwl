package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestOutboxEnqueueListAck(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, p := range []string{`{"n":1}`, `{"n":2}`, `{"n":3}`} {
		id, err := db.EnqueueOutbox(ctx, OutboxStoplist, json.RawMessage(p), t0)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	got, err := db.ListOutbox(ctx, 0, 0)
	if err != nil || len(got) != 3 || got[0].ID != ids[0] || got[2].ID != ids[2] ||
		got[0].Kind != OutboxStoplist || string(got[0].Payload) != `{"n": 1}` {
		t.Fatalf("list: %v %+v", err, got)
	}
	if got, _ = db.ListOutbox(ctx, ids[0], 1); len(got) != 1 || got[0].ID != ids[1] {
		t.Fatalf("after+limit: %+v", got)
	}

	// Неудача: attempts+1, last_error, сообщение остаётся в очереди.
	if err := db.AckOutbox(ctx, ids[0], false, "iiko down", t0); err != nil {
		t.Fatal(err)
	}
	if err := db.AckOutbox(ctx, ids[0], false, "iiko still down", t0); err != nil {
		t.Fatal(err)
	}
	got, _ = db.ListOutbox(ctx, 0, 0)
	if len(got) != 3 || got[0].Attempts != 2 || got[0].LastError == nil || *got[0].LastError != "iiko still down" {
		t.Fatalf("after failures: %+v", got)
	}

	// Успех убирает из выдачи; повторный ack (в том числе неудачный) ничего не меняет и не падает.
	for i := 0; i < 2; i++ {
		if err := db.AckOutbox(ctx, ids[0], true, "", t0); err != nil {
			t.Fatalf("ack ok #%d: %v", i, err)
		}
	}
	if err := db.AckOutbox(ctx, ids[0], false, "late", t0); err != nil {
		t.Fatal(err)
	}
	var attempts int
	var delivered bool
	if err := db.Pool.QueryRow(ctx, `SELECT attempts, delivered_at IS NOT NULL FROM outbox WHERE id=$1`, ids[0]).
		Scan(&attempts, &delivered); err != nil || attempts != 2 || !delivered {
		t.Fatalf("delivered row: %v attempts=%d delivered=%v", err, attempts, delivered)
	}
	if got, _ = db.ListOutbox(ctx, 0, 0); len(got) != 2 || got[0].ID != ids[1] {
		t.Fatalf("after ack: %+v", got)
	}

	if err := db.AckOutbox(ctx, 999999, true, "", t0); !errors.Is(err, ErrOutboxNotFound) {
		t.Fatalf("missing: %v", err)
	}
}
