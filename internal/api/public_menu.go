package api

import (
	"net/http"
	"strconv"
	"time"
)

// Статусы блюда для гостя: ни причин, ни остатков, ни партий — только «можно заказать» или нет.
const (
	menuAvailable = "available"
	menuFew       = "few" // осталось мало порций: «успейте заказать»
	menuStopped   = "stopped"

	// menuFewServings — порогом «мало» считаем до трёх порций включительно.
	menuFewServings = 3
)

// PublicMenuJSON — строка публичного меню гостя.
type PublicMenuJSON struct {
	DishID   string `json:"dish_id"`
	DishName string `json:"dish_name"`
	Status   string `json:"status"`
}

// publicMenuStatus — единственное, что гость узнаёт о складе: хватает ли блюда.
func publicMenuStatus(available bool, servings int64) string {
	switch {
	case !available || servings <= 0:
		return menuStopped
	case servings <= menuFewServings:
		return menuFew
	}
	return menuAvailable
}

// publicMenuRoutes — GET /api/public/menu: меню гостя со стоп-листом. Включается тем же PUBLIC_RESCUE, что и витрина
// «Спаси продукты» (публичные страницы гостя), и под тем же лимитом на адрес.
func (rt *Router) publicMenuRoutes() {
	if !rt.deps.PublicRescue || rt.deps.Menu == nil {
		return
	}
	lim := &ipLimiter{hits: map[string][]time.Time{}, max: rescuePerMinute}
	rt.HandlePublic(http.MethodGet, "/api/public/menu", func(w http.ResponseWriter, r *http.Request) {
		if !lim.allow(rescueClientIP(r), time.Now()) {
			w.Header().Set("Retry-After", "60")
			WriteError(w, http.StatusTooManyRequests, CodeOverloaded, "too many requests", nil)
			return
		}
		snap, err := rt.deps.Menu.MenuSnapshot(r.Context(), rt.deps.Now().UTC())
		if err != nil {
			rt.internalError(w, r, err)
			return
		}
		held, err := rt.heldSet(r.Context())
		if err != nil {
			rt.internalError(w, r, err)
			return
		}
		out := []PublicMenuJSON{}
		for _, d := range menuAvailability(snap) {
			// Блюдо на паузе гость заказать не может — для него это «нет в наличии».
			out = append(out, PublicMenuJSON{DishID: d.ID, DishName: d.Name, Status: publicMenuStatus(d.Available && !held[d.ID], d.Servings)})
		}
		w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(rescueMaxAge))
		WriteJSON(w, http.StatusOK, out)
	})
}
