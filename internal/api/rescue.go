package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"umirhack/internal/domain"
)

const (
	// rescueDiscountPercent — скидка экрана «Сегодня» (frontend: ACTION_LABELS.discount, «скидка 20 % на блюдо»).
	rescueDiscountPercent = 20
	rescueMinDiscount     = 5
	rescueMaxDiscount     = 50
	rescueMaxRows         = 50
	rescueMaxAge          = 15 // секунд, Cache-Control
	rescueNote            = "Скидка на блюдо дня"
	rescuePerMinute       = 60 // запросов в минуту с одного адреса
)

// RescueJSON — строка публичной витрины «Спаси продукты». Только то, что видно гостю.
type RescueJSON struct {
	DishID          string    `json:"dish_id"`
	DishName        string    `json:"dish_name"`
	ServingsLeft    int64     `json:"servings_left"`
	DiscountPercent int       `json:"discount_percent"`
	ValidUntil      time.Time `json:"valid_until"`
	Note            string    `json:"note"`
}

// ipLimiter — скользящее окно по адресу; память ограничена очисткой устаревших записей.
type ipLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
	max  int
}

func (l *ipLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-time.Minute)
	if len(l.hits) > 10000 {
		for k, v := range l.hits {
			if len(v) == 0 || !v[len(v)-1].After(cut) {
				delete(l.hits, k)
			}
		}
	}
	kept := l.hits[ip][:0:0]
	for _, t := range l.hits[ip] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[ip] = kept
		return false
	}
	l.hits[ip] = append(kept, now)
	return true
}

func (rt *Router) rescueRoutes() {
	if !rt.deps.PublicRescue || rt.deps.HotOffers == nil {
		return
	}
	lim := &ipLimiter{hits: map[string][]time.Time{}, max: rescuePerMinute}
	rt.HandlePublic(http.MethodGet, "/api/public/rescue", func(w http.ResponseWriter, r *http.Request) {
		ip := rescueClientIP(r)
		if !lim.allow(ip, time.Now()) {
			w.Header().Set("Retry-After", "60")
			WriteError(w, http.StatusTooManyRequests, CodeOverloaded, "too many requests", nil)
			return
		}
		rt.getRescue(w, r)
	})
}

// getRescue — GET /api/public/rescue: блюда из горячих предложений, которые стоит заказать со скидкой.
// Только чтение; внутренних данных (партии, себестоимость, причины, остатки) в ответе нет.
func (rt *Router) getRescue(w http.ResponseWriter, r *http.Request) {
	cfg := rt.deps.HotConfig
	algo := cfg.DefaultAlgo
	if algo != "a" && algo != "b" {
		algo = "a"
	}
	snap, err := rt.deps.HotOffers.Snapshot(r.Context(), rt.deps.Now().UTC())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	if err := rt.withHolds(r.Context(), &snap); err != nil {
		rt.internalError(w, r, err)
		return
	}
	resp := buildHotOffers(snap, algo, cfg.Rank)
	hidden := map[string]bool{}
	if rt.deps.HotOverrides != nil {
		overrides, err := rt.deps.HotOverrides.ListHotOverrides(r.Context())
		if err != nil {
			rt.internalError(w, r, err)
			return
		}
		names := make(map[string]string, len(snap.Dishes))
		for _, d := range snap.Dishes {
			names[d.ID] = d.Name
		}
		resp = applyOverrides(resp, overrides, names, snap.Stopped)
		for _, h := range resp.Hidden {
			hidden[h.DishID] = true
		}
	}
	avail := snap.Available()
	recipes := make(map[string][]domain.RecipeItem, len(snap.Dishes))
	for _, d := range snap.Dishes {
		recipes[d.ID] = d.Recipe
	}
	out := []RescueJSON{}
	for _, o := range resp.Offers {
		if len(out) >= rescueMaxRows {
			break
		}
		if snap.Stopped[o.DishID] || hidden[o.DishID] {
			continue
		}
		// Действие «скидка» — как на экране «Сегодня»: цель больше двукратного обычного темпа.
		target := o.TargetToday
		if target <= 0 {
			target = o.TargetPortions
		}
		if target <= 0 || o.UsualPortions < 0 || target <= 2*o.UsualPortions {
			continue
		}
		real := max(2*o.UsualPortions, o.UsualPortions+1)
		left := target - real
		if left <= 0 {
			continue
		}
		left = min(left, domain.Servings(recipes[o.DishID], avail))
		if left <= 0 {
			continue
		}
		out = append(out, RescueJSON{DishID: o.DishID, DishName: o.DishName, ServingsLeft: left,
			DiscountPercent: min(max(rescueDiscountPercent, rescueMinDiscount), rescueMaxDiscount),
			ValidUntil:      o.SellUntil, Note: rescueNote})
	}
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(rescueMaxAge))
	WriteJSON(w, http.StatusOK, out)
}

// rescueClientIP — адрес гостя для лимита. За Caddy RemoteAddr — адрес прокси, поэтому для loopback и частных
// адресов берётся крайнее правое значение X-Forwarded-For (его дописывает сам прокси); с публичного адреса
// заголовок игнорируется — иначе клиент обошёл бы лимит, подставив свой.
func rescueClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !(peer.IsLoopback() || peer.IsPrivate()) {
		return host
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if last := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); last != nil {
		return last.String()
	}
	return host
}
