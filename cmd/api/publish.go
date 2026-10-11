package main

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"umirhack/internal/api"
	"umirhack/internal/domain"
	"umirhack/internal/events"
	"umirhack/internal/store"
)

// maxExpiredIDs — сколько id партий несёт batch.expired (7.5).
const maxExpiredIDs = 50

// publisher превращает результаты операций склада в события SSE. Хендлеры о шине не знают:
// публикация — обёртки над зависимостями Stock и Orders.
type publisher struct {
	bus  *events.Bus
	menu api.MenuSource
	stop *events.StopTracker
	now  func() time.Time
	log  *slog.Logger

	// recheckMu держит снимок меню и Update вместе: иначе устаревший снимок мог бы
	// примениться после свежего и записать в трекер неверный стоп-лист.
	recheckMu sync.Mutex
}

func newPublisher(bus *events.Bus, menu api.MenuSource, now func() time.Time, log *slog.Logger) *publisher {
	return &publisher{bus: bus, menu: menu, stop: events.NewStopTracker(bus), now: now, log: log}
}

// prime запоминает стоп-лист на старте, чтобы первое изменение дало корректную разность.
func (p *publisher) prime(ctx context.Context) { p.recheckStopList(ctx) }

// stockChanged публикует stock.changed и сверяет стоп-лист (разность до и после).
func (p *publisher) stockChanged(ctx context.Context, productIDs []string) {
	p.bus.Publish(events.StockChanged, map[string]any{"product_ids": productIDs})
	p.recheckStopList(ctx)
}

func (p *publisher) recheckStopList(ctx context.Context) {
	p.recheckMu.Lock()
	defer p.recheckMu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	snap, err := p.menu.MenuSnapshot(ctx, p.now().UTC())
	if err != nil {
		p.log.Warn("events: stop-list snapshot", slog.String("error", err.Error()))
		return
	}
	stopped := domain.StopList(snap.Dishes, snap.Available)
	ids := make([]string, 0, len(stopped))
	for _, s := range stopped {
		ids = append(ids, s.Dish.ID)
	}
	p.stop.Update(ids)
}

// onExpiring — janitor.OnExpiring: batch.expiring по одному на партию (7.5). Каждая партия
// приходит сюда ровно один раз: метку в БД ставит тот же проход, что вернул список.
func (p *publisher) onExpiring(expiring []store.ExpiringBatch) {
	for _, e := range expiring {
		p.bus.Publish(events.BatchExpiring, map[string]any{
			"batch_id": e.BatchID, "product_id": e.ProductID, "expires_at": e.ExpiresAt})
	}
}

// onSweep — janitor.OnSweep: одно batch.expired на проход.
func (p *publisher) onSweep(expired []store.ExpiredBatch) {
	batchIDs := make([]int64, 0, maxExpiredIDs)
	products := make([]string, 0, len(expired))
	for i, e := range expired {
		if i < maxExpiredIDs {
			batchIDs = append(batchIDs, e.BatchID)
		}
		products = append(products, e.ProductID)
	}
	products = uniqSorted(products)
	p.bus.Publish(events.BatchExpired, map[string]any{
		"batch_ids": batchIDs, "product_ids": products, "count": len(expired)})
	p.stockChanged(context.Background(), products)
}

type pubStock struct {
	api.Stock
	p *publisher
}

// ReceiveDelivery публикует delivery.accepted только для новой поставки (не replay).
func (s pubStock) ReceiveDelivery(ctx context.Context, in store.DeliveryInput, now func() time.Time, loc *time.Location) (store.DeliveryResponse, error) {
	resp, err := s.Stock.ReceiveDelivery(ctx, in, now, loc)
	if err != nil || resp.Replayed {
		return resp, err
	}
	products := make([]string, 0, len(resp.Batches))
	for _, b := range resp.Batches {
		products = append(products, b.ProductID)
	}
	products = uniqSorted(products)
	s.p.bus.Publish(events.DeliveryAccepted, map[string]any{"delivery_id": resp.ID, "product_ids": products})
	s.p.stockChanged(ctx, products)
	return resp, nil
}

type pubCatalog struct {
	api.Catalog
	p *publisher
}

// ImportCatalog после записи (не dry-run) сверяет стоп-лист: смена техкарт меняет его состав,
// а остатки не трогает, поэтому stock.changed не нужен (7.5).
func (c pubCatalog) ImportCatalog(ctx context.Context, in store.CatalogInput, dryRun bool) (store.CatalogDiff, error) {
	diff, err := c.Catalog.ImportCatalog(ctx, in, dryRun)
	if err == nil && !dryRun {
		c.p.recheckStopList(ctx)
	}
	return diff, err
}

type pubOrders struct {
	api.Orders
	p *publisher
}

// PlaceOrder публикует order.accepted / order.rejected для нового заказа (не replay).
func (o pubOrders) PlaceOrder(ctx context.Context, in store.OrderInput, now func() time.Time) (store.OrderResponse, error) {
	resp, err := o.Orders.PlaceOrder(ctx, in, now)
	if err != nil || resp.Replayed {
		return resp, err
	}
	var products []string
	typ := events.OrderAccepted
	if resp.Status == store.OrderRejected {
		typ = events.OrderRejected
		for _, m := range resp.Missing {
			products = append(products, m.ProductID)
		}
	} else {
		for _, w := range resp.Writeoffs {
			products = append(products, w.ProductID)
		}
	}
	products = uniqSorted(products)
	o.p.bus.Publish(typ, map[string]any{"order_id": resp.ID, "product_ids": products})
	o.p.stockChanged(ctx, products)
	return resp, nil
}

// uniqSorted — без повторов, по возрастанию, не nil (в JSON — [], не null).
func uniqSorted(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
