package api

import (
	"net/http"
	"time"
)

// ClockInfo — то, что /api/clock узнаёт о часах стенда сверх текущего времени.
// nil в Deps.Clock означает реальные часы без симулятора.
type ClockInfo interface {
	Speed() float64
	Simulated() bool
	SimulatorRunning() bool
}

// HotFollower — необязательное дополнение к ClockInfo: слушается ли генератор стенда горячих
// предложений (SIM_FOLLOW_HOT). Экран «Сеть» подписывает им точки демо «со SwapOwl и без».
type HotFollower interface {
	FollowHot() bool
}

// ProfileReporter — необязательное дополнение к ClockInfo: профиль сида (SEED_PROFILE).
type ProfileReporter interface {
	SeedProfile() string
}

type clockOut struct {
	Now              string  `json:"now"`
	Speed            float64 `json:"speed"`
	Timezone         string  `json:"timezone"`
	Simulated        bool    `json:"simulated"`
	SimulatorRunning bool    `json:"simulator_running"`
	FollowHot        bool    `json:"follow_hot"`
	SeedProfile      string  `json:"seed_profile"`
}

func (rt *Router) clockRoutes() {
	rt.HandlePublic(http.MethodGet, "/api/clock", rt.getClock) // открыт всем (7.4)
}

// getClock — GET /api/clock. Для реальных часов speed=1, simulated=false, simulator_running=false.
func (rt *Router) getClock(w http.ResponseWriter, _ *http.Request) {
	out := clockOut{
		Now:      rt.deps.Now().UTC().Format(time.RFC3339Nano),
		Speed:    1,
		Timezone: rt.location().String(),
		// Без стенда профиль — демо.
		SeedProfile: "demo",
	}
	if c := rt.deps.Clock; c != nil {
		out.Speed, out.Simulated, out.SimulatorRunning = c.Speed(), c.Simulated(), c.SimulatorRunning()
		if f, ok := c.(HotFollower); ok {
			out.FollowHot = f.FollowHot()
		}
		if p, ok := c.(ProfileReporter); ok {
			out.SeedProfile = p.SeedProfile()
		}
	}
	WriteJSON(w, http.StatusOK, out)
}
