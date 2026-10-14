package invoice

import (
	"context"
	"errors"
	"testing"
	"time"
)

var catalog = []Product{{"milk", "Молоко", "ml"}, {"cream", "Сливки", "ml"}, {"parmesan", "Пармезан", "g"}, {"tomato", "Томаты", "g"}}

func TestCleanDropsUnknownIDsAndEmptyLines(t *testing.T) {
	bogus, milk := "unicorn", "milk"
	res, err := Clean(Result{Lines: []Line{
		{RawName: "Молоко", ProductID: &milk, Confidence: "high"},
		{RawName: "Единорог", ProductID: &bogus, Confidence: "high"},
		{RawName: "  "},
		{RawName: "Соль", Confidence: "maybe"},
	}}, catalog)
	if err != nil || len(res.Lines) != 3 {
		t.Fatalf("%v %+v", err, res)
	}
	if res.Lines[0].Confidence != "high" || res.Lines[1].ProductID != nil || res.Lines[1].Confidence != "low" || res.Lines[2].Confidence != "low" {
		t.Fatalf("lines %+v", res.Lines)
	}
	if _, err := Clean(Result{}, catalog); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("empty: %v", err)
	}
}

func TestDemoMatchesStandCatalog(t *testing.T) {
	res, err := Demo{}.Recognize(context.Background(), Image{}, catalog)
	if err != nil || !res.Demo || len(res.Lines) != 5 {
		t.Fatalf("%v %+v", err, res)
	}
	matched := 0
	for _, l := range res.Lines {
		if l.ProductID != nil {
			matched++
		}
	}
	if matched != 4 {
		t.Fatalf("matched %d, want 4 of 5 (йогурт — не из справочника)", matched)
	}
}

func TestLimiterSlidingHour(t *testing.T) {
	l := NewLimiter(2)
	t0 := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	if ok, _ := l.Allow(t0); !ok {
		t.Fatal("1")
	}
	if ok, _ := l.Allow(t0.Add(10 * time.Minute)); !ok {
		t.Fatal("2")
	}
	ok, retry := l.Allow(t0.Add(20 * time.Minute))
	if ok || retry != 40*time.Minute {
		t.Fatalf("3: %v %v", ok, retry)
	}
	if ok, _ := l.Allow(t0.Add(61 * time.Minute)); !ok {
		t.Fatal("после часа первая попытка освободила место")
	}
}
