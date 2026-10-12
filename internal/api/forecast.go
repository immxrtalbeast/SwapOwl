package api

import (
	"net/http"
	"time"

	"umirhack/internal/store"
)

// ForecastProductJSON — продукт в разбивке прогноза списаний.
type ForecastProductJSON struct {
	ProductID   string        `json:"product_id"`
	ProductName string        `json:"product_name"`
	AtRisk      store.QtyJSON `json:"at_risk"`
	TotalMinor  int64         `json:"total_minor"`
}

// ForecastDayJSON — сумма по дню срока партий (зона ресторана).
type ForecastDayJSON struct {
	Date       string `json:"date"`
	TotalMinor int64  `json:"total_minor"`
}

// ForecastWriteoffsResponse — тело GET /api/forecast/writeoffs.
type ForecastWriteoffsResponse struct {
	Hours       int                   `json:"hours"`
	TotalMinor  int64                 `json:"total_minor"`
	Products    []ForecastProductJSON `json:"products"`
	Days        []ForecastDayJSON     `json:"days"`
	GeneratedAt time.Time             `json:"generated_at"`
}

func (rt *Router) forecastRoutes() {
	if rt.deps.HotOffers == nil {
		return
	}
	rt.Handle(http.MethodGet, "/api/forecast/writeoffs", rt.getForecastWriteoffs)
}

// getForecastWriteoffs — GET /api/forecast/writeoffs?hours=24|72|168 (по умолчанию 168).
func (rt *Router) getForecastWriteoffs(w http.ResponseWriter, r *http.Request) {
	hours := 168
	switch h := r.URL.Query().Get("hours"); h {
	case "":
	case "24":
		hours = 24
	case "72":
		hours = 72
	case "168":
	default:
		validationError(w, "hours", "hours must be 24, 72 or 168")
		return
	}
	snap, err := rt.deps.HotOffers.Snapshot(r.Context(), rt.deps.Now().UTC())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, buildForecast(store.ForecastWriteoffs(snap, hours), snap.Schedule.Location))
}

func buildForecast(f store.WriteoffForecast, loc *time.Location) ForecastWriteoffsResponse {
	if loc == nil {
		loc = time.UTC
	}
	resp := ForecastWriteoffsResponse{Hours: f.Hours, TotalMinor: f.TotalMinor, GeneratedAt: f.GeneratedAt.In(loc),
		Products: []ForecastProductJSON{}, Days: []ForecastDayJSON{}}
	for _, p := range f.Products {
		resp.Products = append(resp.Products, ForecastProductJSON{ProductID: p.ProductID, ProductName: p.Name,
			AtRisk: store.QtyJSON{Qty: int64(p.AtRisk), Unit: p.Unit}, TotalMinor: p.TotalMinor})
	}
	for _, d := range f.Days {
		resp.Days = append(resp.Days, ForecastDayJSON{Date: d.Date, TotalMinor: d.TotalMinor})
	}
	return resp
}
