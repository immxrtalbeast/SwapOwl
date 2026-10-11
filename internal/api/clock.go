package api

import "net/http"

// ClockInfo — то, что /api/clock узнаёт о часах стенда сверх текущего времени.
// nil в Deps.Clock означает реальные часы без симулятора.
type ClockInfo interface {
	Speed() float64
	Simulated() bool
	SimulatorRunning() bool
}

type clockOut struct {
	Now              string  `json:"now"`
	Speed            float64 `json:"speed"`
	Timezone         string  `json:"timezone"`
	Simulated        bool    `json:"simulated"`
	SimulatorRunning bool    `json:"simulator_running"`
}

func (rt *Router) clockRoutes() {
	rt.HandlePublic(http.MethodGet, "/api/clock", rt.getClock) // открыт всем (7.4)
}

// getClock — GET /api/clock. Для реальных часов speed=1, simulated=false, simulator_running=false.
func (rt *Router) getClock(w http.ResponseWriter, _ *http.Request) {
	out := clockOut{
		Now:      rt.deps.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
		Speed:    1,
		Timezone: rt.location().String(),
	}
	if c := rt.deps.Clock; c != nil {
		out.Speed, out.Simulated, out.SimulatorRunning = c.Speed(), c.Simulated(), c.SimulatorRunning()
	}
	WriteJSON(w, http.StatusOK, out)
}
