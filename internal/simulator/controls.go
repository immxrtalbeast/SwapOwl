package simulator

import (
	"sort"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/hotoffers"
)

// Контроли bench (solution.md, 2.7): все исключают блюда из стоп-листа и отдают
// не больше topN предложений. C0 — прогон без рекомендаций, см. Run.

// randomOffers — RND: случайные блюда. Случайность зависит только от
// (seed, заказ), не от алгоритма и склада, поэтому прогоны парные.
func randomOffers(seedValue int64, orderIdx int, dishes []domain.Dish, stopped map[string]bool, topN int) []hotoffers.Offer {
	var ids []string
	for _, d := range dishes {
		if !stopped[d.ID] {
			ids = append(ids, d.ID)
		}
	}
	sort.Strings(ids)
	var out []hotoffers.Offer
	for j := 0; j < topN && len(ids) > 0; j++ {
		k := int(hash(seedValue, int64(orderIdx), int64(900+j)) % uint64(len(ids)))
		out = append(out, hotoffers.Offer{DishID: ids[k]})
		ids = append(ids[:k], ids[k+1:]...)
	}
	return out
}

// popularOffers — POP: самые популярные блюда по оценке спроса (по убыванию, затем id).
func popularOffers(lambda map[string]float64, dishes []domain.Dish, stopped map[string]bool, topN int) []hotoffers.Offer {
	var ids []string
	for _, d := range dishes {
		if !stopped[d.ID] {
			ids = append(ids, d.ID)
		}
	}
	sort.Slice(ids, func(a, b int) bool {
		if lambda[ids[a]] != lambda[ids[b]] {
			return lambda[ids[a]] > lambda[ids[b]]
		}
		return ids[a] < ids[b]
	})
	return take(ids, topN)
}

// nearestOffers — C1: блюда, у которых есть доступная партия ингредиента с ближайшим
// сроком; порядок — срок, затем id блюда.
func nearestOffers(now time.Time, batches []domain.Batch, dishes []domain.Dish, recipes map[string]map[string]domain.Qty,
	stopped map[string]bool, topN int) []hotoffers.Offer {
	first := map[string]time.Time{} // продукт → срок ближайшей доступной партии
	for _, b := range batches {
		if t, ok := first[b.ProductID]; !ok || b.ExpiresAt.Before(t) {
			first[b.ProductID] = b.ExpiresAt
		}
	}
	key := map[string]time.Time{}
	var ids []string
	for _, d := range dishes {
		if stopped[d.ID] {
			continue
		}
		var best time.Time
		for pid := range recipes[d.ID] {
			if t, ok := first[pid]; ok && (best.IsZero() || t.Before(best)) {
				best = t
			}
		}
		if best.IsZero() {
			continue
		}
		key[d.ID] = best
		ids = append(ids, d.ID)
	}
	sort.Slice(ids, func(a, b int) bool {
		if !key[ids[a]].Equal(key[ids[b]]) {
			return key[ids[a]].Before(key[ids[b]])
		}
		return ids[a] < ids[b]
	})
	return take(ids, topN)
}

func take(ids []string, n int) []hotoffers.Offer {
	if len(ids) > n {
		ids = ids[:n]
	}
	out := make([]hotoffers.Offer, len(ids))
	for i, id := range ids {
		out[i] = hotoffers.Offer{DishID: id}
	}
	return out
}
