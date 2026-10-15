package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"umirhack/internal/auth"
	"umirhack/internal/simulator"
	"umirhack/internal/store"
)

// Лимиты ручки «Что если» (FS-167).
const (
	whatIfMaxScenarios = 5
	whatIfMaxHorizon   = 7
	whatIfTimeout      = 5 * time.Second
)

// WhatIfScenarioJSON — сценарий в запросе. Поля нужного типа обязательны, лишние запрещены.
type WhatIfScenarioJSON struct {
	Type      string   `json:"type"`
	DishID    string   `json:"dish_id,omitempty"`
	ProductID string   `json:"product_id,omitempty"`
	Percent   *float64 `json:"percent,omitempty"`
	Factor    *float64 `json:"factor,omitempty"`
}

// WhatIfRequest — тело POST /api/what-if.
type WhatIfRequest struct {
	HorizonDays *int                 `json:"horizon_days"`
	Scenarios   []WhatIfScenarioJSON `json:"scenarios"`
}

// WhatIfProductJSON — списание по продукту в базе и в сценарии.
type WhatIfProductJSON struct {
	ProductID     string `json:"product_id"`
	ProductName   string `json:"product_name"`
	BaseMinor     int64  `json:"base_minor"`
	ScenarioMinor int64  `json:"scenario_minor"`
	DeltaMinor    int64  `json:"delta_minor"`
}

// WhatIfBaseJSON — базовый прогноз «если ничего не делать».
type WhatIfBaseJSON struct {
	TotalMinor int64               `json:"total_minor"`
	Products   []WhatIfProductJSON `json:"products"`
}

// WhatIfResultJSON — итог одного сценария.
type WhatIfResultJSON struct {
	WhatIfScenarioJSON
	TotalMinor  int64               `json:"total_minor"`
	DeltaMinor  int64               `json:"delta_minor"`
	DeltaPct    float64             `json:"delta_pct"`
	Products    []WhatIfProductJSON `json:"products"`
	Assumptions []string            `json:"assumptions"`
}

// WhatIfResponse — тело ответа POST /api/what-if.
type WhatIfResponse struct {
	HorizonDays int                `json:"horizon_days"`
	Base        WhatIfBaseJSON     `json:"base"`
	Scenarios   []WhatIfResultJSON `json:"scenarios"`
	GeneratedAt time.Time          `json:"generated_at"`
}

func (rt *Router) whatIfRoutes() {
	if rt.deps.HotOffers == nil {
		return
	}
	// Читающая ручка: POST только потому, что сценарии не влезают в query. Ничего не пишет → viewer.
	rt.HandleRole(http.MethodPost, "/api/what-if", auth.Viewer, rt.postWhatIf)
}

// postWhatIf — POST /api/what-if: пересчёт прогноза списаний по сценариям на копии снимка.
func (rt *Router) postWhatIf(w http.ResponseWriter, r *http.Request) {
	var req WhatIfRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.HorizonDays == nil || *req.HorizonDays < 1 || *req.HorizonDays > whatIfMaxHorizon {
		validationError(w, "horizon_days", fmt.Sprintf("horizon_days — целое число от 1 до %d", whatIfMaxHorizon))
		return
	}
	if len(req.Scenarios) > whatIfMaxScenarios {
		validationError(w, "scenarios", fmt.Sprintf("не больше %d сценариев за запрос", whatIfMaxScenarios))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), whatIfTimeout)
	defer cancel()
	snap, err := rt.deps.HotOffers.Snapshot(ctx, rt.deps.Now().UTC())
	if err != nil {
		rt.whatIfFail(w, r, err)
		return
	}
	scs := make([]simulator.WhatIfScenario, len(req.Scenarios))
	for i, s := range req.Scenarios {
		sc, field, msg := checkWhatIfScenario(snap, i, s)
		if msg != "" {
			validationError(w, field, msg)
			return
		}
		scs[i] = sc
	}
	res, err := simulator.WhatIf(ctx, snap, *req.HorizonDays, scs)
	if err != nil {
		rt.whatIfFail(w, r, err)
		return
	}
	loc := snap.Schedule.Location
	if loc == nil {
		loc = time.UTC
	}
	WriteJSON(w, http.StatusOK, buildWhatIf(res, req.Scenarios, snap.Now.In(loc)))
}

func (rt *Router) whatIfFail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		WriteError(w, http.StatusServiceUnavailable, CodeOverloaded,
			fmt.Sprintf("Расчёт сценариев не уложился в %d с. Уменьшите горизонт или число сценариев и повторите.", int(whatIfTimeout.Seconds())), nil)
		return
	}
	rt.internalError(w, r, err)
}

// checkWhatIfScenario проверяет сценарий i; при ошибке возвращает поле и текст.
func checkWhatIfScenario(snap store.Snapshot, i int, s WhatIfScenarioJSON) (simulator.WhatIfScenario, string, string) {
	f := func(name string) string { return fmt.Sprintf("scenarios[%d].%s", i, name) }
	out := simulator.WhatIfScenario{Type: s.Type}
	dish := func() (string, string) {
		if s.DishID == "" || s.ProductID != "" {
			return f("dish_id"), "dish_id обязателен, product_id для этого типа не нужен"
		}
		for _, d := range snap.Dishes {
			if d.ID == s.DishID {
				return "", ""
			}
		}
		return f("dish_id"), "неизвестное блюдо " + s.DishID
	}
	inRange := func(v *float64, name string, lo, hi float64) string {
		if v == nil || math.IsNaN(*v) || *v < lo || *v > hi {
			return fmt.Sprintf("%s — число от %g до %g", name, lo, hi)
		}
		return ""
	}
	switch s.Type {
	case simulator.WhatIfDiscount, simulator.WhatIfDemand:
		if field, msg := dish(); msg != "" {
			return out, field, msg
		}
		out.DishID = s.DishID
		if s.Type == simulator.WhatIfDiscount {
			if msg := inRange(s.Percent, "percent", 5, 50); msg != "" || s.Factor != nil {
				return out, f("percent"), "percent — число от 5 до 50 (factor для скидки не нужен)"
			}
			out.Percent = *s.Percent
		} else {
			if msg := inRange(s.Factor, "factor", 0.5, 2); msg != "" || s.Percent != nil {
				return out, f("factor"), "factor — число от 0.5 до 2 (percent для спроса не нужен)"
			}
			out.Factor = *s.Factor
		}
	case simulator.WhatIfOrderLess:
		if s.ProductID == "" || s.DishID != "" {
			return out, f("product_id"), "product_id обязателен, dish_id для этого типа не нужен"
		}
		if _, ok := snap.Products[s.ProductID]; !ok {
			return out, f("product_id"), "неизвестный продукт " + s.ProductID
		}
		if msg := inRange(s.Percent, "percent", 10, 100); msg != "" || s.Factor != nil {
			return out, f("percent"), "percent — число от 10 до 100 (factor для закупки не нужен)"
		}
		out.ProductID, out.Percent = s.ProductID, *s.Percent
	default:
		return out, f("type"), "type — discount, order_less или demand"
	}
	return out, "", ""
}

func buildWhatIf(res simulator.WhatIfResult, in []WhatIfScenarioJSON, now time.Time) WhatIfResponse {
	resp := WhatIfResponse{HorizonDays: res.HorizonDays, GeneratedAt: now, Scenarios: []WhatIfResultJSON{},
		Base: WhatIfBaseJSON{TotalMinor: res.BaseMinor, Products: []WhatIfProductJSON{}}}
	for _, p := range res.BaseProducts {
		resp.Base.Products = append(resp.Base.Products, whatIfProduct(p))
	}
	for i, o := range res.Outcomes {
		r := WhatIfResultJSON{WhatIfScenarioJSON: in[i], TotalMinor: o.TotalMinor, DeltaMinor: o.TotalMinor - res.BaseMinor,
			Products: []WhatIfProductJSON{}, Assumptions: o.Assumptions}
		if res.BaseMinor > 0 {
			r.DeltaPct = math.Round(float64(r.DeltaMinor)*1000/float64(res.BaseMinor)) / 10
		}
		for _, p := range o.Products {
			r.Products = append(r.Products, whatIfProduct(p))
		}
		resp.Scenarios = append(resp.Scenarios, r)
	}
	return resp
}

func whatIfProduct(p simulator.WhatIfProduct) WhatIfProductJSON {
	return WhatIfProductJSON{ProductID: p.ProductID, ProductName: p.ProductName, BaseMinor: p.BaseMinor,
		ScenarioMinor: p.ScenarioMinor, DeltaMinor: p.ScenarioMinor - p.BaseMinor}
}
