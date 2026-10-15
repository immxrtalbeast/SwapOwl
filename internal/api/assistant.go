package api

import (
	"context"
	"net/http"
	"time"
	"unicode/utf8"

	"umirhack/internal/assistant"
	"umirhack/internal/auth"
	"umirhack/internal/store"
)

// maxQuestionRunes — длиннее вопрос не нужен: разбор по ключевым словам.
const maxQuestionRunes = 300

// AskRequest — тело POST /api/assistant/ask.
type AskRequest struct {
	Question string `json:"question"`
}

func (rt *Router) assistantRoutes() {
	if rt.deps.Stock == nil && rt.deps.Menu == nil {
		return
	}
	// Читающая ручка: POST только ради тела вопроса. Ничего не пишет → viewer.
	rt.HandleRole(http.MethodPost, "/api/assistant/ask", auth.Viewer, rt.postAsk)
}

// postAsk — POST /api/assistant/ask {question}: ответ по правилам на вопрос своими словами (FS-176).
// Данные — те же, что видят экраны: остатки, предупреждения, стоп-лист, поставки, горячие предложения.
func (rt *Router) postAsk(w http.ResponseWriter, r *http.Request) {
	var req AskRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if n := utf8.RuneCountInString(req.Question); n < 1 || n > maxQuestionRunes {
		validationError(w, "question", "question — от 1 до 300 символов")
		return
	}
	facts, err := rt.assistantFacts(r.Context())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, assistant.Answer(req.Question, facts))
}

func qty(q store.QtyJSON) assistant.Qty { return assistant.Qty{Qty: q.Qty, Unit: string(q.Unit)} }

// assistantFacts собирает данные из тех же источников, что и экраны; недоступный источник просто пропускается.
func (rt *Router) assistantFacts(ctx context.Context) (assistant.Facts, error) {
	now := rt.deps.Now().UTC()
	loc := rt.deps.Location
	if loc == nil {
		loc = time.UTC
	}
	f := assistant.Facts{Now: now, Loc: loc}

	if rt.deps.Stock != nil {
		items, err := rt.deps.Stock.ListProductStock(ctx, now)
		if err != nil {
			return f, err
		}
		for _, p := range items {
			f.Products = append(f.Products, assistant.Product{ID: p.ID, Name: p.Name, Available: qty(p.Available),
				Batches: p.BatchesCount, NearestExpiresAt: p.NearestExpiresAt})
		}
	}
	if rt.deps.Monitor != nil {
		items, err := rt.deps.Monitor.ListAlerts(ctx, now)
		if err != nil {
			return f, err
		}
		for _, a := range items {
			f.Alerts = append(f.Alerts, assistant.Alert{ProductName: a.ProductName, Remaining: qty(a.Remaining),
				ExpiresAt: a.ExpiresAt, Expired: a.Status == "expired", Doomed: a.Doomed})
		}
	}
	if rt.deps.Menu != nil {
		snap, err := rt.deps.Menu.MenuSnapshot(ctx, now)
		if err != nil {
			return f, err
		}
		held, err := rt.heldSet(ctx)
		if err != nil {
			return f, err
		}
		for _, d := range menuAvailability(snap) {
			dish := assistant.Dish{Name: d.Name, Available: d.Available && !held[d.ID], Servings: d.Servings, Held: held[d.ID]}
			for _, m := range d.Missing {
				dish.Missing = append(dish.Missing, assistant.Missing{ProductName: m.ProductName,
					Required: qty(m.Required), Present: qty(m.Available)})
			}
			f.Dishes = append(f.Dishes, dish)
		}
	}
	if rt.deps.Supply != nil {
		data, err := rt.deps.Supply.SupplyData(ctx, now)
		if err != nil {
			return f, err
		}
		for _, s := range buildSupply(data, loc) {
			f.Supply = append(f.Supply, assistant.Supply{ProductName: s.ProductName, Quantity: qty(s.Quantity),
				Urgency: s.Urgency, BlocksDishes: s.BlocksDishes})
		}
	}
	if rt.deps.HotOffers != nil {
		hot, err := rt.assistantHot(ctx, now)
		if err != nil {
			return f, err
		}
		f.Hot = hot
	}
	return f, nil
}

// assistantHot — горячие предложения так, как их видит зал: с правкой менеджера, без скрытых и стоп-листа.
func (rt *Router) assistantHot(ctx context.Context, now time.Time) ([]assistant.Hot, error) {
	cfg := rt.deps.HotConfig
	algo := cfg.DefaultAlgo
	if algo != "a" && algo != "b" {
		algo = "a"
	}
	snap, err := rt.deps.HotOffers.Snapshot(ctx, now)
	if err != nil {
		return nil, err
	}
	if err := rt.withHolds(ctx, &snap); err != nil {
		return nil, err
	}
	resp := buildHotOffers(snap, algo, cfg.Rank)
	if rt.deps.HotOverrides != nil {
		overrides, err := rt.deps.HotOverrides.ListHotOverrides(ctx)
		if err != nil {
			return nil, err
		}
		names := make(map[string]string, len(snap.Dishes))
		for _, d := range snap.Dishes {
			names[d.ID] = d.Name
		}
		resp = applyOverrides(resp, overrides, names, snap.Stopped)
	}
	var out []assistant.Hot
	for _, p := range resp.ManagerPicks {
		out = append(out, assistant.Hot{DishName: p.DishName, Note: p.Note})
	}
	for _, o := range resp.Offers {
		h := assistant.Hot{DishName: o.DishName, Note: o.ManagerNote}
		seen := map[string]bool{}
		for _, r := range o.Reasons {
			if !seen[r.ProductName] {
				seen[r.ProductName] = true
				h.Products = append(h.Products, r.ProductName)
			}
		}
		out = append(out, h)
	}
	return out, nil
}
