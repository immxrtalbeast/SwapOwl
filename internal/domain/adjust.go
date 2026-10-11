package domain

import "time"

// InventoryPlan — результат сверки учёта с пересчётом: недостача списывается по FEFO,
// излишек становится новой партией.
type InventoryPlan struct {
	Shortage []Writeoff // партии и количества; пусто, если недостачи нет
	Surplus  Qty        // > 0 — излишек
}

// PlanInventory сверяет доступный остаток продукта (партии: remaining > 0 и now < ExpiresAt)
// с пересчитанным количеством. Меньше учёта — недостача по FEFO (ExpiresAt, ID), больше — излишек.
func PlanInventory(productID string, batches []Batch, counted Qty) InventoryPlan {
	var avail Qty
	for _, b := range batches {
		if b.ProductID == productID && b.Remaining > 0 {
			avail = satAdd(avail, b.Remaining)
		}
	}
	switch {
	case counted > avail:
		return InventoryPlan{Surplus: counted - avail}
	case counted < avail:
		plan, _ := PlanSale([]Need{{ProductID: productID, Qty: avail - counted}}, batches)
		return InventoryPlan{Shortage: plan}
	}
	return InventoryPlan{}
}

// Sold — сколько по заказу списано продажей из партии.
type Sold struct {
	BatchID   int64
	ProductID string
	Qty       Qty
	ExpiresAt time.Time
}

// PlanCancel делит проданное на то, что возвращается в партию (партия не просрочена:
// now < ExpiresAt), и то, что вернуть нельзя (партия просрочена). Порядок сохраняется.
func PlanCancel(sold []Sold, now time.Time) (returns, expired []Writeoff) {
	for _, s := range sold {
		if s.Qty <= 0 {
			continue
		}
		w := Writeoff{BatchID: s.BatchID, ProductID: s.ProductID, Qty: s.Qty}
		if now.Before(s.ExpiresAt) {
			returns = append(returns, w)
		} else {
			expired = append(expired, w)
		}
	}
	return returns, expired
}
