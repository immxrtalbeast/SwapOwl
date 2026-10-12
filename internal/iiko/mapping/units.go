package mapping

import (
	"math"
	"strings"

	"umirhack/internal/iiko"
)

// maxPackUnitLen — предел метки упаковки в нашем API.
const maxPackUnitLen = 32

// PieceWeightOf переводит вес штуки из iiko (кг) в граммы. Только для продуктов в «шт»;
// иначе или при весе ≤ 0 ok == false. [ПРЕДПОЛОЖЕНО]: поле unitWeight не сверено с документацией iiko.
func PieceWeightOf(u Unit, unitWeightKg float64) (grams int64, ok bool) {
	if u.Base != "pcs" || math.IsNaN(unitWeightKg) || unitWeightKg <= 0 {
		return 0, false
	}
	g := int64(math.Round(unitWeightKg * 1000))
	return g, g >= 1
}

// PackOf берёт первую действующую фасовку: размер упаковки в базовых единицах и её название.
// Фасовка без названия, с количеством мельче базовой единицы или с дробным числом штук пропускается.
// [ПРЕДПОЛОЖЕНО]: структура containers не сверена с документацией iiko.
func PackOf(u Unit, containers []iiko.Container) (size int64, label string, ok bool) {
	for _, c := range containers {
		name := strings.TrimSpace(c.Name)
		if c.Deleted || name == "" || len(name) > maxPackUnitLen || !(c.Count > 0) {
			continue
		}
		v := c.Count * float64(u.Factor)
		n := math.Round(v)
		if n < 1 || n > 1e12 || math.Abs(v-n) > 1e-6 {
			continue
		}
		return int64(n), name, true
	}
	return 0, "", false
}

// applyPiecesAndPacks заполняет вес штуки и упаковку продукта каталога, если iiko их отдала.
// Размер упаковки заменяет кратность заказа PackSize: фасовка поставщика — и есть кратность.
func applyPiecesAndPacks(cp *CatalogProduct, p iiko.Product, u Unit) {
	if g, ok := PieceWeightOf(u, p.UnitWeight); ok {
		cp.PieceWeight, cp.PieceWeightUnit = g, "g"
	}
	if size, label, ok := PackOf(u, p.Containers); ok {
		cp.PackSize, cp.PackUnit = size, label
	}
}
