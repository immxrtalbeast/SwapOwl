package b

import (
	"math/rand"
	"reflect"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/hotoffers"
)

var loc = time.UTC

func sched() domain.Schedule {
	return domain.Schedule{Location: loc, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour}
}

func cfg() hotoffers.Config {
	return hotoffers.Config{TopN: 3, WMin: 0.5, CoverHours: 12, MaxIter: 200}
}

var now = time.Date(2026, 10, 10, 11, 0, 0, 0, loc)

func dish(id string, items ...interface{}) domain.Dish {
	d := domain.Dish{ID: id}
	for i := 0; i < len(items); i += 2 {
		d.Recipe = append(d.Recipe, domain.RecipeItem{ProductID: items[i].(string), Quantity: domain.Qty(items[i+1].(int))})
	}
	return d
}

func TestWorkingHours(t *testing.T) {
	s := sched()
	cases := []struct {
		from, to time.Time
		want     float64
	}{
		{now, now.Add(2 * time.Hour), 2},
		{now, time.Date(2026, 10, 10, 23, 30, 0, 0, loc), 12},
		{now, time.Date(2026, 10, 11, 11, 0, 0, 0, loc), 13},
		{time.Date(2026, 10, 10, 2, 0, 0, 0, loc), time.Date(2026, 10, 10, 9, 0, 0, 0, loc), 0},
		{now, now, 0},
	}
	for i, c := range cases {
		if got := workingHours(c.from, c.to, s); got != c.want {
			t.Errorf("case %d: got %v want %v", i, got, c.want)
		}
	}
}

func TestRankTable(t *testing.T) {
	soon := now.Add(4 * time.Hour)
	later := now.Add(24 * time.Hour)
	in := hotoffers.Input{
		Now: now,
		Batches: []domain.Batch{
			{ID: 1, ProductID: "milk", Remaining: 1000, ExpiresAt: soon},
			{ID: 2, ProductID: "flour", Remaining: 5000, ExpiresAt: later},
		},
		Dishes: []domain.Dish{
			dish("latte", "milk", 200),
			dish("pancake", "milk", 100, "flour", 100),
			dish("bread", "flour", 300),
		},
		Price:    map[string]int64{"milk": 100, "flour": 10},
		RateHour: map[string]float64{"milk": 10, "flour": 0},
		Schedule: sched(),
		Config:   cfg(),
	}
	offers, excluded := Rank(in)
	if len(excluded) != 0 {
		t.Fatalf("unexpected excluded: %+v", excluded)
	}
	if len(offers) == 0 || offers[0].DishID != "latte" {
		t.Fatalf("want latte first, got %+v", offers)
	}
	for _, o := range offers {
		if len(o.Reasons) == 0 || o.TargetPortions <= 0 {
			t.Errorf("bad offer %+v", o)
		}
	}
}

func TestSafeguardExcludesScarceNonRisky(t *testing.T) {
	in := hotoffers.Input{
		Now: now,
		Batches: []domain.Batch{
			{ID: 1, ProductID: "milk", Remaining: 1000, ExpiresAt: now.Add(4 * time.Hour)},
			{ID: 2, ProductID: "saffron", Remaining: 10, ExpiresAt: now.Add(48 * time.Hour)},
		},
		Dishes: []domain.Dish{
			dish("latte", "milk", 100),
			dish("risotto", "milk", 100, "saffron", 1),
		},
		Price:    map[string]int64{"milk": 100, "saffron": 5000},
		RateHour: map[string]float64{"milk": 10, "saffron": 5},
		Schedule: sched(),
		Config:   cfg(),
	}
	offers, excluded := Rank(in)
	if len(excluded) != 1 || excluded[0].DishID != "risotto" || excluded[0].ProductID != "saffron" || excluded[0].Why == "" {
		t.Fatalf("excluded = %+v", excluded)
	}
	for _, o := range offers {
		if o.DishID == "risotto" {
			t.Fatal("excluded dish in offers")
		}
	}
}

func TestStoppedAndDoomedSkipped(t *testing.T) {
	in := hotoffers.Input{
		Now: now,
		Batches: []domain.Batch{
			{ID: 1, ProductID: "milk", Remaining: 1000, ExpiresAt: now.Add(4 * time.Hour)},
			{ID: 2, ProductID: "fish", Remaining: 1000, ExpiresAt: now.Add(10 * time.Minute)},
		},
		Dishes:   []domain.Dish{dish("latte", "milk", 100), dish("sushi", "fish", 100)},
		Stopped:  map[string]bool{"latte": true},
		Price:    map[string]int64{"milk": 100, "fish": 100},
		RateHour: map[string]float64{"milk": 1, "fish": 1},
		Schedule: sched(),
		Config:   cfg(),
	}
	offers, _ := Rank(in)
	if len(offers) != 0 {
		t.Fatalf("want none (stopped / doomed), got %+v", offers)
	}
}

func randInput(r *rand.Rand) hotoffers.Input {
	products := []string{"p1", "p2", "p3", "p4"}
	in := hotoffers.Input{
		Now:      now,
		Stopped:  map[string]bool{},
		Price:    map[string]int64{},
		RateHour: map[string]float64{},
		Schedule: sched(),
		Config:   cfg(),
	}
	for _, p := range products {
		in.Price[p] = int64(r.Intn(500))
		in.RateHour[p] = float64(r.Intn(40)) / 4
	}
	for i := 0; i < r.Intn(9); i++ {
		in.Batches = append(in.Batches, domain.Batch{
			ID: int64(i + 1), ProductID: products[r.Intn(4)],
			Remaining: domain.Qty(1 + r.Intn(3000)),
			ExpiresAt: now.Add(time.Duration(30+r.Intn(60*24*3)) * time.Minute),
		})
	}
	for i := 0; i < 1+r.Intn(6); i++ {
		d := domain.Dish{ID: string(rune('a' + i))}
		for _, j := range r.Perm(4)[:1+r.Intn(3)] {
			d.Recipe = append(d.Recipe, domain.RecipeItem{ProductID: products[j], Quantity: domain.Qty(1 + r.Intn(300))})
		}
		in.Dishes = append(in.Dishes, d)
		if r.Intn(5) == 0 {
			in.Stopped[d.ID] = true
		}
	}
	return in
}

// Инвариант 10: допустимость и детерминизм.
func TestPropertyAdmissibleAndDeterministic(t *testing.T) {
	r := rand.New(rand.NewSource(10))
	for n := 0; n < 2000; n++ {
		in := randInput(r)
		offers, excluded := Rank(in)
		batches := map[int64]domain.Batch{}
		for _, b := range in.Batches {
			batches[b.ID] = b
		}
		if len(offers) > in.Config.TopN {
			t.Fatalf("too many offers")
		}
		for _, o := range offers {
			if in.Stopped[o.DishID] {
				t.Fatalf("stopped dish %s offered", o.DishID)
			}
			if len(o.Reasons) == 0 {
				t.Fatalf("no reasons")
			}
			for _, rs := range o.Reasons {
				b, ok := batches[rs.BatchID]
				if !ok || b.Remaining <= 0 || !in.Now.Before(b.ExpiresAt) || b.ProductID != rs.ProductID {
					t.Fatalf("bad reason %+v", rs)
				}
			}
		}
		// перемешанные срезы
		sh := in
		sh.Batches = append([]domain.Batch(nil), in.Batches...)
		sh.Dishes = append([]domain.Dish(nil), in.Dishes...)
		r.Shuffle(len(sh.Batches), func(i, j int) { sh.Batches[i], sh.Batches[j] = sh.Batches[j], sh.Batches[i] })
		r.Shuffle(len(sh.Dishes), func(i, j int) { sh.Dishes[i], sh.Dishes[j] = sh.Dishes[j], sh.Dishes[i] })
		o2, e2 := Rank(sh)
		if !reflect.DeepEqual(offers, o2) || !reflect.DeepEqual(excluded, e2) {
			t.Fatalf("non-deterministic:\n%+v\n%+v", offers, o2)
		}
	}
}

// firstUserPlace — число блюд без продукта впереди первого блюда с этим продуктом;
// -1, если блюд с продуктом в списке нет. Внутри группы блюд с продуктом порядок
// законно меняется: они делят один рисковый остаток, и жадный цикл может отдать
// его целиком блюду с большей нормой.
func firstUserPlace(offers []hotoffers.Offer, uses map[string]bool) int {
	n := 0
	for _, o := range offers {
		if uses[o.DishID] {
			return n
		}
		n++
	}
	return -1
}

// Инвариант 11: сдвиг срока ближе к now не ухудшает место блюда с этим продуктом.
func TestPropertyMonotoneByExpiry(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	checked := 0
	for n := 0; n < 5000; n++ {
		in := randInput(r)
		in.Config.TopN = 100
		if len(in.Batches) == 0 {
			continue
		}
		k := r.Intn(len(in.Batches))
		b := in.Batches[k]
		w := workingHours(in.Now, b.ExpiresAt, in.Schedule)
		if w < in.Config.WMin {
			continue
		}
		before, _ := Rank(in)
		moved := in
		moved.Batches = append([]domain.Batch(nil), in.Batches...)
		nb := b
		nb.ExpiresAt = in.Now.Add(time.Duration(float64(b.ExpiresAt.Sub(in.Now)) * (0.3 + 0.7*r.Float64())))
		if workingHours(in.Now, nb.ExpiresAt, in.Schedule) < in.Config.WMin {
			continue
		}
		moved.Batches[k] = nb
		after, _ := Rank(moved)
		uses := map[string]bool{}
		for _, d := range in.Dishes {
			for _, it := range d.Recipe {
				if it.ProductID == b.ProductID {
					uses[d.ID] = true
				}
			}
		}
		pb := firstUserPlace(before, uses)
		if pb < 0 {
			continue // блюд с продуктом не было в списке — ограничения нет
		}
		checked++
		if pa := firstUserPlace(after, uses); pa < 0 || pa > pb {
			t.Fatalf("product %s dishes worsened %d -> %d\nin=%+v\nbatch %+v -> %v", b.ProductID, pb, pa, in, b, nb.ExpiresAt)
		}
	}
	if checked == 0 {
		t.Fatal("property never exercised")
	}
}

// Партия со сроком через 300 рабочих часов вне горизонта: рекомендации нет.
func TestHorizonExcludesFarBatch(t *testing.T) {
	c := cfg()
	c.HorizonHours = 24
	in := hotoffers.Input{
		Now:      now,
		Batches:  []domain.Batch{{ID: 1, ProductID: "potato", Remaining: 5600, ExpiresAt: now.Add(300 / 13 * 24 * time.Hour)}},
		Dishes:   []domain.Dish{dish("steak", "potato", 100)},
		Price:    map[string]int64{"potato": 100},
		RateHour: map[string]float64{"potato": 1},
		Schedule: sched(),
		Config:   c,
	}
	if w := workingHours(in.Now, in.Batches[0].ExpiresAt, in.Schedule); w < 290 || w > 320 {
		t.Fatalf("setup: W=%v", w)
	}
	if offers, _ := Rank(in); len(offers) != 0 {
		t.Fatalf("far batch must not produce offers: %+v", offers)
	}
	in.Config.HorizonHours = 0
	if offers, _ := Rank(in); len(offers) == 0 {
		t.Fatal("without horizon the offer must exist")
	}
}

// К3, 9 баллов: суммарная цель по блюдам с общим рисковым продуктом не превышает того, что под угрозой.
// Под риском три партии салата: в причине — весь риск продукта и число партий (FS-153), а не одна партия,
// иначе карточка «43 порции при 1438 г» выглядит как нарушение.
func TestSharedRiskTargetsWithinProductRisk(t *testing.T) {
	c := cfg()
	in := hotoffers.Input{
		Now: now,
		Batches: []domain.Batch{
			{ID: 1, ProductID: "lettuce", Remaining: 1500, ExpiresAt: now.Add(6 * time.Hour)},
			{ID: 2, ProductID: "lettuce", Remaining: 1500, ExpiresAt: now.Add(8 * time.Hour)},
			{ID: 3, ProductID: "lettuce", Remaining: 1500, ExpiresAt: now.Add(10 * time.Hour)},
			{ID: 4, ProductID: "bun", Remaining: 500, ExpiresAt: now.Add(200 * time.Hour)},
		},
		Dishes:   []domain.Dish{dish("caesar", "lettuce", 80), dish("burger", "lettuce", 20, "bun", 1), dish("salad", "lettuce", 40)},
		Price:    map[string]int64{"lettuce": 400, "bun": 1500},
		RateHour: map[string]float64{"lettuce": 10, "bun": 50}, // булочки расходятся сами: цель задаёт только салат
		Schedule: sched(),
		Config:   c,
	}
	offers, _ := Rank(in)
	if len(offers) == 0 {
		t.Fatal("нет предложений")
	}
	norm := map[string]domain.Qty{"caesar": 80, "burger": 20, "salad": 40}
	var used, risk domain.Qty
	for _, o := range offers {
		used += domain.Qty(o.TargetPortions) * norm[o.DishID]
		for _, r := range o.Reasons {
			if r.ProductID != "lettuce" {
				continue
			}
			if r.Batches != 3 || r.BatchID != 1 {
				t.Fatalf("%s: в причине должны быть 3 партии и ближайшая №1: %+v", o.DishID, r)
			}
			if risk != 0 && r.ProductAtRisk != risk {
				t.Fatalf("риск продукта должен быть одним числом у всех блюд: %d и %d", risk, r.ProductAtRisk)
			}
			if r.AtRisk > 1500 {
				t.Fatalf("at_risk — риск одной партии (≤ 1500 г), а не продукта: %d", r.AtRisk)
			}
			risk = r.ProductAtRisk
		}
	}
	// Последняя выбранная порция может взять часть нормы сверх остатка риска.
	if used > risk+80 {
		t.Fatalf("суммарная цель %d г салата больше, чем под угрозой %d г", used, risk)
	}
}
