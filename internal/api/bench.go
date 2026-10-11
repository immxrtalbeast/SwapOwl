package api

import (
	"encoding/json"
	"net/http"

	"umirhack/docs"
)

// BenchResult — ответ GET /api/analytics/bench (7.4). Поля цифры заполнены только при Available.
type BenchResult struct {
	Available                  bool    `json:"available"`
	Runs                       int     `json:"runs,omitempty"`
	MainAlgo                   string  `json:"main_algo,omitempty"`
	WasteReductionVsRandomPct  float64 `json:"waste_reduction_vs_random_pct,omitempty"`
	WasteReductionVsNearestPct float64 `json:"waste_reduction_vs_nearest_pct,omitempty"`
}

// parseBench разбирает содержимое docs/bench.json. Пустой, нечитаемый или неполный результат —
// available: false: цифру без прогона не показываем (2.7, «Граница»).
func parseBench(raw []byte) BenchResult {
	var b BenchResult
	if err := json.Unmarshal(raw, &b); err != nil || !b.Available || b.Runs <= 0 || b.MainAlgo == "" {
		return BenchResult{}
	}
	return b
}

// getBench — GET /api/analytics/bench: результат bench, встроенный в бинарь.
func (rt *Router) getBench(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, parseBench(docs.BenchJSON))
}
