package a

import (
	"math/rand"
	"reflect"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/hotoffers"
)

var t0 = time.Date(2025, 10, 10, 11, 0, 0, 0, time.UTC)

func sched() domain.Schedule {
	return domain.Schedule{Location: time.UTC, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour}
}

func dish(id string, items ...any) domain.Dish {
	d := domain.Dish{ID: id}
	for i := 0; i < len(items); i += 2 {
		d.Recipe = append(d.Recipe, domain.RecipeItem{ProductID: items[i].(string), Quantity: domain.Qty(items[i+1].(int))})
	}
	return d
}

func base() hotoffers.Input {
	return hotoffers.Input{
		Now:      t0,
		Schedule: sched(),
		Config:   hotoffers.Config{TopN: 3, WMin: 1},
		Price:    map[string]int64{"x": 1000, "y": 1000},
		Stopped:  map[string]bool{},
	}
}

func TestWorkingHours(t *testing.T) {
	// 11:00 → 12:00 следующего дня: 12 ч в первый день + 2 ч во второй.
	got := workingHours(t0, t0.Add(25*time.Hour), sched())
	if got != 14 {
		t.Fatalf("got %v want 14", got)
	}
}

func TestTable(t *testing.T) {
	in := base()
	in.Dishes = []domain.Dish{dish("d1", "x", 100), dish("d2", "x", 100, "y", 50), dish("d3", "y", 10), dish("stop", "x", 100)}
	in.Stopped["stop"] = true
	in.Batches = []domain.Batch{
		{ID: 1, ProductID: "x", Remaining: 1200, ExpiresAt: t0.Add(6 * time.Hour)},
		{ID: 2, ProductID: "x", Remaining: 1000, ExpiresAt: t0.Add(30 * time.Hour)},
		{ID: 3, ProductID: "y", Remaining: 0, ExpiresAt: t0.Add(2 * time.Hour)},
		{ID: 4, ProductID: "y", Remaining: 5, ExpiresAt: t0.Add(-time.Hour)},
	}
	offers, excl := Rank(in)
	// need_x = 1.0*1200/6 = 200; batch 2: 2200/(12+13+... ) меньше.
	if len(offers) != 2 || offers[0].DishID != "d1" && offers[0].DishID != "d2" {
		t.Fatalf("offers: %+v", offers)
	}
	for _, o := range offers {
		if o.DishID == "stop" || o.DishID == "d3" {
			t.Fatalf("unexpected %s", o.DishID)
		}
		if len(o.Reasons) == 0 || o.Reasons[0].BatchID != 1 {
			t.Fatalf("reasons: %+v", o.Reasons)
		}
	}
	// share d1 = 100/300, d2 = 100/300 → равны, тай-брейк по id.
	if offers[0].DishID != "d1" || offers[1].DishID != "d2" {
		t.Fatalf("tie-break: %+v", offers)
	}
	if len(excl) != 1 || excl[0].DishID != "stop" {
		t.Fatalf("excluded: %+v", excl)
	}
}

func TestDoomedSkipped(t *testing.T) {
	in := base()
	in.Dishes = []domain.Dish{dish("d1", "x", 100)}
	in.Batches = []domain.Batch{{ID: 1, ProductID: "x", Remaining: 100, ExpiresAt: t0.Add(30 * time.Minute)}}
	if o, _ := Rank(in); len(o) != 0 {
		t.Fatalf("doomed batch must not produce offers: %+v", o)
	}
}

func TestTopN(t *testing.T) {
	in := base()
	in.Config.TopN = 1
	in.Dishes = []domain.Dish{dish("d1", "x", 100), dish("d2", "y", 100)}
	in.Batches = []domain.Batch{
		{ID: 1, ProductID: "x", Remaining: 100, ExpiresAt: t0.Add(5 * time.Hour)},
		{ID: 2, ProductID: "y", Remaining: 900, ExpiresAt: t0.Add(5 * time.Hour)},
	}
	o, _ := Rank(in)
	if len(o) != 1 || o[0].DishID != "d2" {
		t.Fatalf("%+v", o)
	}
}

func randInput(r *rand.Rand) hotoffers.Input {
	in := base()
	prods := []string{"p0", "p1", "p2", "p3"}
	in.Price = map[string]int64{}
	for _, p := range prods {
		in.Price[p] = int64(r.Intn(5000))
	}
	for i := 0; i < 2+r.Intn(6); i++ {
		d := domain.Dish{ID: string(rune('a' + i))}
		for _, p := range prods {
			if r.Intn(2) == 0 {
				d.Recipe = append(d.Recipe, domain.RecipeItem{ProductID: p, Quantity: domain.Qty(1 + r.Intn(300))})
			}
		}
		in.Dishes = append(in.Dishes, d)
		if r.Intn(4) == 0 {
			in.Stopped[d.ID] = true
		}
	}
	for i := 0; i < r.Intn(10); i++ {
		in.Batches = append(in.Batches, domain.Batch{
			ID: int64(i + 1), ProductID: prods[r.Intn(len(prods))],
			Remaining: domain.Qty(r.Intn(3000)),
			ExpiresAt: t0.Add(time.Duration(r.Intn(100*60)-300) * time.Minute / 3),
		})
	}
	in.Config.TopN = 1 + r.Intn(6)
	return in
}

func TestInvariantValidAndOrderIndependent(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for n := 0; n < 500; n++ {
		in := randInput(r)
		offers, _ := Rank(in)
		batches := map[int64]domain.Batch{}
		for _, b := range in.Batches {
			batches[b.ID] = b
		}
		for _, o := range offers {
			if in.Stopped[o.DishID] || len(o.Reasons) == 0 {
				t.Fatalf("bad offer %+v", o)
			}
			for _, rs := range o.Reasons {
				b, ok := batches[rs.BatchID]
				if !ok || b.Remaining <= 0 || !in.Now.Before(b.ExpiresAt) {
					t.Fatalf("bad reason %+v", rs)
				}
			}
		}
		sh := in
		sh.Batches = append([]domain.Batch(nil), in.Batches...)
		sh.Dishes = append([]domain.Dish(nil), in.Dishes...)
		r.Shuffle(len(sh.Batches), func(i, j int) { sh.Batches[i], sh.Batches[j] = sh.Batches[j], sh.Batches[i] })
		r.Shuffle(len(sh.Dishes), func(i, j int) { sh.Dishes[i], sh.Dishes[j] = sh.Dishes[j], sh.Dishes[i] })
		again, _ := Rank(sh)
		if !reflect.DeepEqual(offers, again) {
			t.Fatalf("order dependent:\n%+v\n%+v", offers, again)
		}
	}
}

func place(offers []hotoffers.Offer, id string) int {
	for i, o := range offers {
		if o.DishID == id {
			return i
		}
	}
	return len(offers)
}

func TestMonotoneByExpiry(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for n := 0; n < 1000; n++ {
		in := randInput(r)
		in.Config.TopN = 100
		if len(in.Batches) == 0 {
			continue
		}
		i := r.Intn(len(in.Batches))
		b := in.Batches[i]
		w := workingHours(in.Now, b.ExpiresAt, in.Schedule)
		if b.Remaining <= 0 || w < in.Config.WMin+0.5 {
			continue
		}
		before, _ := Rank(in)
		moved := in
		moved.Batches = append([]domain.Batch(nil), in.Batches...)
		// Сдвиг ближе к now, но остаёмся выше порога «обречённой».
		moved.Batches[i].ExpiresAt = b.ExpiresAt.Add(-time.Duration(r.Int63n(int64(b.ExpiresAt.Sub(in.Now)))) / 2)
		if workingHours(in.Now, moved.Batches[i].ExpiresAt, in.Schedule) < in.Config.WMin {
			continue
		}
		after, _ := Rank(moved)
		uses := func(d domain.Dish) bool {
			for _, it := range d.Recipe {
				if it.ProductID == b.ProductID {
					return true
				}
			}
			return false
		}
		// Блюда с тем же продуктом могут обгонять друг друга (у кого доля больше,
		// тот выигрывает сильнее), поэтому место считаем среди блюд без этого продукта.
		ahead := func(offers []hotoffers.Offer, id string) int {
			n := 0
			for _, o := range offers {
				if o.DishID == id {
					return n
				}
				for _, d := range in.Dishes {
					if d.ID == o.DishID && !uses(d) {
						n++
					}
				}
			}
			return n
		}
		for _, d := range in.Dishes {
			if !uses(d) || in.Stopped[d.ID] || place(before, d.ID) == len(before) {
				continue
			}
			if place(after, d.ID) == len(after) || ahead(after, d.ID) > ahead(before, d.ID) {
				t.Fatalf("dish %s dropped", d.ID)
			}
		}
	}
}
