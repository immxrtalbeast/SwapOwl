package simulator

import (
	"context"
	"fmt"
	"math"
	"sort"

	"umirhack/internal/domain"
	"umirhack/internal/store"
)

// Типы сценариев «Что если».
const (
	WhatIfDiscount  = "discount"   // скидка на блюдо
	WhatIfOrderLess = "order_less" // закупить продукта меньше
	WhatIfDemand    = "demand"     // прямое изменение спроса на блюдо
)

// DiscountElasticity — допущение: спрос на блюдо растёт на 1,5 % на каждый 1 % скидки,
// то есть скидка 20 % поднимает спрос в 1,3 раза. Величина выбрана по порядку, а не измерена:
// как и подъём u рекомендации в спецификация (2.7, «НАДЕЮСЬ»), она не подтверждена данными
// ресторана; в ответе её всегда называют словами, чтобы менеджер мог её оспорить.
const DiscountElasticity = 1.5

// DiscountFactor — во сколько раз скидка percent (в %) поднимает спрос; всегда ≥ 1.
func DiscountFactor(percent float64) float64 {
	if percent <= 0 {
		return 1
	}
	return 1 + DiscountElasticity*percent/100
}

// WhatIfScenario — один сценарий. Percent и Factor заполняются по типу.
type WhatIfScenario struct {
	Type      string
	DishID    string
	ProductID string
	Percent   float64
	Factor    float64
}

// WhatIfProduct — списание по продукту: в базе и в сценарии, копейки.
type WhatIfProduct struct {
	ProductID     string
	ProductName   string
	BaseMinor     int64
	ScenarioMinor int64
}

// WhatIfOutcome — итог одного сценария.
type WhatIfOutcome struct {
	Scenario    WhatIfScenario
	TotalMinor  int64
	Products    []WhatIfProduct
	Assumptions []string
}

// WhatIfResult — база и сценарии (в порядке запроса).
type WhatIfResult struct {
	HorizonDays  int
	BaseMinor    int64
	BaseProducts []WhatIfProduct // ScenarioMinor == BaseMinor
	Outcomes     []WhatIfOutcome
}

// WhatIf пересчитывает прогноз списаний на copy снимка для каждого сценария независимо.
// Снимок не меняется; случайности нет, поэтому один снимок и сценарии дают один результат.
// Сценарий, не меняющий ни склад, ни спрос, даёт ровно базовый прогноз.
func WhatIf(ctx context.Context, snap store.Snapshot, horizonDays int, scenarios []WhatIfScenario) (WhatIfResult, error) {
	hours := horizonDays * 24
	base := store.ForecastWriteoffs(snap, hours)
	res := WhatIfResult{HorizonDays: horizonDays, BaseMinor: base.TotalMinor}
	baseBy := map[string]int64{}
	names := map[string]string{}
	for _, p := range base.Products {
		baseBy[p.ProductID] = p.TotalMinor
		names[p.ProductID] = p.Name
		res.BaseProducts = append(res.BaseProducts, WhatIfProduct{ProductID: p.ProductID, ProductName: p.Name,
			BaseMinor: p.TotalMinor, ScenarioMinor: p.TotalMinor})
	}
	for _, sc := range scenarios {
		if err := ctx.Err(); err != nil {
			return WhatIfResult{}, err
		}
		cp, notes, err := applyScenario(snap, sc)
		if err != nil {
			return WhatIfResult{}, err
		}
		f := store.ForecastWriteoffs(cp, hours)
		out := WhatIfOutcome{Scenario: sc, TotalMinor: f.TotalMinor, Assumptions: notes}
		seen := map[string]bool{}
		for _, p := range f.Products {
			seen[p.ProductID] = true
			names[p.ProductID] = p.Name
			out.Products = append(out.Products, WhatIfProduct{ProductID: p.ProductID, ProductName: p.Name,
				BaseMinor: baseBy[p.ProductID], ScenarioMinor: p.TotalMinor})
		}
		for id, v := range baseBy {
			if !seen[id] {
				out.Products = append(out.Products, WhatIfProduct{ProductID: id, ProductName: names[id], BaseMinor: v})
			}
		}
		sort.Slice(out.Products, func(i, j int) bool {
			a, b := out.Products[i], out.Products[j]
			if a.BaseMinor != b.BaseMinor || a.ScenarioMinor != b.ScenarioMinor {
				return max(a.BaseMinor, a.ScenarioMinor) > max(b.BaseMinor, b.ScenarioMinor)
			}
			return a.ProductID < b.ProductID
		})
		res.Outcomes = append(res.Outcomes, out)
	}
	return res, nil
}

// cloneSnapshot глубоко копирует изменяемые части снимка (партии и карты спроса).
func cloneSnapshot(s store.Snapshot) store.Snapshot {
	c := s
	c.Batches = append([]domain.Batch(nil), s.Batches...)
	c.RateHour = copyMap(s.RateHour)
	c.RateDay = copyMap(s.RateDay)
	c.Lambda = copyMap(s.Lambda)
	return c
}

func copyMap(m map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// applyScenario возвращает изменённую копию снимка и допущения словами.
func applyScenario(snap store.Snapshot, sc WhatIfScenario) (store.Snapshot, []string, error) {
	cp := cloneSnapshot(snap)
	switch sc.Type {
	case WhatIfDiscount:
		f := DiscountFactor(sc.Percent)
		name, err := scaleDemand(&cp, sc.DishID, f)
		if err != nil {
			return cp, nil, err
		}
		return cp, []string{
			fmt.Sprintf("Скидка %s%% на «%s» поднимает спрос в %s раза (эластичность %s — допущение, не измерение).",
				num(sc.Percent), name, num(f), num(DiscountElasticity)),
			"Цена и выручка в расчёте не участвуют: считаются только списания по сроку.",
		}, nil
	case WhatIfDemand:
		name, err := scaleDemand(&cp, sc.DishID, sc.Factor)
		if err != nil {
			return cp, nil, err
		}
		return cp, []string{fmt.Sprintf("Спрос на «%s» меняется в %s раза от текущей оценки.", name, num(sc.Factor))}, nil
	case WhatIfOrderLess:
		name, removed, err := cutStock(&cp, sc.ProductID, sc.Percent)
		if err != nil {
			return cp, nil, err
		}
		return cp, []string{
			fmt.Sprintf("Закупка «%s» меньше на %s%%: из остатка убирается %s%% (%d баз. ед.), начиная с самых свежих партий.",
				name, num(sc.Percent), num(sc.Percent), removed),
			"Это приближение: в снимке нет будущих поставок, поэтому «заказать меньше» = меньше свежего запаса.",
		}, nil
	}
	return cp, nil, fmt.Errorf("simulator: неизвестный тип сценария %q", sc.Type)
}

// scaleDemand умножает спрос на блюдо в f раз и пересчитывает нормы продуктов от разницы,
// чтобы базовые rate_p_час снимка остались как есть.
func scaleDemand(s *store.Snapshot, dishID string, f float64) (string, error) {
	var dish *domain.Dish
	for i := range s.Dishes {
		if s.Dishes[i].ID == dishID {
			dish = &s.Dishes[i]
		}
	}
	if dish == nil {
		return "", fmt.Errorf("simulator: неизвестное блюдо %q", dishID)
	}
	delta := map[string]float64{dishID: s.Lambda[dishID] * (f - 1)}
	s.Lambda[dishID] *= f
	for pid, perDay := range domain.ProductDemandPerDay([]domain.Dish{*dish}, delta) {
		s.RateDay[pid] = math.Max(0, s.RateDay[pid]+perDay)
		s.RateHour[pid] = math.Max(0, s.RateHour[pid]+domain.RatePerHour(perDay, s.Schedule))
	}
	return dish.Name, nil
}

// cutStock убирает percent остатка продукта, сначала из партий с самым поздним сроком.
func cutStock(s *store.Snapshot, productID string, percent float64) (string, domain.Qty, error) {
	ref, ok := s.Products[productID]
	if !ok {
		return "", 0, fmt.Errorf("simulator: неизвестный продукт %q", productID)
	}
	var total domain.Qty
	idx := []int{}
	for i, b := range s.Batches {
		if b.ProductID == productID {
			total += b.Remaining
			idx = append(idx, i)
		}
	}
	cut := domain.Qty(math.Round(float64(total) * percent / 100))
	sort.Slice(idx, func(a, b int) bool {
		x, y := s.Batches[idx[a]], s.Batches[idx[b]]
		if !x.ExpiresAt.Equal(y.ExpiresAt) {
			return x.ExpiresAt.After(y.ExpiresAt)
		}
		return x.ID > y.ID
	})
	left := cut
	for _, i := range idx {
		take := min(left, s.Batches[i].Remaining)
		s.Batches[i].Remaining -= take
		left -= take
	}
	kept := s.Batches[:0]
	for _, b := range s.Batches {
		if b.Remaining > 0 || b.ProductID != productID {
			kept = append(kept, b)
		}
	}
	s.Batches = kept
	return ref.Name, cut, nil
}

// num печатает число по-русски: без хвостовых нулей, запятая.
func num(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	for len(s) > 0 && s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	s = trimDot(s)
	for i := range s {
		if s[i] == '.' {
			return s[:i] + "," + s[i+1:]
		}
	}
	return s
}

func trimDot(s string) string {
	if len(s) > 0 && s[len(s)-1] == '.' {
		return s[:len(s)-1]
	}
	return s
}
