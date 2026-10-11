package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

// stockOrders принимает первые left заказов и отклоняет остальные; потокобезопасен.
type stockOrders struct {
	fakeOrders
	mu   sync.Mutex
	left int
	ids  map[string]bool
}

func (s *stockOrders) PlaceOrder(_ context.Context, in store.OrderInput, _ func() time.Time) (store.OrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ids == nil {
		s.ids = map[string]bool{}
	}
	s.ids[in.ID] = true
	if s.left > 0 {
		s.left--
		return store.OrderResponse{ID: in.ID, Status: store.OrderAccepted}, nil
	}
	return store.OrderResponse{ID: in.ID, Status: store.OrderRejected}, nil
}

type fakeSim struct{ running bool }

func (f *fakeSim) SetRunning(r bool) { f.running = r }
func (f *fakeSim) Running() bool     { return f.running }

func simRouter(o Orders, sim SimControl) http.Handler {
	return NewRouter(Deps{Now: func() time.Time { return standTime }, Demo: &fakeDemo{}, DemoToken: "tok", Orders: o, Sim: sim}).Handler()
}

func TestBurstCountsAcceptedAndRejected(t *testing.T) {
	so := &stockOrders{left: 4}
	rec := demoPost(simRouter(so, &fakeSim{}), "/api/demo/burst", `{"dish_id":"soup","orders":10}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if b := rec.Body.String(); !strings.Contains(b, `"accepted":4`) || !strings.Contains(b, `"rejected":6`) {
		t.Fatalf("body %s", b)
	}
	if len(so.ids) != 10 {
		t.Fatalf("want 10 distinct order ids, got %d", len(so.ids))
	}
	for id := range so.ids {
		if !uuidRe.MatchString(id) {
			t.Fatalf("bad uuid %q", id)
		}
	}
}

func TestBurstValidation(t *testing.T) {
	h := simRouter(&stockOrders{left: 1}, &fakeSim{})
	for body, code := range map[string]int{
		`{"dish_id":"soup","orders":0}`:  http.StatusBadRequest,
		`{"dish_id":"soup","orders":51}`: http.StatusBadRequest,
		`{"dish_id":"soup"}`:             http.StatusBadRequest,
		`{"orders":3}`:                   http.StatusBadRequest,
		`{"dish_id":"nope","orders":3}`:  http.StatusBadRequest,
		`{"dish_id":"soup","orders":50}`: http.StatusOK,
	} {
		if rec := demoPost(h, "/api/demo/burst", body, ""); rec.Code != code {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
}

func TestSimulatorToggle(t *testing.T) {
	sim := &fakeSim{}
	h := simRouter(&stockOrders{}, sim)
	if rec := demoPost(h, "/api/demo/simulator", `{"running":true}`, ""); rec.Code != http.StatusOK || !sim.running {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := demoPost(h, "/api/demo/simulator", `{"running":false}`, ""); rec.Code != http.StatusOK || sim.running {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := demoPost(h, "/api/demo/simulator", `{}`, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("%d", rec.Code)
	}
}

func TestSimRoutesAbsentWithoutDemoMode(t *testing.T) {
	h := NewRouter(Deps{Now: func() time.Time { return standTime }, Orders: &stockOrders{left: 1}, Sim: &fakeSim{}}).Handler()
	if rec := demoPost(h, "/api/demo/burst", `{"dish_id":"soup","orders":1}`, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("%d", rec.Code)
	}
}

func TestPlacerSumsNeeds(t *testing.T) {
	fo := &fakeOrders{resp: store.OrderResponse{Status: store.OrderAccepted}}
	p := OrderPlacer{Orders: fo, Now: func() time.Time { return standTime }}
	_, err := p.Place(context.Background(), oid, store.SourceAPI, []store.OrderItem{{DishID: "soup", Portions: 2}, {DishID: "sauce", Portions: 1}})
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Need{{ProductID: "cream", Qty: 50}, {ProductID: "salmon", Qty: 300}}
	if len(fo.in.Needs) != 2 || fo.in.Needs[0] != want[0] || fo.in.Needs[1] != want[1] {
		t.Fatalf("%+v", fo.in.Needs)
	}
	if _, err := p.Place(context.Background(), oid, store.SourceAPI, []store.OrderItem{{DishID: "x", Portions: 1}}); err == nil {
		t.Fatal("unknown dish must fail")
	}
}
