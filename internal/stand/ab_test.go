package stand

import (
	"context"
	"testing"
	"time"

	"umirhack/internal/api"
	"umirhack/internal/simulator"
)

// runAB гонит точку с профилем ab 48 ч времени стенда (48 часовых шагов ≈ ×600): заказы и поставки
// идут из сценария bench perishable3, гости слушаются списка (follow) или нет. Возвращает списания в копейках.
func runAB(t *testing.T, follow bool, seedValue int64) int64 {
	t.Helper()
	s, db := newStandProfile(t, ProfileAB)
	ctx := context.Background()
	sink := &placerSink{p: api.OrderPlacer{Orders: db, Now: s.Now}}
	s.Sim = NewSimulator(ctx, sink, s.Now, seedValue, nil)
	s.Sim.Hot, s.Sim.FollowHot, s.Sim.Uplift = snapHot{s}, follow, DefaultHotUplift
	s.UseProfile(ProfileAB)
	if err := s.Reset(ctx); err != nil { // как POST /api/demo/reset при SEED_PROFILE=ab
		t.Fatal(err)
	}
	start := s.Now()
	for i := 0; i < 48; i++ {
		from := s.Now()
		if _, err := s.Jump(ctx, 1); err != nil {
			t.Fatal(err)
		}
		if err := s.Sim.step(ctx, nil, from, s.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if sink.count == 0 {
		t.Fatal("заказов не было")
	}
	rep, err := db.WriteoffsReport(ctx, start, s.Now().Add(time.Hour), s.Loc)
	if err != nil {
		t.Fatal(err)
	}
	return rep.TotalMinor
}

// Профиль ab: на сцене разница списаний должна быть видна (bench: −10,8 %), при неизменном
// SIM_HOT_UPLIFT = 1,5. Порог — 8 % от точки без подсказок.
func TestABProfileDifferenceVisible(t *testing.T) {
	if testing.Short() {
		t.Skip("48 часов времени стенда на двух точках")
	}
	const seedValue = 2026
	with := runAB(t, true, seedValue)
	without := runAB(t, false, seedValue)
	t.Logf("списания за 48 ч: с подсказками %d коп., без %d коп., разница %.1f %%",
		with, without, 100*float64(with-without)/float64(without))
	if without == 0 {
		t.Fatal("без подсказок списаний нет — сравнивать нечего")
	}
	if float64(without-with) < 0.08*float64(without) {
		t.Fatalf("разница меньше 8 %% от точки без подсказок: %d против %d", with, without)
	}
}

// Профиль ab берёт данные сценария из bench, а не копию: набор продуктов и блюд совпадает.
func TestABWorldIsBenchScenario(t *testing.T) {
	var want simulator.World
	for _, sc := range simulator.Scenarios() {
		if sc.Name == ABScenario {
			want = sc.World
		}
	}
	got := ABWorld()
	if len(got.Dishes) != len(want.Dishes) || len(got.Products) != len(want.Products) || len(got.Dishes) == 0 {
		t.Fatalf("блюд %d/%d, продуктов %d/%d", len(got.Dishes), len(want.Dishes), len(got.Products), len(want.Products))
	}
	for i, d := range got.Dishes {
		if d.ID != want.Dishes[i].ID || d.PriorDaily != want.Dishes[i].PriorDaily || d.PriceMinor <= 0 {
			t.Fatalf("блюдо %d: %+v", i, d)
		}
	}
}

func TestSeedProfileReported(t *testing.T) {
	if (&Stand{}).SeedProfile() != "demo" || (&Stand{Profile: ProfileAB}).SeedProfile() != "ab" {
		t.Fatal("SeedProfile: пусто и demo — demo, ab — ab")
	}
}
