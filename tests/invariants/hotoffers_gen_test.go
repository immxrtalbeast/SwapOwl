package invariants

import (
	"fmt"
	"math/rand"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/hotoffers"
	ha "umirhack/internal/hotoffers/a"
	hb "umirhack/internal/hotoffers/b"
)

var algos = map[string]hotoffers.Ranker{"a": ha.Rank, "b": hb.Rank}

var moscow = mustLoc("Europe/Moscow")

func mustLoc(n string) *time.Location {
	l, err := time.LoadLocation(n)
	if err != nil {
		panic(err)
	}
	return l
}

// randomInput строит случайный снимок: партии только доступные (контракт Input).
func randomInput(rng *rand.Rand) hotoffers.Input {
	now := time.Date(2026, 10, 9, 8+rng.Intn(14), rng.Intn(60), 0, 0, moscow)
	nProd := 2 + rng.Intn(5)
	in := hotoffers.Input{
		Now:      now,
		Stopped:  map[string]bool{},
		Price:    map[string]int64{},
		RateHour: map[string]float64{},
		Schedule: domain.Schedule{Location: moscow, OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour},
		Config:   hotoffers.Config{TopN: 3, WMin: 0.5, CoverHours: 12, MaxIter: 200},
	}
	for p := 0; p < nProd; p++ {
		id := fmt.Sprintf("p%d", p)
		in.Price[id] = int64(1000 + rng.Intn(50000))
		in.RateHour[id] = float64(rng.Intn(400))
		for b := 0; b < 1+rng.Intn(3); b++ {
			q := domain.Qty(100 + rng.Intn(5000))
			in.Batches = append(in.Batches, domain.Batch{
				ID: int64(len(in.Batches) + 1), ProductID: id, Quantity: q, Remaining: 1 + domain.Qty(rng.Int63n(int64(q))),
				ReceivedAt: now.Add(-time.Duration(rng.Intn(48)) * time.Hour),
				ExpiresAt:  now.Add(time.Duration(2+rng.Intn(150)) * time.Hour),
			})
		}
	}
	for d := 0; d < 2+rng.Intn(5); d++ {
		dish := domain.Dish{ID: fmt.Sprintf("d%d", d), Name: fmt.Sprintf("d%d", d)}
		for _, k := range rng.Perm(nProd)[:1+rng.Intn(min(3, nProd))] {
			dish.Recipe = append(dish.Recipe, domain.RecipeItem{ProductID: fmt.Sprintf("p%d", k), Quantity: domain.Qty(10 + rng.Intn(300))})
		}
		in.Dishes = append(in.Dishes, dish)
		if rng.Intn(4) == 0 {
			in.Stopped[dish.ID] = true
		}
	}
	return in
}

func shuffled(rng *rand.Rand, in hotoffers.Input) hotoffers.Input {
	out := in
	out.Batches = append([]domain.Batch(nil), in.Batches...)
	rng.Shuffle(len(out.Batches), func(i, j int) { out.Batches[i], out.Batches[j] = out.Batches[j], out.Batches[i] })
	out.Dishes = make([]domain.Dish, len(in.Dishes))
	for i, d := range in.Dishes {
		d.Recipe = append([]domain.RecipeItem(nil), d.Recipe...)
		rng.Shuffle(len(d.Recipe), func(a, b int) { d.Recipe[a], d.Recipe[b] = d.Recipe[b], d.Recipe[a] })
		out.Dishes[i] = d
	}
	rng.Shuffle(len(out.Dishes), func(i, j int) { out.Dishes[i], out.Dishes[j] = out.Dishes[j], out.Dishes[i] })
	return out
}

func cloneInput(in hotoffers.Input) hotoffers.Input {
	out := in
	out.Batches = append([]domain.Batch(nil), in.Batches...)
	return out
}
