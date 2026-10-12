package seed

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/domain/units"
	"umirhack/internal/generator"
	"umirhack/internal/store"
)

// HistorySeed — фиксированный seed генератора для истории стенда.
const HistorySeed int64 = 20261010

// HistoryDays — глубина истории поставок и заказов.
const HistoryDays = 14

// Закупка истории — «до уровня» (как в FS-114, docs/pitch-numbers.md). Генератор привозит от 1 до 3
// суток априорного спроса независимо от остатка, поэтому «как есть» остатки копятся и портятся
// (FS-125: 185 тыс. ₽ просрочки в неделю). Расписание поставок берём у генератора, а количество
// пересчитываем: прогноз спроса на интервал до следующей поставки минус текущий непросроченный остаток.
const (
	// historyDemandRatio — доля априорного спроса, которую реально дают заказы генератора.
	historyDemandRatio = 0.75
	// historyReserve — запас сверх прогноза; подобран так, чтобы списания недели были в коридоре
	// 4–10 % себестоимости проданного.
	historyReserve = 1.2
	// Каждая historyShortEvery-я позиция приходит со сроком historyShortHours (в FS-114 — 20 % поставок).
	historyShortEvery = 5
	historyShortHours = 36
)

// history — строки таблиц, посчитанные в памяти и согласованные с журналом движений.
type history struct {
	deliveries []hDelivery
	batches    []hBatch // id партии = индекс + 1
	orders     []hOrder
	moves      []hMove
}

type hDelivery struct {
	ID, Number         string
	CreatedAt, Receive time.Time
	Hash               []byte
	Resp               store.DeliveryResponse
}

type hBatch struct {
	ID           int64
	DeliveryID   string
	Line         int
	ProductID    string
	Quantity     int64
	Remaining    int64
	ReceivedAt   time.Time
	ExpiresAt    time.Time
	WrittenOffAt *time.Time
	// Demo — партия, заданная условиями демо-сценария: заказы истории её не трогают.
	Demo bool
}

type hOrder struct {
	ID     string
	Status string
	At     time.Time
	Hash   []byte
	Items  []store.OrderItem
	Resp   store.OrderResponse
}

type hMove struct {
	BatchID   int64
	ProductID string
	Kind      string
	Reason    string
	Qty       int64
	OrderID   string
	At        time.Time
}

// buildHistory проигрывает стартовые поставки и HistoryDays суток событий генератора
// до t0 на пустом складе. Заказы списывают по FEFO через domain.PlanWriteoff,
// истёкшие партии списываются движением writeoff/expired со временем expires_at.
func buildHistory(t0 time.Time) (*history, error) {
	t0 = t0.UTC()
	events, err := generator.Generate(HistorySeed, t0.Add(-HistoryDays*24*time.Hour), t0)
	if err != nil {
		return nil, err
	}
	// Лосось и сливки, не истёкшие к t0, — только партии демо-сценария: поставки истории
	// с такими позициями приходят без них, а заказы истории демо-партии не расходуют.
	demo := DemoProductIDs()
	for ei := range events {
		e := &events[ei]
		if e.Kind != generator.KindDelivery {
			continue
		}
		d := *e.Delivery
		d.Items = nil
		for _, it := range e.Delivery.Items {
			if demo[it.ProductID] && e.At.Add(time.Duration(it.ShelfLifeHours)*time.Hour).After(t0) {
				continue
			}
			d.Items = append(d.Items, it)
		}
		e.Delivery = &d
	}
	for _, d := range StartingDeliveries(t0) {
		d := d
		events = append(events, generator.Event{At: d.CreatedAt, Kind: generator.KindDelivery, Delivery: &d})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })

	prods := map[string]Product{}
	for _, p := range Products() {
		prods[p.ID] = p
	}
	recipes := map[string][]domain.RecipeItem{}
	for _, d := range Dishes() {
		recipes[d.ID] = d.Recipe
	}

	h := &history{}
	sweep := func(now time.Time) {
		for i := range h.batches {
			b := &h.batches[i]
			if b.Remaining > 0 && !b.ExpiresAt.After(now) {
				at := b.ExpiresAt
				h.moves = append(h.moves, hMove{b.ID, b.ProductID, "writeoff", "expired", b.Remaining, "", at})
				b.Remaining, b.WrittenOffAt = 0, &at
			}
		}
	}
	for _, e := range events {
		at := e.At.UTC()
		sweep(at)
		switch e.Kind {
		case generator.KindDelivery:
			d, seeded := e.Delivery, strings.HasPrefix(e.Delivery.ID, "D-SEED-")
			if !seeded {
				d = h.procure(d, at, prods)
			}
			if len(d.Items) == 0 {
				continue // закупать нечего — поставки не было
			}
			err = h.addDelivery(d, at, prods, seeded)
		case generator.KindOrder:
			err = h.addOrder(e.Order, at, prods, recipes)
		}
		if err != nil {
			return nil, err
		}
	}
	sweep(t0)
	sort.SliceStable(h.moves, func(i, j int) bool { return h.moves[i].At.Before(h.moves[j].At) })
	return h, nil
}

// procure заменяет количества в поставке истории закупкой «до уровня» по текущим остаткам.
func (h *history) procure(d *generator.Delivery, at time.Time, prods map[string]Product) *generator.Delivery {
	need := DailyNeed()
	stock := map[string]float64{}
	for _, b := range h.batches {
		if !b.Demo && at.Before(b.ExpiresAt) {
			stock[b.ProductID] += float64(b.Remaining)
		}
	}
	out := *d
	out.Items = nil
	for i, it := range d.Items {
		p := prods[it.ProductID]
		days := 1.0
		if p.ShelfLifeHours > 100 { // поставки нескоропортящихся приходят примерно через день
			days = 2
		}
		want := need[p.ID]*historyDemandRatio*days*historyReserve - stock[p.ID]
		if want <= 0 {
			continue
		}
		q, u := FormatQty(RoundToPack(domain.Qty(math.Ceil(want)), p.PackSize), p.Unit)
		it.Quantity, it.Unit = q, u
		if (int(at.Unix()/86400)+i)%historyShortEvery == 0 && it.ShelfLifeHours > historyShortHours {
			it.ShelfLifeHours = historyShortHours // остатки поставщика с коротким сроком
		}
		out.Items = append(out.Items, it)
	}
	return &out
}

func (h *history) addDelivery(d *generator.Delivery, at time.Time, prods map[string]Product, seeded bool) error {
	in := store.DeliveryInput{ID: d.ID, Number: d.Number, CreatedAt: d.CreatedAt.UTC()}
	resp := store.DeliveryResponse{ID: d.ID, Number: d.Number, CreatedAt: in.CreatedAt, ReceivedAt: at,
		Batches: []store.BatchJSON{}, Warnings: []store.WarningJSON{}}
	for i, it := range d.Items {
		p, ok := prods[it.ProductID]
		if !ok {
			return fmt.Errorf("seed: поставка %s: неизвестный продукт %q", d.ID, it.ProductID)
		}
		q, err := units.Parse(it.Quantity, it.Unit, units.Unit(p.Unit))
		if err != nil {
			return fmt.Errorf("seed: поставка %s: %w", d.ID, err)
		}
		in.Items = append(in.Items, store.DeliveryItem{ProductID: it.ProductID, Name: it.Name, Unit: p.Unit,
			Qty: int64(q), ShelfLifeHours: it.ShelfLifeHours})
		b := hBatch{ID: int64(len(h.batches) + 1), DeliveryID: d.ID, Line: i + 1, ProductID: it.ProductID,
			Quantity: int64(q), Remaining: int64(q), ReceivedAt: at,
			ExpiresAt: at.Add(time.Duration(it.ShelfLifeHours) * time.Hour),
			Demo:      seeded && DemoProductIDs()[it.ProductID]}
		h.batches = append(h.batches, b)
		h.moves = append(h.moves, hMove{b.ID, b.ProductID, "receipt", "", b.Quantity, "", at})
		resp.Batches = append(resp.Batches, store.BatchJSON{ID: b.ID, LineNo: b.Line, ProductID: b.ProductID,
			Quantity: store.QtyJSON{Qty: b.Quantity, Unit: p.Unit}, ReceivedAt: at, ExpiresAt: b.ExpiresAt, Status: "ok"})
	}
	h.deliveries = append(h.deliveries, hDelivery{ID: d.ID, Number: d.Number, CreatedAt: in.CreatedAt, Receive: at, Hash: in.BodyHash(), Resp: resp})
	return nil
}

func (h *history) addOrder(o *generator.Order, at time.Time, prods map[string]Product, recipes map[string][]domain.RecipeItem) error {
	in := store.OrderInput{ID: o.ID}
	for _, it := range o.Items {
		rec, ok := recipes[it.DishID]
		if !ok {
			return fmt.Errorf("seed: заказ %s: неизвестное блюдо %q", o.ID, it.DishID)
		}
		in.Items = append(in.Items, store.OrderItem{DishID: it.DishID, Portions: it.Portions})
		for _, r := range rec {
			q, err := units.Mul(units.Qty(r.Quantity), int64(it.Portions))
			if err != nil {
				return fmt.Errorf("seed: заказ %s: %w", o.ID, err)
			}
			in.Needs = append(in.Needs, domain.Need{ProductID: r.ProductID, Qty: domain.Qty(q)})
		}
	}
	var avail []domain.Batch
	for _, b := range h.batches {
		if !b.Demo && b.Remaining > 0 && at.Before(b.ExpiresAt) {
			avail = append(avail, domain.Batch{ID: b.ID, ProductID: b.ProductID, Quantity: domain.Qty(b.Quantity),
				Remaining: domain.Qty(b.Remaining), ReceivedAt: b.ReceivedAt, ExpiresAt: b.ExpiresAt})
		}
	}
	plan, short := domain.PlanWriteoff(in.Needs, avail)
	if short != nil {
		// Отклонённые заказы в историю не пишем: без свежих лосося и сливок их было бы много,
		// а сценарию нужны только принятые продажи.
		return nil
	}
	resp := store.OrderResponse{ID: o.ID, At: at, Items: in.Items, Writeoffs: []store.WriteoffJSON{}, Missing: []store.MissingJSON{}}
	resp.Status = store.OrderAccepted
	for _, w := range plan {
		b := &h.batches[w.BatchID-1]
		b.Remaining -= int64(w.Qty)
		h.moves = append(h.moves, hMove{b.ID, b.ProductID, "sale", "", int64(w.Qty), o.ID, at})
		resp.Writeoffs = append(resp.Writeoffs, store.WriteoffJSON{ProductID: w.ProductID, BatchID: b.ID,
			Qty: store.QtyJSON{Qty: int64(w.Qty), Unit: prods[w.ProductID].Unit}, ExpiresAt: b.ExpiresAt})
	}
	h.orders = append(h.orders, hOrder{ID: o.ID, Status: resp.Status, At: at, Hash: in.BodyHash(), Items: in.Items, Resp: resp})
	return nil
}
