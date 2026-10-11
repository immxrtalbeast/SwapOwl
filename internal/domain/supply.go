package domain

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// SupplyUrgency — срочность предложения поставки.
type SupplyUrgency string

// Срочность предложения поставки.
const (
	UrgencyNow     SupplyUrgency = "now"
	UrgencySoon    SupplyUrgency = "soon"
	UrgencyPlanned SupplyUrgency = "planned"
)

// SupplyProduct — продукт для расчёта предложения поставки. Вызывающий передаёт только
// продукты, входящие хотя бы в один рецепт.
type SupplyProduct struct {
	ID            string
	Name          string
	LeadDays      int
	PackSize      Qty
	UsableStock   Qty     // Σ (remaining − risk)
	RatePerDay    float64 // ожидаемая потребность, базовых единиц в день
	MaxRecipeNorm Qty     // наибольшая норма среди блюд
	BlocksDishes  int     // сколько блюд из стоп-листа блокирует
}

// SupplyInput — вход SupplySuggestions.
type SupplyInput struct {
	Now         time.Time
	Products    []SupplyProduct
	CoverDays   int
	MinPortions int64
}

// SupplySuggestion — предложение поставки.
type SupplySuggestion struct {
	ProductID    string
	ProductName  string
	Quantity     Qty
	UsableStock  Qty
	RatePerDay   Qty
	StockoutAt   *time.Time // nil при нулевом темпе
	BlocksDishes int
	Urgency      SupplyUrgency
	Explanation  string
}

// maxStockoutDays ограничивает дату обнуления, чтобы time.Duration не переполнился.
const maxStockoutDays = 100 * 365

// SupplySuggestions — предложение необходимых поставок (solution.md, 2.6).
// к заказу = max(0, rate·(срок доставки + дни покрытия) − запас), вверх до кратного упаковке;
// при нулевом темпе — MinPortions·наибольшая норма − запас. Строки с нулём не возвращаются.
// Порядок: блокирующие блюда из стоп-листа, затем дата обнуления (nil — последними), затем id.
func SupplySuggestions(in SupplyInput) []SupplySuggestion {
	out := make([]SupplySuggestion, 0, len(in.Products))
	for _, p := range in.Products {
		usable := p.UsableStock
		if usable < 0 {
			usable = 0
		}
		rate := p.RatePerDay
		if rate < 0 || math.IsNaN(rate) {
			rate = 0
		}
		horizon := p.LeadDays + in.CoverDays

		var need float64
		zeroRate := rate == 0
		if zeroRate {
			need = float64(in.MinPortions)*float64(p.MaxRecipeNorm) - float64(usable)
		} else {
			need = math.Ceil(rate*float64(horizon)) - float64(usable)
		}
		if need <= 0 {
			continue
		}
		pack := p.PackSize
		if pack <= 0 {
			pack = 1
		}
		packs := math.Ceil(need / float64(pack))
		if packs*float64(pack) >= math.MaxInt64 {
			continue
		}
		qty := Qty(packs) * pack

		s := SupplySuggestion{
			ProductID: p.ID, ProductName: p.Name, Quantity: qty, UsableStock: usable,
			RatePerDay: Qty(math.Ceil(rate)), BlocksDishes: p.BlocksDishes,
		}
		stockoutDays := math.Inf(1)
		if !zeroRate {
			stockoutDays = float64(usable) / rate
			if stockoutDays > maxStockoutDays {
				stockoutDays = maxStockoutDays
			}
			t := in.Now.Add(time.Duration(stockoutDays * float64(day)))
			s.StockoutAt = &t
		}

		switch {
		case p.BlocksDishes > 0 || stockoutDays < float64(p.LeadDays):
			s.Urgency = UrgencyNow
		case stockoutDays < float64(horizon):
			s.Urgency = UrgencySoon
		default:
			s.Urgency = UrgencyPlanned
		}
		s.Explanation = explainSupply(p, s, zeroRate, horizon, in.MinPortions, stockoutDays)
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.BlocksDishes > 0) != (b.BlocksDishes > 0) {
			return a.BlocksDishes > 0
		}
		if (a.StockoutAt == nil) != (b.StockoutAt == nil) {
			return a.StockoutAt != nil
		}
		if a.StockoutAt != nil && !a.StockoutAt.Equal(*b.StockoutAt) {
			return a.StockoutAt.Before(*b.StockoutAt)
		}
		return a.ProductID < b.ProductID
	})
	return out
}

func explainSupply(p SupplyProduct, s SupplySuggestion, zeroRate bool, horizon int, minPortions int64, stockoutDays float64) string {
	var txt string
	if zeroRate {
		txt = fmt.Sprintf("Расхода нет; минимальный запас — %d порций × норма %d = %d, пригодный запас %d. К заказу %d (упаковка %d).",
			minPortions, int64(p.MaxRecipeNorm), minPortions*int64(p.MaxRecipeNorm), int64(s.UsableStock), int64(s.Quantity), int64(p.PackSize))
	} else {
		txt = fmt.Sprintf("Расход ~%d в день, пригодный запас %d хватит на %.1f сут.; нужно на %d сут. (доставка %d + покрытие %d). К заказу %d (упаковка %d).",
			int64(s.RatePerDay), int64(s.UsableStock), stockoutDays, horizon, p.LeadDays, horizon-p.LeadDays, int64(s.Quantity), int64(p.PackSize))
	}
	if p.BlocksDishes > 0 {
		txt += fmt.Sprintf(" Блокирует блюд из стоп-листа: %d.", p.BlocksDishes)
	}
	return txt
}
