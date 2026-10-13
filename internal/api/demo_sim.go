package api

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/domain/units"
	"umirhack/internal/store"
)

// MaxBurst — верхняя граница залпа (7.4).
const MaxBurst = 50

// ErrUnknownDish — блюда нет в справочнике.
var ErrUnknownDish = errors.New("api: unknown dish")

// SimControl — запуск и пауза генератора заказов (2.8). nil в Deps.Sim — ручка симулятора не регистрируется.
type SimControl interface {
	SetRunning(running bool)
	Running() bool
}

// OrderPlacer принимает заказ тем же путём, что POST /api/orders: считает потребность по рецептам и
// вызывает Orders.PlaceOrder. Генератор и залп идут через него, а не мимо сервисов.
type OrderPlacer struct {
	Orders Orders
	Now    func() time.Time
	Gate   Gate // nil — без затвора; изменяющие запросы берут его на чтение (2.8)
}

// Place кладёт заказ; отказ по нехватке остатков — не ошибка, а ответ со статусом rejected.
func (p OrderPlacer) Place(ctx context.Context, id, source string, items []store.OrderItem) (store.OrderResponse, error) {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.DishID)
	}
	recipes, err := p.Orders.DishRecipes(ctx, ids)
	if err != nil {
		return store.OrderResponse{}, err
	}
	need := map[string]domain.Qty{}
	for _, it := range items {
		recipe, ok := recipes[it.DishID]
		if !ok {
			return store.OrderResponse{}, fmt.Errorf("%w: %q", ErrUnknownDish, it.DishID)
		}
		for _, ri := range recipe {
			q, err := units.Mul(units.Qty(ri.Quantity), int64(it.Portions))
			if err == nil {
				var sum units.Qty
				sum, err = units.Add(units.Qty(need[ri.ProductID]), q)
				q = sum
			}
			if err != nil {
				return store.OrderResponse{}, err
			}
			need[ri.ProductID] = domain.Qty(q)
		}
	}
	needs := make([]domain.Need, 0, len(need))
	for pid, q := range need {
		needs = append(needs, domain.Need{ProductID: pid, Qty: q})
	}
	sort.Slice(needs, func(i, j int) bool { return needs[i].ProductID < needs[j].ProductID })
	if p.Gate != nil {
		p.Gate.RLock()
		defer p.Gate.RUnlock()
	}
	return p.Orders.PlaceOrder(ctx, store.OrderInput{ID: id, Source: source, Items: items, Needs: needs}, p.Now)
}

func (rt *Router) demoSimRoutes() {
	if rt.deps.Demo == nil {
		return
	}
	// Как speed/jump/reset: только по DEMO_TOKEN (2.8), чтобы посетитель стенда не запустил генератор или залп.
	if rt.deps.Sim != nil {
		rt.HandlePublic(http.MethodPost, "/api/demo/simulator", rt.demoAuth(rt.demoSimulator))
	}
	if rt.deps.Orders != nil {
		rt.HandlePublic(http.MethodPost, "/api/demo/burst", rt.demoAuth(rt.demoBurst))
	}
}

func (rt *Router) demoSimulator(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Running *bool `json:"running"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if in.Running == nil {
		validationError(w, "running", `body must be {"running": bool}`)
		return
	}
	rt.deps.Sim.SetRunning(*in.Running)
	rt.audit(r, "demo.simulator", slog.Bool("running", *in.Running))
	WriteJSON(w, http.StatusOK, map[string]any{"running": rt.deps.Sim.Running()})
}

func (rt *Router) demoBurst(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DishID string `json:"dish_id"`
		Orders *int   `json:"orders"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if !validLabel(in.DishID) {
		validationError(w, "dish_id", "dish_id must be 1..128 characters without control characters or edge spaces")
		return
	}
	if in.Orders == nil || *in.Orders < 1 || *in.Orders > MaxBurst {
		validationError(w, "orders", fmt.Sprintf("orders must be an integer in 1..%d", MaxBurst))
		return
	}
	n := *in.Orders
	placer := OrderPlacer{Orders: rt.deps.Orders, Now: rt.deps.Now, Gate: rt.deps.Gate}
	recipes, err := rt.deps.Orders.DishRecipes(r.Context(), []string{in.DishID})
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	if _, ok := recipes[in.DishID]; !ok {
		WriteError(w, http.StatusBadRequest, CodeUnknownDish, fmt.Sprintf("unknown dish %q", in.DishID), map[string]any{"field": "dish_id"})
		return
	}

	var (
		wg                         sync.WaitGroup
		mu                         sync.Mutex
		accepted, rejected, failed int
		firstErr                   error
		start                      = make(chan struct{})
	)
	for i := 0; i < n; i++ {
		id, err := newUUID()
		if err != nil {
			rt.internalError(w, r, err)
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // все заказы стартуют одновременно
			resp, err := placer.Place(r.Context(), id, store.SourceAPI, []store.OrderItem{{DishID: in.DishID, Portions: 1}})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				failed++
				if firstErr == nil {
					firstErr = err
				}
			case resp.Status == store.OrderRejected:
				rejected++
			default:
				accepted++
			}
		}()
	}
	close(start)
	wg.Wait()
	rt.audit(r, "demo.burst", slog.String("dish_id", in.DishID), slog.Int("orders", n),
		slog.Int("accepted", accepted), slog.Int("rejected", rejected))
	if firstErr != nil {
		rt.internalError(w, r, firstErr)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"dish_id": in.DishID, "orders": n, "accepted": accepted, "rejected": rejected})
}

// newUUID — случайный UUID v4.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
