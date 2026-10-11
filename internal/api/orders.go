package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/domain/units"
	"umirhack/internal/store"
)

// Orders — то, что нужно маршрутам заказов от хранилища.
type Orders interface {
	DishRecipes(ctx context.Context, ids []string) (map[string][]domain.RecipeItem, error)
	PlaceOrder(ctx context.Context, in store.OrderInput, now func() time.Time) (store.OrderResponse, error)
	ListOrders(ctx context.Context, status string, limit int) ([]store.OrderResponse, error)
	ListDishes(ctx context.Context) ([]store.DishView, error)
}

const (
	maxOrderItems = 100
	maxPortions   = 10000
	defaultLimit  = 100
	maxLimit      = 500
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type orderIn struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Items  []struct {
		DishID   string `json:"dish_id"`
		Portions *int   `json:"portions"`
	} `json:"items"`
}

func (rt *Router) orderRoutes() {
	if rt.deps.Orders == nil {
		return
	}
	rt.Handle(http.MethodPost, "/api/orders", rt.postOrder)
	rt.Handle(http.MethodGet, "/api/orders", rt.listOrders)
	if rt.deps.Menu == nil { // иначе /api/dishes отдаёт меню с порциями (stoplist.go)
		rt.Handle(http.MethodGet, "/api/dishes", rt.listDishes)
	}
}

func (rt *Router) postOrder(w http.ResponseWriter, r *http.Request) {
	var in orderIn
	if !decodeBody(w, r, &in) {
		return
	}
	if !uuidRe.MatchString(in.ID) {
		validationError(w, "id", "id must be a client-generated UUID")
		return
	}
	if len(in.Items) == 0 || len(in.Items) > maxOrderItems {
		validationError(w, "items", fmt.Sprintf("items must contain 1..%d positions", maxOrderItems))
		return
	}
	switch in.Source {
	case "":
		in.Source = store.SourceAPI
	case store.SourceAPI, store.SourcePOS:
	default:
		validationError(w, "source", `source must be "api" or "pos"`)
		return
	}
	in.ID = strings.ToLower(in.ID)
	items := make([]store.OrderItem, 0, len(in.Items))
	seen := map[string]bool{}
	ids := make([]string, 0, len(in.Items))
	for i, it := range in.Items {
		field := func(f string) string { return fmt.Sprintf("items[%d].%s", i, f) }
		if !validLabel(it.DishID) {
			validationError(w, field("dish_id"), "dish_id must be 1..128 characters without control characters or edge spaces")
			return
		}
		if seen[it.DishID] {
			validationError(w, field("dish_id"), "a dish may appear in at most one line")
			return
		}
		seen[it.DishID] = true
		if it.Portions == nil || *it.Portions < 1 || *it.Portions > maxPortions {
			validationError(w, field("portions"), fmt.Sprintf("portions must be an integer in 1..%d", maxPortions))
			return
		}
		items = append(items, store.OrderItem{DishID: it.DishID, Portions: *it.Portions})
		ids = append(ids, it.DishID)
	}
	recipes, err := rt.deps.Orders.DishRecipes(r.Context(), ids)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	need := map[string]domain.Qty{}
	for i, it := range items {
		recipe, ok := recipes[it.DishID]
		if !ok {
			WriteError(w, http.StatusBadRequest, CodeUnknownDish, fmt.Sprintf("unknown dish %q", it.DishID),
				map[string]any{"field": fmt.Sprintf("items[%d].dish_id", i)})
			return
		}
		for _, ri := range recipe {
			q, err := units.Mul(units.Qty(ri.Quantity), int64(it.Portions))
			if err == nil {
				var sum units.Qty
				sum, err = units.Add(units.Qty(need[ri.ProductID]), q)
				q = sum
			}
			if err != nil {
				validationError(w, fmt.Sprintf("items[%d].portions", i), "order quantity exceeds the limit")
				return
			}
			need[ri.ProductID] = domain.Qty(q)
		}
	}
	needs := make([]domain.Need, 0, len(need))
	for p, q := range need {
		needs = append(needs, domain.Need{ProductID: p, Qty: q})
	}
	sort.Slice(needs, func(i, j int) bool { return needs[i].ProductID < needs[j].ProductID })

	resp, err := rt.deps.Orders.PlaceOrder(r.Context(), store.OrderInput{ID: in.ID, Source: in.Source, Items: items, Needs: needs}, rt.deps.Now)
	switch {
	case err == nil:
	case errors.Is(err, store.ErrOrderConflict):
		WriteError(w, http.StatusConflict, CodeOrderConflict, "order id already used with different content", map[string]any{"order_id": in.ID})
		return
	default:
		rt.internalError(w, r, err)
		return
	}
	if resp.Status == store.OrderRejected {
		WriteError(w, http.StatusConflict, CodeInsufficient, "not enough stock for the order", map[string]any{
			"order_id": resp.ID, "status": resp.Status, "at": resp.At, "replayed": resp.Replayed, "missing": resp.Missing})
		return
	}
	status := http.StatusCreated
	if resp.Replayed {
		status = http.StatusOK
	}
	WriteJSON(w, status, resp)
}

func (rt *Router) listOrders(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status != "" && status != store.OrderAccepted && status != store.OrderRejected {
		validationError(w, "status", "status must be accepted or rejected")
		return
	}
	limit := defaultLimit
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxLimit {
			validationError(w, "limit", fmt.Sprintf("limit must be an integer in 1..%d", maxLimit))
			return
		}
		limit = n
	}
	out, err := rt.deps.Orders.ListOrders(r.Context(), status, limit)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

func (rt *Router) listDishes(w http.ResponseWriter, r *http.Request) {
	out, err := rt.deps.Orders.ListDishes(r.Context())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}
