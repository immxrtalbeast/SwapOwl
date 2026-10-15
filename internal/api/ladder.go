package api

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"umirhack/internal/store"
)

// Ступени «лестницы спасения партии».
const (
	LadderSell     = "sell"     // продать горячим предложением
	LadderDiscount = "discount" // отдать со скидкой гостю (витрина «Спаси продукты»)
	LadderPrep     = "prep"     // переработать в заготовку: пока не реализовано
	LadderTransfer = "transfer" // передать в соседнюю точку: считает хаб сети

	ladderRoadmap = "roadmap"
	ladderNetwork = "network"
)

// LadderStepJSON — одна ступень: что спасёт, чего это стоит и во что обойдётся. Ступени без расчёта несут status.
type LadderStepJSON struct {
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	SavedMinor int64  `json:"saved_minor"`
	CostMinor  int64  `json:"cost_minor"`
	NetMinor   int64  `json:"net_minor"`
	Status     string `json:"status,omitempty"`
	Detail     string `json:"detail"`
}

// LadderBatchJSON — партия под риском и её ступени.
type LadderBatchJSON struct {
	BatchID     int64         `json:"batch_id"`
	ProductID   string        `json:"product_id"`
	ProductName string        `json:"product_name"`
	AtRisk      store.QtyJSON `json:"at_risk"`
	AtRiskMinor int64         `json:"at_risk_minor"`
	ExpiresAt   time.Time     `json:"expires_at"`
	Best        string        `json:"best"`
	// CombinedMinor — сколько удастся не списать, если пройти ступени по порядку: сначала продать, остаток со скидкой.
	CombinedMinor int64            `json:"combined_minor"`
	Steps         []LadderStepJSON `json:"steps"`
}

// ladderIn — вход решателя: всё готовое, никаких обращений к БД и часам.
type ladderIn struct {
	Alerts []store.AlertView
	Offers []OfferJSON
	Rescue []RescueJSON
	Snap   store.Snapshot
	Loc    *time.Location
}

// norm — норма продукта на порцию блюда.
func (in ladderIn) norm(dishID, productID string) int64 {
	for _, d := range in.Snap.Dishes {
		if d.ID != dishID {
			continue
		}
		for _, it := range d.Recipe {
			if it.ProductID == productID {
				return int64(it.Quantity)
			}
		}
	}
	return 0
}

func minI64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// buildLadder — для каждой истекающей партии считает ступени «продать» и «со скидкой» в копейках и называет лучшую.
// Продажа: порции горячего предложения, которые движок просит продать до срока, покрывают часть партии;
// скидка: витринные порции покрывают остаток, а скидка (цена порции × процент) — цена этой ступени.
// Заготовка и передача в точку сети — без расчёта (status). Чистая функция.
func buildLadder(in ladderIn) []LadderBatchJSON {
	out := []LadderBatchJSON{}
	for _, a := range in.Alerts {
		if a.Status != "expiring" || a.Remaining.Qty <= 0 {
			continue
		}
		price := in.Snap.Price[a.ProductID] // копеек за 1000 базовых единиц; 0 — цена неизвестна
		money := func(q int64) int64 { return q * price / 1000 }
		row := LadderBatchJSON{BatchID: a.BatchID, ProductID: a.ProductID, ProductName: a.ProductName, AtRisk: a.Remaining,
			AtRiskMinor: money(a.Remaining.Qty), ExpiresAt: a.ExpiresAt, Steps: []LadderStepJSON{}}

		// 1. Продать: лучшее предложение (первое по порядку) с этой партией в причинах.
		sell := LadderStepJSON{Kind: LadderSell, Title: "Продать горячим"}
		var sellDish string
		var covered int64
		for _, o := range in.Offers {
			has := false
			for _, r := range o.Reasons {
				if r.BatchID == a.BatchID {
					has = true
					break
				}
			}
			n := in.norm(o.DishID, a.ProductID)
			if !has || n <= 0 {
				continue
			}
			portions := o.TargetToday
			if portions <= 0 {
				portions = o.TargetPortions
			}
			covered = minI64(a.Remaining.Qty, portions*n)
			sellDish = o.DishID
			sell.Detail = fmt.Sprintf("%s — %d порц. до %s", o.DishName, portions, o.SellUntil.In(in.Loc).Format("15:04"))
			break
		}
		switch {
		case sellDish == "":
			sell.Status, sell.Detail = "unavailable", "нет подходящего блюда в горячих предложениях"
		case price <= 0:
			sell.Status = "no_price"
		default:
			sell.SavedMinor = money(covered)
			sell.NetMinor = sell.SavedMinor
		}
		row.Steps = append(row.Steps, sell)

		// 2. Скидка: витринное блюдо с этим продуктом покрывает то, что не покрыла продажа.
		disc := LadderStepJSON{Kind: LadderDiscount, Title: "Скидка гостю"}
		remainder := a.Remaining.Qty - covered
		var discDone bool
		if remainder > 0 && price > 0 {
			best := -1
			var bestCover int64
			for i, r := range in.Rescue {
				n := in.norm(r.DishID, a.ProductID)
				// Порции витрины того же блюда уже входят в цель продажи: второй раз их не засчитываем.
				if n <= 0 || r.ServingsLeft <= 0 || r.DishID == sellDish {
					continue
				}
				if c := minI64(remainder, r.ServingsLeft*n); c > bestCover {
					best, bestCover = i, c
				}
			}
			if best >= 0 {
				r := in.Rescue[best]
				n := in.norm(r.DishID, a.ProductID)
				portions := (bestCover + n - 1) / n
				dishPrice := in.Snap.DishPrice[r.DishID]
				disc.SavedMinor = money(bestCover)
				disc.Detail = fmt.Sprintf("%s: −%d %%, %d порц.", r.DishName, r.DiscountPercent, portions)
				if dishPrice > 0 {
					disc.CostMinor = portions * dishPrice * int64(r.DiscountPercent) / 100
					disc.NetMinor = disc.SavedMinor - disc.CostMinor
				} else {
					disc.Status = "no_price"
				}
				discDone = true
			}
		}
		if !discDone && disc.Detail == "" {
			disc.Status = "unavailable"
			switch {
			case remainder <= 0:
				disc.Detail = "продажа покрывает партию целиком"
			case price <= 0:
				disc.Detail = "нет цены продукта"
			default:
				disc.Detail = "нет блюда на витрине с этим продуктом"
			}
		}
		row.Steps = append(row.Steps, disc)

		// 3 и 4: без расчёта в этой версии.
		row.Steps = append(row.Steps,
			LadderStepJSON{Kind: LadderPrep, Title: "Переработать в заготовку", Status: ladderRoadmap, Detail: "продлит срок партии; пока не в этой версии"},
			LadderStepJSON{Kind: LadderTransfer, Title: "Передать в соседнюю точку", Status: ladderNetwork, Detail: "оценка — на экране «Сеть»"},
		)

		var bestNet int64
		for _, s := range row.Steps {
			if s.Status == "" && s.NetMinor > bestNet {
				bestNet, row.Best = s.NetMinor, s.Kind
			}
		}
		for _, s := range row.Steps[:2] {
			if s.Status == "" && s.NetMinor > 0 {
				row.CombinedMinor += s.NetMinor
			}
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].AtRiskMinor > out[j].AtRiskMinor })
	return out
}

func (rt *Router) ladderRoutes() {
	if rt.deps.Monitor == nil || rt.deps.HotOffers == nil {
		return
	}
	rt.Handle(http.MethodGet, "/api/rescue-ladder", rt.getRescueLadder)
}

// getRescueLadder — GET /api/rescue-ladder (viewer): для партий под риском — ступени спасения в копейках и лучшая.
// Только чтение; те же данные, что у экранов «Горячие», «Сегодня» и витрины «Спаси продукты».
func (rt *Router) getRescueLadder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := rt.deps.Now().UTC()
	alerts, err := rt.deps.Monitor.ListAlerts(ctx, now)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	snap, err := rt.deps.HotOffers.Snapshot(ctx, now)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	if err := rt.withHolds(ctx, &snap); err != nil {
		rt.internalError(w, r, err)
		return
	}
	cfg := rt.deps.HotConfig
	algo := cfg.DefaultAlgo
	if algo != "a" && algo != "b" {
		algo = "a"
	}
	resp := buildHotOffers(snap, algo, cfg.Rank)
	hidden := map[string]bool{}
	if rt.deps.HotOverrides != nil {
		overrides, err := rt.deps.HotOverrides.ListHotOverrides(ctx)
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
	loc := rt.deps.Location
	if loc == nil {
		loc = time.UTC
	}
	WriteJSON(w, http.StatusOK, buildLadder(ladderIn{Alerts: alerts, Offers: resp.Offers,
		Rescue: rescueRows(snap, resp, hidden), Snap: snap, Loc: loc}))
}
