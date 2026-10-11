// Package mapping переводит данные iiko в тела запросов нашего API (docs/iiko.md, «Адаптер»).
// Чистые функции: ни сети, ни часов.
package mapping

import (
	"crypto/sha1" //nolint:gosec // UUID v5 определён через SHA-1
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"umirhack/internal/iiko"
)

// Defaults — значения, которых нет в iiko, но требует наш каталог.
type Defaults struct {
	PriceMinorPer1000 int64   // цена продукта, если в накладных её не нашлось
	LeadDays          int     // срок поставки поставщиком
	PackSize          int64   // кратность заказа поставщику, в базовых единицах
	PriorDaily        float64 // априорный дневной спрос блюда
}

// Unit — единица продукта: базовая в нашем API и основная в iiko.
type Unit struct {
	Base   string // g, ml, pcs
	Main   string // kg, l, pcs — единица количества в API
	Factor int64  // базовых единиц в основной
}

// UnitOf переводит основную единицу iiko (кг, л, шт).
func UnitOf(mainUnit string) (Unit, error) {
	switch mainUnit {
	case "кг":
		return Unit{"g", "kg", 1000}, nil
	case "л":
		return Unit{"ml", "l", 1000}, nil
	case "шт":
		return Unit{"pcs", "pcs", 1}, nil
	}
	return Unit{}, fmt.Errorf("неизвестная единица %q", mainUnit)
}

// Qty переводит количество в основной единице в целое число базовых единиц (строкой).
// Дробь мельче базовой единицы округляется; результат не меньше 0.
func Qty(v float64, u Unit) string {
	return strconv.FormatInt(int64(math.Round(v*float64(u.Factor))), 10)
}

// Catalog — тело PUT /api/catalog.
type Catalog struct {
	Products []CatalogProduct `json:"products"`
	Dishes   []CatalogDish    `json:"dishes"`
}

// CatalogProduct — продукт каталога.
type CatalogProduct struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Unit              string `json:"unit"`
	PriceMinorPer1000 int64  `json:"price_minor_per_1000"`
	LeadDays          int    `json:"lead_days"`
	PackSize          int64  `json:"pack_size"`
	ShelfLifeHours    int    `json:"shelf_life_hours,omitempty"`
}

// CatalogDish — блюдо каталога.
type CatalogDish struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	PriceMinor int64        `json:"price_minor"`
	PriorDaily float64      `json:"prior_daily"`
	Recipe     []RecipeItem `json:"recipe"`
}

// RecipeItem — строка рецепта.
type RecipeItem struct {
	ProductID string `json:"product_id"`
	Quantity  string `json:"quantity"`
	Unit      string `json:"unit"`
}

// BuildCatalog собирает каталог. prices — цена за основную единицу по id продукта (из накладных).
// Пропущенное (удалённое, без техкарты, с неизвестной единицей) попадает в skipped.
func BuildCatalog(products []iiko.Product, charts []iiko.AssemblyChart, prices map[string]float64, d Defaults) (Catalog, map[string]Unit, []string) {
	var skipped []string
	units := map[string]Unit{}
	cat := Catalog{Products: []CatalogProduct{}, Dishes: []CatalogDish{}}
	for _, p := range products {
		if p.Deleted || p.Type != "GOODS" {
			continue
		}
		u, err := UnitOf(p.MainUnit)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("продукт %s: %v", p.ID, err))
			continue
		}
		units[p.ID] = u
		price := d.PriceMinorPer1000
		if pr, ok := prices[p.ID]; ok && pr > 0 {
			price = int64(math.Round(pr * 100 * 1000 / float64(u.Factor)))
		}
		cat.Products = append(cat.Products, CatalogProduct{
			ID: p.ID, Name: p.Name, Unit: u.Base, PriceMinorPer1000: price,
			LeadDays: d.LeadDays, PackSize: d.PackSize, ShelfLifeHours: p.ShelfLifeHours,
		})
	}
	chartOf := map[string]iiko.AssemblyChart{}
	for _, c := range charts {
		if c.DateTo == nil { // действующая техкарта
			chartOf[c.AssembledProductID] = c
		}
	}
	for _, p := range products {
		if p.Deleted || p.Type != "DISH" {
			continue
		}
		c, ok := chartOf[p.ID]
		if !ok || len(c.Items) == 0 {
			skipped = append(skipped, fmt.Sprintf("блюдо %s: нет действующей техкарты", p.ID))
			continue
		}
		dish := CatalogDish{ID: p.ID, Name: p.Name, PriceMinor: int64(math.Round(p.Price * 100)), PriorDaily: d.PriorDaily, Recipe: []RecipeItem{}}
		amount := c.AssembledAmount
		if amount <= 0 {
			amount = 1
		}
		okDish := true
		for _, it := range c.Items {
			u, known := units[it.ProductID]
			if !known || it.AmountIn <= 0 {
				skipped = append(skipped, fmt.Sprintf("блюдо %s: строка техкарты %s не принята", p.ID, it.ProductID))
				okDish = false
				break
			}
			q := Qty(it.AmountIn/amount, u)
			if q == "0" { // норма не меньше одной базовой единицы
				q = "1"
			}
			dish.Recipe = append(dish.Recipe, RecipeItem{ProductID: it.ProductID, Quantity: q, Unit: u.Base})
		}
		if okDish {
			cat.Dishes = append(cat.Dishes, dish)
		}
	}
	return cat, units, skipped
}

// Prices — последняя цена за основную единицу по продуктам из накладных (порядок — по дате).
func Prices(invs []iiko.IncomingInvoice) map[string]float64 {
	sorted := append([]iiko.IncomingInvoice{}, invs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].DateIncoming < sorted[j].DateIncoming })
	out := map[string]float64{}
	for _, inv := range sorted {
		for _, it := range inv.Items {
			if it.Price > 0 {
				out[it.Product] = it.Price
			}
		}
	}
	return out
}

// Delivery — тело POST /api/deliveries.
type Delivery struct {
	ID        string         `json:"id"`
	Number    string         `json:"number"`
	CreatedAt string         `json:"created_at"`
	Items     []DeliveryItem `json:"items"`
}

// DeliveryItem — позиция поставки.
type DeliveryItem struct {
	ProductID      string `json:"product_id"`
	Name           string `json:"name,omitempty"`
	Quantity       string `json:"quantity"`
	Unit           string `json:"unit"`
	ShelfLifeHours int    `json:"shelf_life_hours,omitempty"`
}

// BuildDelivery переводит приходную накладную. ok=false — накладную не передаём
// (не проведена или нет ни одной принятой позиции); причина в reason.
func BuildDelivery(inv iiko.IncomingInvoice, units map[string]Unit, shelf map[string]int, loc *time.Location) (d Delivery, reason string, ok bool) {
	if inv.Status != "PROCESSED" {
		return d, "статус " + inv.Status, false
	}
	at, err := time.ParseInLocation(iiko.DateTimeLayout, inv.DateIncoming, loc)
	if err != nil {
		return d, "дата " + inv.DateIncoming, false
	}
	d = Delivery{ID: inv.ID, Number: inv.DocumentNumber, CreatedAt: at.Format(time.RFC3339), Items: []DeliveryItem{}}
	for _, it := range inv.Items {
		u, known := units[it.Product]
		if !known || it.Amount <= 0 || Qty(it.Amount, u) == "0" {
			continue
		}
		d.Items = append(d.Items, DeliveryItem{ProductID: it.Product, Quantity: Qty(it.Amount, u), Unit: u.Base, ShelfLifeHours: shelf[it.Product]})
	}
	if len(d.Items) == 0 {
		return d, "нет известных позиций", false
	}
	return d, "", true
}

// Sale — строка продаж за день: порции блюда.
type Sale struct {
	Date     string // yyyy-MM-dd
	DishID   string
	Portions int64
}

// Order — тело POST /api/orders.
type Order struct {
	ID     string      `json:"id"`
	Source string      `json:"source"`
	Items  []OrderItem `json:"items"`
}

// OrderItem — строка заказа.
type OrderItem struct {
	DishID   string `json:"dish_id"`
	Portions int64  `json:"portions"`
}

// BuildOrders собирает по одному заказу `pos` на день: id — UUID v5 от «iiko-sales:<дата>».
// В OLAP-отчёте нет id заказов iiko, поэтому единицей идемпотентности служит день.
func BuildOrders(sales []Sale) []Order {
	byDate := map[string][]OrderItem{}
	for _, s := range sales {
		if s.Portions > 0 && s.DishID != "" {
			byDate[s.Date] = append(byDate[s.Date], OrderItem{DishID: s.DishID, Portions: s.Portions})
		}
	}
	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	var out []Order
	for _, d := range dates {
		items := byDate[d]
		sort.Slice(items, func(i, j int) bool { return items[i].DishID < items[j].DishID })
		out = append(out, Order{ID: OrderID(d), Source: "pos", Items: items})
	}
	return out
}

// OrderID — id заказа за день: UUID v5.
func OrderID(date string) string { return UUIDv5(Namespace, "iiko-sales:"+date) }

// Namespace — пространство имён UUID v5 адаптера (фиксированная константа).
var Namespace = [16]byte{0x6b, 0xa7, 0xb8, 0x10, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8}

// UUIDv5 — RFC 4122, версия 5.
func UUIDv5(ns [16]byte, name string) string {
	h := sha1.New() //nolint:gosec
	h.Write(ns[:])
	h.Write([]byte(name))
	s := h.Sum(nil)[:16]
	s[6] = s[6]&0x0f | 0x50
	s[8] = s[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", s[0:4], s[4:6], s[6:8], s[8:10], s[10:16])
}
