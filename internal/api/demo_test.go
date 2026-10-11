package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"umirhack/internal/clock"
)

type fakeDemo struct {
	mu      sync.Mutex
	speeds  []float64
	jumps   []float64
	resets  int
	failure error
}

func (d *fakeDemo) SetSpeed(_ context.Context, s float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.speeds = append(d.speeds, s)
	return d.failure
}
func (d *fakeDemo) Jump(_ context.Context, h float64) (time.Time, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if h <= 0 || h > 720 {
		return time.Time{}, clock.ErrInvalidJump
	}
	d.jumps = append(d.jumps, h)
	return standTime, d.failure
}
func (d *fakeDemo) Reset(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.resets++
	return d.failure
}
func (d *fakeDemo) Now() time.Time { return standTime }
func (d *fakeDemo) Speed() float64 { return 1 }

func demoPost(h http.Handler, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func demoRouter(d Demo) http.Handler {
	return NewRouter(Deps{Now: func() time.Time { return standTime }, Demo: d, DemoToken: "tok"}).Handler()
}

func TestDemoRequiresToken(t *testing.T) {
	d := &fakeDemo{}
	h := demoRouter(d)
	for _, p := range []string{"/api/demo/clock/speed", "/api/demo/clock/jump", "/api/demo/reset"} {
		for _, tok := range []string{"", "wrong"} {
			if rec := demoPost(h, p, `{"speed":1,"hours":1}`, tok); rec.Code != http.StatusUnauthorized {
				t.Errorf("%s token %q: %d", p, tok, rec.Code)
			}
		}
	}
	if len(d.speeds)+len(d.jumps)+d.resets != 0 {
		t.Fatal("handlers ran without a token")
	}
}

func TestDemoEmptyTokenOpensNothing(t *testing.T) {
	h := NewRouter(Deps{Demo: &fakeDemo{}}).Handler()
	if rec := demoPost(h, "/api/demo/reset", `{}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("code %d", rec.Code)
	}
}

func TestDemoDisabledIs404(t *testing.T) {
	h := demoRouter(nil)
	for _, p := range []string{"/api/demo/clock/speed", "/api/demo/clock/jump", "/api/demo/reset", "/api/demo/simulator"} {
		if rec := demoPost(h, p, `{}`, "tok"); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

func TestDemoCommands(t *testing.T) {
	d := &fakeDemo{}
	h := demoRouter(d)
	if rec := demoPost(h, "/api/demo/clock/speed", `{"speed":600}`, "tok"); rec.Code != 200 {
		t.Fatalf("speed: %d %s", rec.Code, rec.Body)
	}
	if rec := demoPost(h, "/api/demo/clock/speed", `{"speed":0}`, "tok"); rec.Code != 200 {
		t.Fatalf("pause: %d", rec.Code)
	}
	for _, body := range []string{`{"speed":3601}`, `{"speed":-1}`, `{}`, `nope`} {
		if rec := demoPost(h, "/api/demo/clock/speed", body, "tok"); rec.Code != 400 {
			t.Errorf("speed %s: %d", body, rec.Code)
		}
	}
	if rec := demoPost(h, "/api/demo/clock/jump", `{"hours":6}`, "tok"); rec.Code != 200 {
		t.Fatalf("jump: %d", rec.Code)
	}
	for _, body := range []string{`{"hours":0}`, `{"hours":721}`, `{}`} {
		if rec := demoPost(h, "/api/demo/clock/jump", body, "tok"); rec.Code != 400 {
			t.Errorf("jump %s: %d", body, rec.Code)
		}
	}
	if rec := demoPost(h, "/api/demo/reset", `{}`, "tok"); rec.Code != 200 {
		t.Fatalf("reset: %d", rec.Code)
	}
	if len(d.speeds) != 2 || len(d.jumps) != 1 || d.resets != 1 {
		t.Fatalf("calls: %+v", d)
	}
}

func TestDemoNotSimulatedIs409(t *testing.T) {
	notSim := errors.New("not simulated")
	h := NewRouter(Deps{Demo: &fakeDemo{failure: notSim}, DemoToken: "tok", NotSimulated: notSim}).Handler()
	rec := demoPost(h, "/api/demo/clock/speed", `{"speed":2}`, "tok")
	if rec.Code != http.StatusConflict {
		t.Fatalf("code %d", rec.Code)
	}
}

type spyGate struct {
	mu   sync.Mutex
	held int
	max  int
}

func (g *spyGate) RLock()   { g.mu.Lock(); g.held++; g.max++; g.mu.Unlock() }
func (g *spyGate) RUnlock() { g.mu.Lock(); g.held--; g.mu.Unlock() }

func TestGateTakenByMutatingRequestsOnly(t *testing.T) {
	g := &spyGate{}
	var heldInside, heldInsideGet, heldInsideDemo int
	rt := NewRouter(Deps{Gate: g, Demo: &fakeDemo{}, DemoToken: "tok"})
	rt.Handle(http.MethodPost, "/api/test", func(w http.ResponseWriter, _ *http.Request) { heldInside = g.held })
	rt.Handle(http.MethodGet, "/api/test", func(w http.ResponseWriter, _ *http.Request) { heldInsideGet = g.held })
	h := rt.Handler()
	demoPost(h, "/api/test", `{}`, "")
	do(h, http.MethodGet, "/api/test", nil)
	heldInsideDemo = g.max
	demoPost(h, "/api/demo/reset", `{}`, "tok")
	if heldInside != 1 || heldInsideGet != 0 || g.max != heldInsideDemo || g.held != 0 {
		t.Fatalf("post held %d, get held %d, max %d→%d, left %d", heldInside, heldInsideGet, heldInsideDemo, g.max, g.held)
	}
}
