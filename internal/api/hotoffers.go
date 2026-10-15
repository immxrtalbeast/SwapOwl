package api

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/hotoffers"
	"umirhack/internal/hotoffers/a"
	"umirhack/internal/hotoffers/b"
	"umirhack/internal/store"
)

// HotOffersSource — общий снимок склада (store.Snapshotter).
type HotOffersSource interface {
	Snapshot(ctx context.Context, now time.Time) (store.Snapshot, error)
}

// HotOffersConfig — настройки ручки: основной алгоритм (HOTOFFERS_ALGO) и параметры алгоритмов.
type HotOffersConfig struct {
	DefaultAlgo string // "a" | "b"; пусто — "a"
	Rank        hotoffers.Config
}

// OfferJSON — горячее предложение в ответе (7.4).
type OfferJSON struct {
	DishID         string          `json:"dish_id"`
	DishName       string          `json:"dish_name"`
	Priority       float64         `json:"priority"`
	TargetPortions int64           `json:"target_portions"`
	TargetToday    int64           `json:"target_today"`
	UsualPortions  int64           `json:"usual_portions"`
	SellUntil      time.Time       `json:"sell_until"`
	Reasons        []HotReasonJSON `json:"reasons"`
	Explanation    string          `json:"explanation"`
	// MarginMinor — маржа порции (цена блюда − закупочная стоимость рецепта), копейки; нет цены блюда или продукта — поля нет.
	MarginMinor *int64 `json:"margin_minor,omitempty"`
	// ExpectedMarginMinor — MarginMinor × TargetToday: сколько маржи принесут порции, которые надо продать сегодня.
	ExpectedMarginMinor *int64 `json:"expected_margin_minor,omitempty"`
	// Pinned — менеджер ещё и закрепил это блюдо; ManagerNote — его подсказка для зала.
	Pinned      bool   `json:"pinned,omitempty"`
	ManagerNote string `json:"manager_note,omitempty"`
}

// HotReasonJSON — ссылка на партию в предложении.
type HotReasonJSON struct {
	ProductID        string         `json:"product_id"`
	ProductName      string         `json:"product_name"`
	BatchID          int64          `json:"batch_id"`
	AtRisk           store.QtyJSON  `json:"at_risk"`
	ProductAtRisk    *store.QtyJSON `json:"product_at_risk,omitempty"` // Bg: риск продукта во всех партиях горизонта
	RiskBatches      int            `json:"risk_batches,omitempty"`    // Bg: партий под риском; batch_id — ближайшая
	ExpiresAt        time.Time      `json:"expires_at"`
	WorkingHoursLeft float64        `json:"working_hours_left"`
}

// ExcludedJSON — блюдо, снятое предохранителем Bg.
type ExcludedJSON struct {
	DishID    string `json:"dish_id"`
	ProductID string `json:"product_id"`
	Why       string `json:"why"`
}

// HotOffersResponse — тело GET /api/hot-offers.
type HotOffersResponse struct {
	Algo     string         `json:"algo"`
	Offers   []OfferJSON    `json:"offers"`
	Excluded []ExcludedJSON `json:"excluded"`
	// ManagerPicks — блюда, добавленные менеджером вручную (FS-159), которых нет в расчёте; отдельно от offers,
	// потому что у них нет партии-причины. Блюдо из стоп-листа сюда не попадает — оно в excluded.
	ManagerPicks []ManagerPickJSON `json:"manager_picks"`
	// Hidden — блюда, скрытые менеджером: из offers убраны.
	Hidden []HiddenJSON `json:"hidden"`
}

// ManagerPickJSON — блюдо, закреплённое менеджером.
type ManagerPickJSON struct {
	DishID   string    `json:"dish_id"`
	DishName string    `json:"dish_name"`
	Note     string    `json:"note"`
	SetAt    time.Time `json:"set_at"`
	SetBy    string    `json:"set_by"`
}

// HiddenJSON — блюдо, скрытое менеджером.
type HiddenJSON struct {
	DishID   string    `json:"dish_id"`
	DishName string    `json:"dish_name"`
	InOffers bool      `json:"in_offers"` // без скрытия алгоритм поставил бы его в список
	SetAt    time.Time `json:"set_at"`
	SetBy    string    `json:"set_by"`
}

// HotOverrides — решения менеджера по горячим предложениям (FS-159). nil — ручной правки нет.
type HotOverrides interface {
	ListHotOverrides(ctx context.Context) ([]store.HotOverride, error)
	SetHotOverride(ctx context.Context, o store.HotOverride) (store.HotOverride, error)
	DeleteHotOverride(ctx context.Context, dishID string) error
}

// applyOverrides — решения менеджера поверх расчёта: скрытые убираются из offers, закреплённые помечаются
// или идут в manager_picks; закреплённое блюдо из стоп-листа — в excluded с причиной. Чистая функция.
func applyOverrides(resp HotOffersResponse, overrides []store.HotOverride, names map[string]string, stopped map[string]bool) HotOffersResponse {
	resp.ManagerPicks, resp.Hidden = []ManagerPickJSON{}, []HiddenJSON{}
	byDish := make(map[string]store.HotOverride, len(overrides))
	for _, o := range overrides {
		byDish[o.DishID] = o
	}
	offers := resp.Offers[:0:0]
	inOffers := map[string]bool{}
	for _, of := range resp.Offers {
		inOffers[of.DishID] = true
		o, ok := byDish[of.DishID]
		switch {
		case ok && o.Mode == store.HotHide:
			continue
		case ok && o.Mode == store.HotPin:
			of.Pinned, of.ManagerNote = true, o.Note
		}
		offers = append(offers, of)
	}
	resp.Offers = offers
	for _, o := range overrides {
		name := names[o.DishID]
		if name == "" {
			name = o.DishID
		}
		switch {
		case o.Mode == store.HotHide:
			resp.Hidden = append(resp.Hidden, HiddenJSON{DishID: o.DishID, DishName: name, InOffers: inOffers[o.DishID], SetAt: o.SetAt, SetBy: o.SetBy})
		case inOffers[o.DishID]:
		case stopped[o.DishID]:
			resp.Excluded = append(resp.Excluded, ExcludedJSON{DishID: o.DishID, Why: name + " закреплено менеджером, но сейчас в стоп-листе — не предлагаем"})
		default:
			resp.ManagerPicks = append(resp.ManagerPicks, ManagerPickJSON{DishID: o.DishID, DishName: name, Note: o.Note, SetAt: o.SetAt, SetBy: o.SetBy})
		}
	}
	return resp
}

func (rt *Router) hotOffersRoutes() {
	if rt.deps.HotOffers == nil {
		return
	}
	rt.Handle(http.MethodGet, "/api/hot-offers", rt.getHotOffers)
	if rt.deps.HotOverrides != nil {
		rt.Handle(http.MethodGet, "/api/hot-offers/overrides", rt.listHotOverrides)
		rt.Handle(http.MethodPut, "/api/hot-offers/overrides/{dish_id}", rt.putHotOverride)
		rt.Handle(http.MethodDelete, "/api/hot-offers/overrides/{dish_id}", rt.deleteHotOverride)
	}
}

// getHotOffers — GET /api/hot-offers?algo=a|b. Пересчёт при каждом чтении на свежем снимке БД;
// состояния списка нет, порядок задаёт алгоритм (детерминирован).
func (rt *Router) getHotOffers(w http.ResponseWriter, r *http.Request) {
	cfg := rt.deps.HotConfig
	algo := r.URL.Query().Get("algo")
	if algo == "" {
		algo = cfg.DefaultAlgo
	}
	if algo == "" {
		algo = "a"
	}
	if algo != "a" && algo != "b" {
		validationError(w, "algo", "algo must be a or b")
		return
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
	resp.ManagerPicks, resp.Hidden = []ManagerPickJSON{}, []HiddenJSON{}
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
	}
	WriteJSON(w, http.StatusOK, resp)
}

// buildHotOffers — чистая сборка ответа: алгоритм + тексты. Без обращений к БД и часам.
func buildHotOffers(snap store.Snapshot, algo string, cfg hotoffers.Config) HotOffersResponse {
	rank := a.Rank
	if algo == "b" {
		rank = b.Rank
	}
	in := hotoffers.Input{Now: snap.Now, Batches: snap.Batches, Dishes: snap.Dishes, Stopped: snap.Stopped,
		Price: snap.Price, RateHour: snap.RateHour, Schedule: snap.Schedule, Config: cfg}
	offers, excluded := rank(in)

	loc := snap.Schedule.Location
	if loc == nil {
		loc = time.UTC
	}
	names := map[string]string{}
	for _, d := range snap.Dishes {
		names[d.ID] = d.Name
	}
	dayHours := (snap.Schedule.CloseAt - snap.Schedule.OpenAt).Hours()

	resp := HotOffersResponse{Algo: algo, Offers: []OfferJSON{}, Excluded: []ExcludedJSON{}}
	for _, o := range offers {
		row := OfferJSON{DishID: o.DishID, DishName: names[o.DishID], Priority: o.Priority,
			TargetPortions: o.TargetPortions, Reasons: []HotReasonJSON{}}
		for _, rs := range o.Reasons {
			p := snap.Products[rs.ProductID]
			row.Reasons = append(row.Reasons, HotReasonJSON{ProductID: rs.ProductID, ProductName: p.Name,
				BatchID: rs.BatchID, AtRisk: store.QtyJSON{Qty: int64(rs.AtRisk), Unit: p.Unit}, RiskBatches: rs.Batches,
				ProductAtRisk: productRisk(rs.ProductAtRisk, p.Unit),
				ExpiresAt:     rs.ExpiresAt.In(loc), WorkingHoursLeft: rs.WorkingHoursLeft})
			if row.SellUntil.IsZero() || rs.ExpiresAt.Before(row.SellUntil) {
				row.SellUntil = rs.ExpiresAt.In(loc)
			}
		}
		if dayHours > 0 {
			// Обычный спрос: λ_d порций в день, делённые на рабочие часы дня, на рабочие часы до sell_until.
			row.UsualPortions = int64(math.Round(snap.Lambda[o.DishID] / dayHours *
				domain.WorkingHours(snap.Now, row.SellUntil, snap.Schedule)))
		}
		row.TargetToday = targetToday(o.TargetPortions, row.SellUntil, snap)
		if m, ok := dishMargin(snap, o.DishID); ok {
			row.MarginMinor = &m
			em := m * row.TargetToday
			row.ExpectedMarginMinor = &em
		}
		row.Explanation = explainOffer(row, algo)
		resp.Offers = append(resp.Offers, row)
	}

	resp.Offers = tieBreakByMargin(resp.Offers)

	stock := snap.Available()
	if algo != "b" {
		// R сообщает о стоп-листе в excluded служебно; в контракте excluded — предохранитель Bg (7.4).
		excluded = nil
	}
	for _, e := range excluded {
		why := e.Why
		if rate := snap.RateHour[e.ProductID]; rate > 0 {
			why = fmt.Sprintf("%s: остатка %s хватит на %s при обычном спросе, а нужно не меньше %s",
				snap.Products[e.ProductID].Name,
				formatQty(int64(stock[e.ProductID]), snap.Products[e.ProductID].Unit),
				workingHours(float64(stock[e.ProductID])/rate), workingHours(cfg.CoverHours))
		}
		resp.Excluded = append(resp.Excluded, ExcludedJSON{DishID: e.DishID, ProductID: e.ProductID, Why: why})
	}
	return resp
}

// dishMargin — маржа порции в копейках: цена блюда − Σ норма × закупочная цена продукта / 1000 (вниз).
// Неизвестна (ok=false), если у блюда нет цены или у продукта рецепта нет цены. Отрицательная маржа возвращается как есть.
func dishMargin(snap store.Snapshot, dishID string) (int64, bool) {
	price := snap.DishPrice[dishID]
	if price <= 0 {
		return 0, false
	}
	for _, d := range snap.Dishes {
		if d.ID != dishID {
			continue
		}
		if len(d.Recipe) == 0 {
			return 0, false
		}
		var cost int64
		for _, it := range d.Recipe {
			p := snap.Price[it.ProductID]
			if p <= 0 {
				return 0, false
			}
			cost += int64(it.Quantity) * p / 1000
		}
		return price - cost, true
	}
	return 0, false
}

// tieBreakByMargin — порядок задаёт алгоритм (спецификация 2.7: при равном приоритете раньше срок опорной
// партии, затем id). Маржа решает только внутри подряд идущих предложений со строго равным приоритетом И равным
// сроком (sell_until): выше то, что приносит больше маржи (стабильно). Остальной порядок не меняется.
func tieBreakByMargin(offers []OfferJSON) []OfferJSON {
	for i := 0; i < len(offers); {
		j := i + 1
		for j < len(offers) && offers[j].Priority == offers[i].Priority && offers[j].SellUntil.Equal(offers[i].SellUntil) {
			j++
		}
		if j-i > 1 {
			group := offers[i:j]
			sort.SliceStable(group, func(a, b int) bool {
				ma, mb := int64(-1<<62), int64(-1<<62)
				if group[a].MarginMinor != nil {
					ma = *group[a].MarginMinor
				}
				if group[b].MarginMinor != nil {
					mb = *group[b].MarginMinor
				}
				return ma > mb
			})
		}
		i = j
	}
	return offers
}

// targetToday — часть цели, которую нужно продать до закрытия сегодня, пропорционально
// рабочим часам: ceil(target × min(W_today, W(now,e*)) / W(now,e*)).
func targetToday(target int64, sellUntil time.Time, snap store.Snapshot) int64 {
	if target <= 0 || sellUntil.IsZero() {
		return 0
	}
	closeToday := domain.ShiftEnd(snap.Now, snap.Schedule)
	wAll := domain.WorkingHours(snap.Now, sellUntil, snap.Schedule)
	if wAll <= 0 {
		return target
	}
	wToday := math.Min(domain.WorkingHours(snap.Now, closeToday, snap.Schedule), wAll)
	t := int64(math.Ceil(float64(target)*wToday/wAll - 1e-9))
	if t > target {
		t = target
	}
	return t
}

// explainOffer — объяснение по-русски с числами: продукт, партия, количество, рабочие часы.
func explainOffer(o OfferJSON, algo string) string {
	parts := make([]string, 0, len(o.Reasons)+1)
	for _, r := range o.Reasons {
		if algo == "b" {
			if r.RiskBatches > 1 && r.ProductAtRisk != nil {
				parts = append(parts, fmt.Sprintf("%s: %s в %d партиях не успеет разойтись (ближайшая — №%d, %s, за %s).",
					r.ProductName, formatQty(r.ProductAtRisk.Qty, r.ProductAtRisk.Unit), r.RiskBatches, r.BatchID,
					formatQty(r.AtRisk.Qty, r.AtRisk.Unit), workingHours(r.WorkingHoursLeft)))
			} else {
				parts = append(parts, fmt.Sprintf("%s, партия №%d: %s не успеет разойтись за %s.",
					r.ProductName, r.BatchID, formatQty(r.AtRisk.Qty, r.AtRisk.Unit), workingHours(r.WorkingHoursLeft)))
			}
		} else {
			parts = append(parts, fmt.Sprintf("%s, партия №%d: в запасе %s, срок через %s.",
				r.ProductName, r.BatchID, formatQty(r.AtRisk.Qty, r.AtRisk.Unit), workingHours(r.WorkingHoursLeft)))
		}
	}
	if o.TargetPortions > 0 {
		parts = append(parts, fmt.Sprintf("Цель — %s против обычных %d.", portions(o.TargetPortions), o.UsualPortions))
	}
	return strings.Join(parts, " ")
}

// formatQty — «1,4 кг», «350 г», «2 л», «3 шт»: русская запятая, крупные единицы от 1000.
func formatQty(q int64, u domain.Unit) string {
	switch u {
	case domain.UnitG:
		if q >= 1000 {
			return decimal(float64(q)/1000) + " кг"
		}
		return fmt.Sprintf("%d г", q)
	case domain.UnitMl:
		if q >= 1000 {
			return decimal(float64(q)/1000) + " л"
		}
		return fmt.Sprintf("%d мл", q)
	}
	return fmt.Sprintf("%d шт", q)
}

// decimal — число с одним знаком после запятой без хвостового нуля: 1,4; 2.
func decimal(x float64) string {
	s := strconv.FormatFloat(math.Round(x*10)/10, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return strings.Replace(s, ".", ",", 1)
}

// workingHours — «5 рабочих часов», «1 рабочий час», «2,5 рабочего часа».
func workingHours(h float64) string {
	r := math.Round(h*10) / 10
	if r != math.Trunc(r) {
		return decimal(r) + " рабочего часа"
	}
	n := int64(r)
	return fmt.Sprintf("%d %s", n, plural(n, "рабочий час", "рабочих часа", "рабочих часов"))
}

func portions(n int64) string {
	return fmt.Sprintf("%d %s", n, plural(n, "порция", "порции", "порций"))
}

func plural(n int64, one, few, many string) string {
	switch m10, m100 := n%10, n%100; {
	case m10 == 1 && m100 != 11:
		return one
	case m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14):
		return few
	}
	return many
}

// BuildHotOffers — тот же расчёт, что у GET /api/hot-offers, для отпечатка списка (hotoffers.changed).
func BuildHotOffers(snap store.Snapshot, algo string, cfg hotoffers.Config) HotOffersResponse {
	return buildHotOffers(snap, algo, cfg)
}

// productRisk — риск продукта для ответа; нуль (алгоритм R его не считает) — поле не выводится.
func productRisk(q domain.Qty, unit domain.Unit) *store.QtyJSON {
	if q <= 0 {
		return nil
	}
	return &store.QtyJSON{Qty: int64(q), Unit: unit}
}
