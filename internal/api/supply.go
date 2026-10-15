package api

import (
	"context"
	"net/http"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

// SupplySource — вход расчёта предложения поставок на момент now (store.SupplyReader).
type SupplySource interface {
	SupplyData(ctx context.Context, now time.Time) (store.SupplyData, error)
}

// SupplyJSON — строка GET /api/supply-suggestions (7.4).
type SupplyJSON struct {
	ProductID    string        `json:"product_id"`
	ProductName  string        `json:"product_name"`
	Quantity     store.QtyJSON `json:"quantity"`
	UsableStock  store.QtyJSON `json:"usable_stock"`
	RatePerDay   store.QtyJSON `json:"rate_per_day"`
	StockoutAt   *time.Time    `json:"stockout_at"`
	BlocksDishes int           `json:"blocks_dishes"`
	Urgency      string        `json:"urgency"`
	Explanation  string        `json:"explanation"`
	// ShelfLifeHours — обычный срок поставщика из карточки; «Принять предложенное» передаёт его в поставку явно,
	// потому что поставка без срока не принимается (FS-160). null — срок нужно ввести.
	ShelfLifeHours *int `json:"shelf_life_hours"`
}

func (rt *Router) supplyRoutes() {
	if rt.deps.Supply == nil {
		return
	}
	rt.Handle(http.MethodGet, "/api/supply-suggestions", rt.getSupply)
}

// getSupply — GET /api/supply-suggestions. Пересчёт при каждом чтении; состояния нет.
func (rt *Router) getSupply(w http.ResponseWriter, r *http.Request) {
	data, err := rt.deps.Supply.SupplyData(r.Context(), rt.deps.Now().UTC())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, buildSupply(data, rt.deps.Location))
}

// buildSupply — чистая сборка ответа из domain.SupplySuggestions.
func buildSupply(data store.SupplyData, loc *time.Location) []SupplyJSON {
	if loc == nil {
		loc = time.UTC
	}
	out := []SupplyJSON{}
	for _, s := range domain.SupplySuggestions(data.Input) {
		u := data.Products[s.ProductID].Unit
		row := SupplyJSON{ProductID: s.ProductID, ProductName: s.ProductName,
			Quantity:     store.QtyJSON{Qty: int64(s.Quantity), Unit: u},
			UsableStock:  store.QtyJSON{Qty: int64(s.UsableStock), Unit: u},
			RatePerDay:   store.QtyJSON{Qty: int64(s.RatePerDay), Unit: u},
			BlocksDishes: s.BlocksDishes, Urgency: string(s.Urgency), Explanation: s.Explanation,
			ShelfLifeHours: data.Meta[s.ProductID].ShelfLifeHours}
		if s.StockoutAt != nil {
			t := s.StockoutAt.In(loc)
			row.StockoutAt = &t
		}
		out = append(out, row)
	}
	return out
}
