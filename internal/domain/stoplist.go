package domain

// Servings — сколько порций собирается из available: min по ингредиентам floor(доступно / норма).
// Блюдо без ингредиентов или с неположительной нормой собрать нельзя — 0.
func Servings(recipe []RecipeItem, available map[string]Qty) int64 {
	if len(recipe) == 0 {
		return 0
	}
	var min int64 = -1
	for _, it := range recipe {
		if it.Quantity <= 0 {
			return 0
		}
		have := available[it.ProductID]
		if have < 0 {
			have = 0
		}
		n := int64(have / it.Quantity)
		if min < 0 || n < min {
			min = n
		}
	}
	return min
}

// StopList возвращает блюда, для которых нельзя собрать ни одной порции, в порядке входа.
// Missing — по каждому недостающему продукту: на одну порцию нужно Required, есть Available.
// Чистая функция остатков: сам стоп-лист нигде не хранится.
func StopList(dishes []Dish, available map[string]Qty) []StoppedDish {
	var out []StoppedDish
	for _, d := range dishes {
		if Servings(d.Recipe, available) > 0 {
			continue
		}
		sd := StoppedDish{Dish: d, Missing: []Shortage{}}
		for _, it := range d.Recipe {
			have := available[it.ProductID]
			if have < 0 {
				have = 0
			}
			if have < it.Quantity {
				sd.Missing = append(sd.Missing, Shortage{
					ProductID: it.ProductID, Required: it.Quantity, Available: have, Missing: it.Quantity - have,
				})
			}
		}
		out = append(out, sd)
	}
	return out
}
