package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"umirhack/internal/domain"
)

// SupplyMeta — срок доставки и упаковка продукта (поля справочника).
type SupplyMeta struct {
	LeadDays int
	PackSize domain.Qty
	// ShelfLifeHours — обычный срок поставщика из карточки (сопоставление с данными поставщика); nil — неизвестен.
	// В расчёт не входит: строка предложения передаёт его в поставку явно (FS-160).
	ShelfLifeHours *int
}

// SupplyData — всё, что нужно ручке предложения поставок: вход чистой функции и справочник.
type SupplyData struct {
	Input    domain.SupplyInput
	Products map[string]ProductRef
	Meta     map[string]SupplyMeta
}

// SupplyReader собирает вход domain.SupplySuggestions из снимка и справочника продуктов.
type SupplyReader struct {
	DB          *DB
	Snap        Snapshotter
	CoverDays   int
	MinPortions int64
}

// SupplyData читает снимок и срок доставки/упаковку продуктов и собирает вход расчёта на момент now.
func (r SupplyReader) SupplyData(ctx context.Context, now time.Time) (SupplyData, error) {
	snap, err := r.Snap.Snapshot(ctx, now)
	if err != nil {
		return SupplyData{}, err
	}
	meta, err := r.DB.supplyMeta(ctx)
	if err != nil {
		return SupplyData{}, err
	}
	in := BuildSupplyInput(snap, meta, r.CoverDays, r.MinPortions)
	return SupplyData{Input: in, Products: snap.Products, Meta: meta}, nil
}

func (d *DB) supplyMeta(ctx context.Context) (map[string]SupplyMeta, error) {
	rows, err := d.Pool.Query(ctx, `SELECT id, lead_days, pack_size, shelf_life_hours FROM products`)
	if err != nil {
		return nil, fmt.Errorf("store: supply meta: %w", err)
	}
	defer rows.Close()
	out := map[string]SupplyMeta{}
	for rows.Next() {
		var id string
		var m SupplyMeta
		var pack int64
		if err := rows.Scan(&id, &m.LeadDays, &pack, &m.ShelfLifeHours); err != nil {
			return nil, fmt.Errorf("store: supply meta: %w", err)
		}
		m.PackSize = domain.Qty(pack)
		out[id] = m
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: supply meta: %w", err)
	}
	return out, nil
}

// BuildSupplyInput — чистая сборка входа: только продукты из рецептов; пригодный запас = Σ(remaining − risk)
// по проекции FEFO со спросом rate_p_час; блокировка — блюда из стоп-листа, которым продукта не хватает на порцию.
func BuildSupplyInput(snap Snapshot, meta map[string]SupplyMeta, coverDays int, minPortions int64) domain.SupplyInput {
	maxNorm := map[string]domain.Qty{}
	blocks := map[string]int{}
	avail := snap.Available()
	for _, d := range snap.Dishes {
		for _, it := range d.Recipe {
			if it.Quantity > maxNorm[it.ProductID] {
				maxNorm[it.ProductID] = it.Quantity
			}
			if snap.Stopped[d.ID] && avail[it.ProductID] < it.Quantity {
				blocks[it.ProductID]++
			}
		}
	}
	byProduct := map[string][]domain.Batch{}
	for _, b := range snap.Batches {
		byProduct[b.ProductID] = append(byProduct[b.ProductID], b)
	}
	ids := make([]string, 0, len(maxNorm))
	for id := range maxNorm {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	in := domain.SupplyInput{Now: snap.Now, CoverDays: coverDays, MinPortions: minPortions}
	for _, id := range ids {
		var usable domain.Qty
		batches := byProduct[id]
		remaining := map[int64]domain.Qty{}
		for _, b := range batches {
			remaining[b.ID] = b.Remaining
		}
		for _, r := range domain.RiskByBatch(batches, snap.RateHour[id], snap.Now, snap.Schedule) {
			usable += remaining[r.BatchID] - r.Risk
		}
		m, ok := meta[id]
		if !ok {
			m = SupplyMeta{PackSize: 1}
		}
		in.Products = append(in.Products, domain.SupplyProduct{
			ID: id, Name: snap.Products[id].Name, LeadDays: m.LeadDays, PackSize: m.PackSize,
			UsableStock: usable, RatePerDay: snap.RateDay[id], MaxRecipeNorm: maxNorm[id], BlocksDishes: blocks[id],
		})
	}
	return in
}
