package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"umirhack/internal/events"
	"umirhack/internal/store"
)

// CodeDishOnHold — заказ содержит блюдо, поставленное менеджером на паузу.
const CodeDishOnHold = "dish_on_hold"

// DishHolds — ручная пауза блюд. nil — маршруты паузы не регистрируются, заказ проходит как раньше.
type DishHolds interface {
	ListDishHolds(ctx context.Context) ([]store.DishHold, error)
	SetDishHold(ctx context.Context, h store.DishHold) (store.DishHold, error)
	DeleteDishHold(ctx context.Context, dishID string) error
}

func (rt *Router) dishHoldRoutes() {
	if rt.deps.DishHolds == nil {
		return
	}
	rt.Handle(http.MethodGet, "/api/dish-holds", rt.listDishHolds)
	rt.Handle(http.MethodPut, "/api/dish-holds/{dish_id}", rt.putDishHold)
	rt.Handle(http.MethodDelete, "/api/dish-holds/{dish_id}", rt.deleteDishHold)
}

// listDishHolds — GET /api/dish-holds: блюда на паузе, новые первыми (viewer).
func (rt *Router) listDishHolds(w http.ResponseWriter, r *http.Request) {
	list, err := rt.deps.DishHolds.ListDishHolds(r.Context())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, list)
}

// putDishHold — PUT /api/dish-holds/{dish_id} {"note"} (operator): поставить блюдо на паузу. Заказ с ним
// отклоняется (409 dish_on_hold), автоматический стоп-лист по остаткам не меняется. Повтор идемпотентен.
func (rt *Router) putDishHold(w http.ResponseWriter, r *http.Request) {
	dish := r.PathValue("dish_id")
	var in struct {
		Note string `json:"note"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > 200 {
		validationError(w, "note", "note — не длиннее 200 символов")
		return
	}
	by := ""
	if p, ok := PrincipalFrom(r.Context()); ok {
		by = p.Login
	}
	h, err := rt.deps.DishHolds.SetDishHold(r.Context(), store.DishHold{DishID: dish, Note: in.Note, SetAt: rt.deps.Now().UTC(), SetBy: by})
	if errors.Is(err, store.ErrDishHoldDish) {
		WriteError(w, http.StatusNotFound, CodeUnknownDish, "блюда нет в справочнике", map[string]any{"dish_id": dish})
		return
	}
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "hold_set", slog.String("dish_id", dish))
	rt.publishHoldChanged(dish, true, h.Note)
	WriteJSON(w, http.StatusOK, h)
}

// deleteDishHold — DELETE /api/dish-holds/{dish_id} (operator): снять паузу.
func (rt *Router) deleteDishHold(w http.ResponseWriter, r *http.Request) {
	dish := r.PathValue("dish_id")
	if err := rt.deps.DishHolds.DeleteDishHold(r.Context(), dish); err != nil {
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "hold_delete", slog.String("dish_id", dish))
	rt.publishHoldChanged(dish, false, "")
	w.WriteHeader(http.StatusNoContent)
}

// heldSet — множество блюд на паузе; без DishHolds — пустое. Горячие предложения, витрина и меню гостя
// не предлагают блюдо, которое нельзя заказать.
func (rt *Router) heldSet(ctx context.Context) (map[string]bool, error) {
	if rt.deps.DishHolds == nil {
		return nil, nil
	}
	list, err := rt.deps.DishHolds.ListDishHolds(ctx)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(list))
	for _, h := range list {
		set[h.DishID] = true
	}
	return set, nil
}

// withHolds добавляет блюда на паузе к стоп-листу снимка: расчёт предложений обходит их, как блюда без остатков.
func (rt *Router) withHolds(ctx context.Context, snap *store.Snapshot) error {
	held, err := rt.heldSet(ctx)
	if err != nil {
		return err
	}
	if len(held) == 0 {
		return nil
	}
	stopped := make(map[string]bool, len(snap.Stopped)+len(held))
	for id := range snap.Stopped {
		stopped[id] = true
	}
	for id := range held {
		stopped[id] = true
	}
	snap.Stopped = stopped
	return nil
}

// publishHoldChanged — открытые экраны меню и заказов перечитывают список.
func (rt *Router) publishHoldChanged(dish string, held bool, note string) {
	if rt.deps.Events != nil {
		rt.deps.Events.Publish(events.StoplistChanged, map[string]any{"added": []string{}, "removed": []string{},
			"reason": "manual_hold", "dish_id": dish, "held": held, "note": note})
	}
}
