package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

type fakeClock struct{ speed float64 }

func (c fakeClock) Speed() float64       { return c.speed }
func (fakeClock) Simulated() bool        { return true }
func (fakeClock) SimulatorRunning() bool { return true }

func TestClockReal(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	rt := NewRouter(Deps{Now: func() time.Time { return standTime }, Location: loc})
	rec := do(rt.Handler(), http.MethodGet, "/api/clock", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"now": "2026-10-10T12:00:00Z", "speed": 1.0, "timezone": "Europe/Moscow",
		"simulated": false, "simulator_running": false}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestClockSimulatedAndOpenWithoutAuth(t *testing.T) {
	rt := NewRouter(Deps{Now: func() time.Time { return standTime }, Clock: fakeClock{speed: 600}})
	var got clockOut
	rec := do(rt.Handler(), http.MethodGet, "/api/clock", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Speed != 600 || !got.Simulated || !got.SimulatorRunning || got.Timezone != "UTC" {
		t.Fatalf("%+v", got)
	}
}

type followClock struct{ fakeClock }

func (followClock) FollowHot() bool { return true }

func TestClockReportsFollowHot(t *testing.T) {
	for _, c := range []struct {
		info ClockInfo
		want bool
	}{{fakeClock{speed: 600}, false}, {followClock{fakeClock{speed: 600}}, true}} {
		rt := NewRouter(Deps{Now: func() time.Time { return standTime }, Clock: c.info})
		var got clockOut
		if err := json.Unmarshal(do(rt.Handler(), http.MethodGet, "/api/clock", nil).Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.FollowHot != c.want {
			t.Errorf("follow_hot = %v, want %v", got.FollowHot, c.want)
		}
	}
}
