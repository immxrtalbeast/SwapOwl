package api

import (
	"context"
	"net/http"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

// MenuSource — срез меню и остатков для стоп-листа.
type MenuSource interface {
	MenuSnapshot(ctx context.Context, now time.Time) (store.MenuSnapshot, error)
}

// ReasonJSON — чего не хватает на одну порцию блюда.
type ReasonJSON struct {
	ProductID   string        `json:"product_id"`
	ProductName string        `json:"product_name"`
	Required    store.QtyJSON `json:"required"`
	Available   store.QtyJSON `json:"available"`
	Missing     store.QtyJSON `json:"missing"`
}

// RecipeJSON — норма продукта на порцию.
type RecipeJSON struct {
	ProductID string        `json:"product_id"`
	Quantity  store.QtyJSON `json:"quantity"`
}

// DishAvailability — строка меню: число доступных порций и, для блюд в стоп-листе, причина.
type DishAvailability struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Available bool         `json:"available"`
	Servings  int64        `json:"servings"`
	Missing   []ReasonJSON `json:"missing"`
	Recipe    []RecipeJSON `json:"recipe"`
}

func (rt *Router) stopListRoutes() {
	if rt.deps.Menu == nil {
		return
	}
	rt.Handle(http.MethodGet, "/api/stop-list", rt.getStopList)
	rt.Handle(http.MethodGet, "/api/dishes", rt.getDishes)
}

// menuAvailability считает по снимку меню: все блюда с порциями и причинами для стоп-листа.
// Ничего не хранится: результат — функция остатков, партий и времени стенда.
func menuAvailability(snap store.MenuSnapshot) []DishAvailability {
	stopped := map[string][]domain.Shortage{}
	for _, s := range domain.StopList(snap.Dishes, snap.Available) {
		stopped[s.Dish.ID] = s.Missing
	}
	out := make([]DishAvailability, 0, len(snap.Dishes))
	for _, d := range snap.Dishes {
		row := DishAvailability{ID: d.ID, Name: d.Name, Missing: []ReasonJSON{}, Recipe: []RecipeJSON{}}
		miss, isStopped := stopped[d.ID]
		if !isStopped {
			row.Available = true
			row.Servings = domain.Servings(d.Recipe, snap.Available)
		}
		for _, it := range d.Recipe {
			row.Recipe = append(row.Recipe, RecipeJSON{ProductID: it.ProductID,
				Quantity: store.QtyJSON{Qty: int64(it.Quantity), Unit: snap.Products[it.ProductID].Unit}})
		}
		for _, m := range miss {
			p := snap.Products[m.ProductID]
			q := func(v domain.Qty) store.QtyJSON { return store.QtyJSON{Qty: int64(v), Unit: p.Unit} }
			row.Missing = append(row.Missing, ReasonJSON{
				ProductID: m.ProductID, ProductName: p.Name,
				Required: q(m.Required), Available: q(m.Available), Missing: q(m.Missing),
			})
		}
		out = append(out, row)
	}
	return out
}

func (rt *Router) menu(w http.ResponseWriter, r *http.Request) ([]DishAvailability, bool) {
	snap, err := rt.deps.Menu.MenuSnapshot(r.Context(), rt.deps.Now().UTC())
	if err != nil {
		rt.internalError(w, r, err)
		return nil, false
	}
	return menuAvailability(snap), true
}

// getDishes — GET /api/dishes: всё меню с рецептами, порциями и причинами для блюд в стоп-листе.
func (rt *Router) getDishes(w http.ResponseWriter, r *http.Request) {
	if rows, ok := rt.menu(w, r); ok {
		WriteJSON(w, http.StatusOK, rows)
	}
}

// getStopList — GET /api/stop-list: только блюда в стоп-листе (available=false, servings=0) с причинами.
func (rt *Router) getStopList(w http.ResponseWriter, r *http.Request) {
	rows, ok := rt.menu(w, r)
	if !ok {
		return
	}
	out := []DishAvailability{}
	for _, row := range rows {
		if !row.Available {
			out = append(out, row)
		}
	}
	WriteJSON(w, http.StatusOK, out)
}
