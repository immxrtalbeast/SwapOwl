package domain

import (
	"math"
	"sort"
)

// PlanWriteoff строит план FEFO по (ExpiresAt, ID). Партии уже отфильтрованы
// по remaining > 0 и now < ExpiresAt. Возвращает либо план, либо нехватку — не оба.
//
// Потребность по одному продукту, повторённая в needs, суммируется. План упорядочен по
// продукту, внутри продукта — по порядку FEFO; следующая партия берётся только после
// исчерпания предыдущей. Нехватка перечисляет все продукты, которых не хватает, по id.
func PlanWriteoff(needs []Need, batches []Batch) ([]Writeoff, []Shortage) {
	required := map[string]Qty{}
	for _, n := range needs {
		if n.Qty <= 0 {
			continue
		}
		required[n.ProductID] = satAdd(required[n.ProductID], n.Qty)
	}
	byProduct := map[string][]Batch{}
	for _, b := range batches {
		if b.Remaining > 0 {
			byProduct[b.ProductID] = append(byProduct[b.ProductID], b)
		}
	}
	ids := make([]string, 0, len(required))
	for id := range required {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var plan []Writeoff
	var short []Shortage
	for _, id := range ids {
		bs := byProduct[id]
		sort.Slice(bs, func(i, j int) bool {
			if !bs[i].ExpiresAt.Equal(bs[j].ExpiresAt) {
				return bs[i].ExpiresAt.Before(bs[j].ExpiresAt)
			}
			return bs[i].ID < bs[j].ID
		})
		var avail Qty
		for _, b := range bs {
			avail = satAdd(avail, b.Remaining)
		}
		need := required[id]
		if avail < need {
			short = append(short, Shortage{ProductID: id, Required: need, Available: avail, Missing: need - avail})
			continue
		}
		left := need
		for _, b := range bs {
			if left == 0 {
				break
			}
			take := b.Remaining
			if take > left {
				take = left
			}
			plan = append(plan, Writeoff{BatchID: b.ID, ProductID: id, Qty: take})
			left -= take
		}
	}
	if len(short) > 0 {
		return nil, short
	}
	return plan, nil
}

func satAdd(a, b Qty) Qty {
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}
