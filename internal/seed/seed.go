package seed

import (
	"fmt"
	"time"

	"umirhack/internal/domain"
)

// Product — продукт справочника.
type Product struct {
	ID                string
	Name              string
	Unit              domain.Unit
	PriceMinorPer1000 int64 // копеек за 1000 базовых единиц
	LeadDays          int
	PackSize          domain.Qty
	ShelfLifeHours    int // срок хранения по умолчанию для поставок
}

// Dish — блюдо с рецептом и априорным спросом.
type Dish struct {
	ID         string
	Name       string
	PriceMinor int64
	PriorDaily float64 // ожидаемое число порций в день
	Recipe     []domain.RecipeItem
}

// Hours — часы работы ресторана в его часовом поясе.
type Hours struct {
	TZ      string
	OpenAt  time.Duration // от начала суток
	CloseAt time.Duration // от начала суток
}

// DeliveryItem — позиция поставки в виде, принимаемом API:
// количество — десятичная строка с единицей g|kg|ml|l|pcs.
type DeliveryItem struct {
	ProductID      string
	Name           string
	Quantity       string
	Unit           string
	ShelfLifeHours int
}

// Delivery — поставка как значение.
type Delivery struct {
	ID        string
	Number    string
	CreatedAt time.Time
	Items     []DeliveryItem
}

// WorkingHours — часы работы демо-ресторана (по умолчанию 10:00–23:00, Москва).
func WorkingHours() Hours {
	return Hours{TZ: "Europe/Moscow", OpenAt: 10 * time.Hour, CloseAt: 23 * time.Hour}
}

// Products возвращает справочник продуктов (каждый вызов — новая копия).
func Products() []Product {
	return []Product{
		{"salmon", "Лосось", domain.UnitG, 180000, 2, 1000, 72},
		{"cream", "Сливки", domain.UnitMl, 25000, 1, 1000, 96},
		{"pasta", "Паста", domain.UnitG, 18000, 3, 1000, 4320},
		{"chicken", "Куриное филе", domain.UnitG, 32000, 1, 1000, 96},
		{"tomato", "Томаты", domain.UnitG, 20000, 1, 1000, 120},
		{"lettuce", "Салат романо", domain.UnitG, 40000, 1, 500, 72},
		{"parmesan", "Пармезан", domain.UnitG, 120000, 3, 500, 720},
		{"beef", "Говяжья вырезка", domain.UnitG, 210000, 2, 1000, 96},
		{"potato", "Картофель", domain.UnitG, 6000, 3, 5000, 336},
		{"bun", "Булочка", domain.UnitPcs, 1500, 1, 20, 72},
		{"egg", "Яйцо", domain.UnitPcs, 1200, 2, 30, 360},
		{"milk", "Молоко", domain.UnitMl, 9000, 1, 1000, 120},
	}
}

// Dishes возвращает блюда с рецептами (каждый вызов — новая копия).
func Dishes() []Dish {
	r := func(p string, q domain.Qty) domain.RecipeItem {
		return domain.RecipeItem{ProductID: p, Quantity: q}
	}
	return []Dish{
		{"salmon-pasta", "Паста с лососем", 69000, 20, []domain.RecipeItem{r("salmon", 120), r("pasta", 100), r("cream", 80)}},
		{"caesar", "Цезарь с курицей", 52000, 25, []domain.RecipeItem{r("chicken", 100), r("lettuce", 80), r("parmesan", 20), r("egg", 1)}},
		{"tomato-salad", "Салат с томатами", 38000, 15, []domain.RecipeItem{r("tomato", 150), r("lettuce", 40)}},
		{"beef-steak", "Стейк из вырезки", 129000, 10, []domain.RecipeItem{r("beef", 220), r("potato", 200)}},
		{"burger", "Бургер", 59000, 22, []domain.RecipeItem{r("beef", 120), r("bun", 1), r("tomato", 40), r("lettuce", 20)}},
		{"omelette", "Омлет", 29000, 18, []domain.RecipeItem{r("egg", 3), r("milk", 60), r("parmesan", 10)}},
	}
}

// StartingDeliveries — стартовые поставки: две партии за 3 и 27 часов до t0,
// покрывают примерно двое суток априорного спроса.
func StartingDeliveries(t0 time.Time) []Delivery {
	prods := Products()
	need := DailyNeed()
	var out []Delivery
	for i, ago := range []time.Duration{27 * time.Hour, 3 * time.Hour} {
		d := Delivery{
			ID:        fmt.Sprintf("D-SEED-%d", i+1),
			Number:    fmt.Sprintf("S%d", i+1),
			CreatedAt: t0.Add(-ago),
		}
		for _, p := range prods {
			qty := RoundToPack(domain.Qty(need[p.ID]+0.999), p.PackSize)
			if qty == 0 {
				qty = p.PackSize
			}
			q, u := FormatQty(qty, p.Unit)
			d.Items = append(d.Items, DeliveryItem{p.ID, p.Name, q, u, p.ShelfLifeHours})
		}
		out = append(out, d)
	}
	return out
}

// DailyNeed — ожидаемая суточная потребность в базовых единицах по априорному спросу.
func DailyNeed() map[string]float64 {
	need := map[string]float64{}
	for _, d := range Dishes() {
		for _, it := range d.Recipe {
			need[it.ProductID] += d.PriorDaily * float64(it.Quantity)
		}
	}
	return need
}

// RoundToPack округляет вверх до целого числа упаковок.
func RoundToPack(q, pack domain.Qty) domain.Qty {
	if pack <= 0 {
		return q
	}
	return (q + pack - 1) / pack * pack
}

// FormatQty записывает количество в базовых единицах строкой для API.
// Крупные граммы и миллилитры записываются в kg / l, чтобы разбор единиц
// проверялся на реальных данных; результат — всегда точная десятичная запись.
func FormatQty(q domain.Qty, base domain.Unit) (string, string) {
	switch base {
	case domain.UnitG:
		if q >= 1000 {
			return decimal(int64(q), 1000), "kg"
		}
		return fmt.Sprintf("%d", q), "g"
	case domain.UnitMl:
		if q >= 1000 {
			return decimal(int64(q), 1000), "l"
		}
		return fmt.Sprintf("%d", q), "ml"
	default:
		return fmt.Sprintf("%d", q), "pcs"
	}
}

func decimal(v, div int64) string {
	whole, frac := v/div, v%div
	if frac == 0 {
		return fmt.Sprintf("%d", whole)
	}
	s := fmt.Sprintf("%03d", frac)
	for s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	return fmt.Sprintf("%d.%s", whole, s)
}
