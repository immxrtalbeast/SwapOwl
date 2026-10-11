package domain

import "sort"

// Shortfall — недостача продукта при продаже из кассы: потребность, которой не нашлось на складе.
type Shortfall struct {
	ProductID string
	Qty       Qty
}

// PlanSale — план продажи из кассы (source: pos). Как PlanWriteoff, но недостача не отказ:
// FEFO по (ExpiresAt, ID) списывает всё доступное, остаток потребности по продукту — недостача.
// Партии уже отфильтрованы по remaining > 0 и now < ExpiresAt. План упорядочен по продукту,
// внутри продукта — по FEFO; недостачи — по id продукта, только с Qty > 0.
func PlanSale(needs []Need, batches []Batch) ([]Writeoff, []Shortfall) {
	required := map[string]Qty{}
	for _, n := range needs {
		if n.Qty > 0 {
			required[n.ProductID] = satAdd(required[n.ProductID], n.Qty)
		}
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
	var short []Shortfall
	for _, id := range ids {
		bs := byProduct[id]
		sort.Slice(bs, func(i, j int) bool {
			if !bs[i].ExpiresAt.Equal(bs[j].ExpiresAt) {
				return bs[i].ExpiresAt.Before(bs[j].ExpiresAt)
			}
			return bs[i].ID < bs[j].ID
		})
		left := required[id]
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
		if left > 0 {
			short = append(short, Shortfall{ProductID: id, Qty: left})
		}
	}
	return plan, short
}
