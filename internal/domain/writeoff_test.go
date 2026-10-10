package domain

import (
	"math/rand"
	"reflect"
	"testing"
	"time"
)

func mb(id int64, p string, rem Qty, exp int) Batch {
	return Batch{ID: id, ProductID: p, Quantity: rem, Remaining: rem, ExpiresAt: t0.Add(time.Duration(exp) * time.Hour)}
}

func TestPlanWriteoffFEFO(t *testing.T) {
	batches := []Batch{mb(3, "a", 10, 48), mb(1, "a", 5, 24), mb(2, "a", 5, 24), mb(4, "b", 7, 1)}
	cases := []struct {
		name  string
		needs []Need
		plan  []Writeoff
		short []Shortage
	}{
		{"ближайший срок, равенство — меньший id", []Need{{"a", 7}}, []Writeoff{{1, "a", 5}, {2, "a", 2}}, nil},
		{"следующая партия после исчерпания", []Need{{"a", 15}}, []Writeoff{{1, "a", 5}, {2, "a", 5}, {3, "a", 5}}, nil},
		{"потребность суммируется", []Need{{"a", 3}, {"a", 4}}, []Writeoff{{1, "a", 5}, {2, "a", 2}}, nil},
		{"два продукта", []Need{{"b", 7}, {"a", 1}}, []Writeoff{{1, "a", 1}, {4, "b", 7}}, nil},
		{"нехватка — план пуст, все продукты", []Need{{"a", 21}, {"b", 8}, {"c", 1}}, nil, []Shortage{
			{"a", 21, 20, 1}, {"b", 8, 7, 1}, {"c", 1, 0, 1}}},
	}
	for _, c := range cases {
		plan, short := PlanWriteoff(c.needs, append([]Batch(nil), batches...))
		if !reflect.DeepEqual(plan, c.plan) || !reflect.DeepEqual(short, c.short) {
			t.Errorf("%s: plan=%v short=%v", c.name, plan, short)
		}
	}
}

func TestPlanWriteoffFEFOProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for iter := 0; iter < 2000; iter++ {
		var bs []Batch
		for i := 0; i < 1+rng.Intn(8); i++ {
			bs = append(bs, mb(int64(i+1), "p", Qty(1+rng.Intn(20)), rng.Intn(5)))
		}
		rng.Shuffle(len(bs), func(i, j int) { bs[i], bs[j] = bs[j], bs[i] })
		need := Qty(1 + rng.Intn(60))
		plan, short := PlanWriteoff([]Need{{"p", need}}, bs)
		if (plan == nil) == (short == nil) {
			t.Fatalf("plan xor short: %v %v", plan, short)
		}
		if plan == nil {
			continue
		}
		taken := map[int64]Qty{}
		var sum Qty
		for _, w := range plan {
			taken[w.BatchID] += w.Qty
			sum += w.Qty
		}
		if sum != need {
			t.Fatalf("sum %d != need %d", sum, need)
		}
		last := plan[len(plan)-1].BatchID
		var lb Batch
		for _, b := range bs {
			if b.ID == last {
				lb = b
			}
		}
		for _, b := range bs {
			earlier := b.ExpiresAt.Before(lb.ExpiresAt) || (b.ExpiresAt.Equal(lb.ExpiresAt) && b.ID < lb.ID)
			if earlier && b.Remaining-taken[b.ID] != 0 {
				t.Fatalf("batch %d earlier than last %d not exhausted", b.ID, last)
			}
		}
	}
}
