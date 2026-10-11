package audit

import (
	"context"
	"testing"
)

func TestRecordValidatesAndStores(t *testing.T) {
	m := &Mem{}
	ctx := context.Background()
	if err := Record(ctx, m, "", "x", nil); err == nil {
		t.Error("empty actor must be rejected")
	}
	if err := Record(ctx, nil, "a", "x", nil); err != nil {
		t.Errorf("nil sink is a no-op: %v", err)
	}
	if err := Record(ctx, m, "user:ann", "first", nil); err != nil {
		t.Fatal(err)
	}
	if err := Record(ctx, m, "user:ann", "second", map[string]any{"k": 1}); err != nil {
		t.Fatal(err)
	}
	got, _ := m.ListAudit(ctx, 1)
	if len(got) != 1 || got[0].Action != "second" || got[0].At.IsZero() {
		t.Fatalf("newest first with limit: %+v", got)
	}
	all, _ := m.ListAudit(ctx, 10)
	if len(all) != 2 || all[1].Details == nil {
		t.Fatalf("all: %+v", all)
	}
}

func TestRecordSurvivesCancelledContext(t *testing.T) {
	m := &Mem{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Record(ctx, m, "a", "x", nil); err != nil {
		t.Fatal(err)
	}
}
