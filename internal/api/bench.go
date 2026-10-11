package api

import (
	"encoding/json"
	"net/http"

	"umirhack/docs"
)

// BenchResult — GET /api/analytics/bench: результат сравнения алгоритмов из docs/bench.json (2.7, «Граница»).
// Без результата — только available: false, цифр нет.
type BenchResult struct {
	Available                  bool    `json:"available"`
	Runs                       int     `json:"runs,omitempty"`
	MainAlgo                   string  `json:"main_algo,omitempty"`
	WasteReductionVsRandomPct  float64 `json:"waste_reduction_vs_random_pct,omitempty"`
	WasteReductionVsNearestPct float64 `json:"waste_reduction_vs_nearest_pct,omitempty"`
}

// parseBench читает встроенный JSON; нечитаемый или неполный результат считается отсутствующим.
func parseBench(raw []byte) BenchResult {
	var b BenchResult
	if err := json.Unmarshal(raw, &b); err != nil || b.Runs <= 0 || b.MainAlgo == "" {
		return BenchResult{}
	}
	return b
}

func (rt *Router) getBench(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, parseBench(docs.BenchJSON))
}
