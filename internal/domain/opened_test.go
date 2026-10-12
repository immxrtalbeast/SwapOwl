package domain

import (
	"errors"
	"testing"
	"time"
)

func TestPlanOpen(t *testing.T) {
	now := time.Date(2026, 10, 12, 14, 0, 0, 0, time.UTC)
	batch := func(rem Qty, exp time.Duration) Batch {
		return Batch{ID: 1, ProductID: "milk", Quantity: 5000, Remaining: rem, ReceivedAt: now.Add(-time.Hour), ExpiresAt: now.Add(exp)}
	}
	q := func(v Qty) *Qty { return &v }
	milk := OpenRules{AfterOpenHours: 48, PackSize: 1000}
	tests := []struct {
		name    string
		b       Batch
		rules   OpenRules
		qty     *Qty
		want    OpenPlan
		wantErr error
	}{
		{"молоко 10 суток → 48 ч, одна упаковка", batch(5000, 240*time.Hour), milk, nil, OpenPlan{1000, now.Add(48 * time.Hour)}, nil},
		{"явное qty", batch(5000, 240*time.Hour), milk, q(2500), OpenPlan{2500, now.Add(48 * time.Hour)}, nil},
		{"остаток меньше упаковки — берём остаток", batch(400, 240*time.Hour), milk, nil, OpenPlan{400, now.Add(48 * time.Hour)}, nil},
		{"упаковка 1 — вся партия", batch(5000, 240*time.Hour), OpenRules{AfterOpenHours: 72, PackSize: 1}, nil, OpenPlan{5000, now.Add(72 * time.Hour)}, nil},
		{"срок партии короче срока после вскрытия — срок не удлиняется", batch(5000, 10*time.Hour), milk, nil, OpenPlan{1000, now.Add(10 * time.Hour)}, nil},
		{"ровно на границе", batch(5000, 48*time.Hour), milk, nil, OpenPlan{1000, now.Add(48 * time.Hour)}, nil},
		{"вся партия по qty", batch(5000, 240*time.Hour), milk, q(5000), OpenPlan{5000, now.Add(48 * time.Hour)}, nil},
		{"qty больше остатка", batch(5000, 240*time.Hour), milk, q(5001), OpenPlan{}, ErrOpenQty},
		{"qty ноль", batch(5000, 240*time.Hour), milk, q(0), OpenPlan{}, ErrOpenQty},
		{"qty отрицательное", batch(5000, 240*time.Hour), milk, q(-1), OpenPlan{}, ErrOpenQty},
		{"нет срока после вскрытия", batch(5000, 240*time.Hour), OpenRules{}, nil, OpenPlan{}, ErrOpenNoShelf},
		{"просрочена: now == expires_at", batch(5000, 0), milk, nil, OpenPlan{}, ErrOpenClosed},
		{"просрочена", batch(5000, -time.Minute), milk, nil, OpenPlan{}, ErrOpenClosed},
		{"пуста", batch(0, time.Hour), milk, nil, OpenPlan{}, ErrOpenClosed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PlanOpen(tc.b, tc.rules, tc.qty, now)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err == nil && (got.Qty != tc.want.Qty || !got.ChildExpiresAt.Equal(tc.want.ChildExpiresAt)) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
