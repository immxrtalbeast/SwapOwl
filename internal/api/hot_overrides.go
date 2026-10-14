package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"umirhack/internal/events"
	"umirhack/internal/store"
)

// listHotOverrides — GET /api/hot-offers/overrides: решения менеджера, новые первыми (viewer).
func (rt *Router) listHotOverrides(w http.ResponseWriter, r *http.Request) {
	list, err := rt.deps.HotOverrides.ListHotOverrides(r.Context())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, list)
}

// putHotOverride — PUT /api/hot-offers/overrides/{dish_id} {"mode":"pin"|"hide","note"} (operator):
// закрепить блюдо в горячих предложениях или скрыть его. Повтор идемпотентен, прежнее решение заменяется.
func (rt *Router) putHotOverride(w http.ResponseWriter, r *http.Request) {
	dish := r.PathValue("dish_id")
	var in struct {
		Mode string `json:"mode"`
		Note string `json:"note"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	switch {
	case in.Mode != store.HotPin && in.Mode != store.HotHide:
		validationError(w, "mode", "mode — pin (добавить в предложения) или hide (скрыть)")
		return
	case utf8.RuneCountInString(in.Note) > 200:
		validationError(w, "note", "note — не длиннее 200 символов")
		return
	}
	by := ""
	if p, ok := PrincipalFrom(r.Context()); ok {
		by = p.Login
	}
	o, err := rt.deps.HotOverrides.SetHotOverride(r.Context(), store.HotOverride{DishID: dish, Mode: in.Mode, Note: in.Note, SetAt: rt.deps.Now().UTC(), SetBy: by})
	if errors.Is(err, store.ErrHotOverrideDish) {
		WriteError(w, http.StatusNotFound, CodeUnknownDish, "блюда нет в справочнике", map[string]any{"dish_id": dish})
		return
	}
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "hot_override_set", slog.String("dish_id", dish), slog.String("mode", in.Mode))
	rt.publishHotChanged(dish)
	WriteJSON(w, http.StatusOK, o)
}

// deleteHotOverride — DELETE /api/hot-offers/overrides/{dish_id} (operator): вернуть блюдо под расчёт.
func (rt *Router) deleteHotOverride(w http.ResponseWriter, r *http.Request) {
	dish := r.PathValue("dish_id")
	if err := rt.deps.HotOverrides.DeleteHotOverride(r.Context(), dish); err != nil {
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "hot_override_delete", slog.String("dish_id", dish))
	rt.publishHotChanged(dish)
	w.WriteHeader(http.StatusNoContent)
}

// publishHotChanged — открытые экраны «Горячие предложения» и «Для зала» перечитывают список.
func (rt *Router) publishHotChanged(dish string) {
	if rt.deps.Events != nil {
		rt.deps.Events.Publish(events.HotOffersChanged, map[string]any{"reason": "manager", "dish_id": dish})
	}
}
