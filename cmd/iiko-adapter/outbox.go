package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"umirhack/internal/iiko"
)

// Режимы адаптера (ADAPTER_MODE). В теневом в iiko ничего не пишется.
const (
	modeShadow = "shadow"
	modeLive   = "live"

	kindStoplist    = "stoplist"     // store.OutboxStoplist; адаптер store не импортирует
	kindSupplyOrder = "supply_order" // store.OutboxSupplyOrder
	outboxBatch     = 1000           // store.MaxOutboxLimit
)

// backoff — пауза после n-й подряд неудачной доставки: base·2^(n−1), не больше max.
func backoff(base, max time.Duration, attempts int) time.Duration {
	d := base
	for i := 1; i < attempts && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	return d
}

// stoplistIDs — id блюд из payload сообщения stoplist (полный текущий стоп-лист).
func stoplistIDs(payload json.RawMessage) ([]string, error) {
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &rows); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)
	return ids, nil
}

// processOutbox читает недоставленное из GET /api/outbox (при старте — всё, что осталось с прошлого
// раза) и доставляет в iiko. Сообщение stoplist несёт стоп-лист целиком, поэтому отправляется только
// последнее, а более ранние подтверждаются как вытесненные. Неудача — ack с ошибкой и пауза с
// удвоением; остальные виды сообщений не трогаем.
func (a *adapter) processOutbox(ctx context.Context) error {
	entries, err := a.api.listOutbox(ctx, outboxBatch)
	if err != nil {
		return err
	}
	var stops, orders []outboxEntry
	for _, e := range entries {
		switch e.Kind {
		case kindStoplist:
			stops = append(stops, e)
		case kindSupplyOrder:
			orders = append(orders, e)
		default:
			a.log.Printf("outbox %d: вид %q адаптер не обрабатывает, пропущено", e.ID, e.Kind)
		}
	}
	// Заказы поставщику независимы друг от друга и от стоп-листа: сбой одного не задерживает остальное.
	return errors.Join(a.processStoplist(ctx, stops), a.processSupplyOrders(ctx, orders))
}

// processStoplist доставляет стоп-лист (последнее сообщение; ранние вытесняются).
func (a *adapter) processStoplist(ctx context.Context, stops []outboxEntry) error {
	if len(stops) == 0 {
		return a.bootstrapStopList(ctx)
	}
	last := stops[len(stops)-1]
	if at, ok := a.retryAt[last.ID]; ok && a.now().Before(at) {
		return nil // пауза после ошибки ещё идёт
	}
	ids, err := stoplistIDs(last.Payload)
	if err != nil {
		// Повтор не поможет, но и терять сообщение молча нельзя: ack с ошибкой, attempts растёт.
		return a.failOutbox(ctx, last, fmt.Errorf("payload: %w", err))
	}
	if a.mode == modeLive {
		if err = a.sendStopList(ctx, ids); err == nil {
			a.bootstrapped = true // отправлено полное состояние, отдельная выгрузка не нужна
		}
	} else {
		err = a.recordShadow(last, ids, len(stops)-1)
	}
	if err != nil {
		return a.failOutbox(ctx, last, err)
	}
	var errs []error
	for _, e := range stops {
		if err := a.api.ackOutbox(ctx, e.ID, true, ""); err != nil {
			errs = append(errs, fmt.Errorf("ack %d: %w", e.ID, err))
			continue
		}
		delete(a.retryAt, e.ID)
	}
	return errors.Join(errs...)
}

// failOutbox подтверждает неудачу и назначает следующую попытку не раньше backoff.
func (a *adapter) failOutbox(ctx context.Context, e outboxEntry, cause error) error {
	if a.retryAt == nil {
		a.retryAt = map[int64]time.Time{}
	}
	a.retryAt[e.ID] = a.now().Add(backoff(a.retryBase, a.retryMax, e.Attempts+1))
	if err := a.api.ackOutbox(ctx, e.ID, false, cause.Error()); err != nil {
		return errors.Join(fmt.Errorf("outbox %d: %w", e.ID, cause), fmt.Errorf("ack: %w", err))
	}
	return fmt.Errorf("outbox %d: %w", e.ID, cause)
}

// sendStopList (боевой режим) заменяет стоп-лист в iiko; без изменений — не пишет.
func (a *adapter) sendStopList(ctx context.Context, ids []string) error {
	items := make([]iiko.StopItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, iiko.StopItem{ProductID: id, Balance: 0})
	}
	cur, err := a.iiko.StopList(ctx)
	if err != nil {
		return err
	}
	if sameStop(cur, items) {
		return nil
	}
	if err := a.iiko.SetStopList(ctx, items); err != nil {
		return err
	}
	a.log.Printf("стоп-лист в iiko: %d позиций", len(items))
	return nil
}

// bootstrapStopList — один раз за запуск в боевом режиме, если в очереди ничего нет, записывает в
// iiko текущий стоп-лист из API. Иначе после перехода из тени (там сообщения подтверждены) iiko
// остался бы без стоп-листа до первого его изменения. Флаг ставится только после успеха;
// sameStop не даёт лишней записи.
func (a *adapter) bootstrapStopList(ctx context.Context) error {
	if a.mode != modeLive || a.bootstrapped {
		return nil
	}
	ids, err := a.api.stoppedDishes(ctx)
	if err != nil {
		return fmt.Errorf("текущий стоп-лист: %w", err)
	}
	if err := a.sendStopList(ctx, ids); err != nil {
		return err
	}
	a.bootstrapped = true
	return nil
}

// supplyOrderPayload — payload сообщения supply_order (store.supplyOrderPayload); количество в базовых единицах.
type supplyOrderPayload struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Items     []struct {
		ProductID string `json:"product_id"`
		Qty       int64  `json:"qty"`
		Unit      string `json:"unit"`
	} `json:"items"`
}

// purchaseOrderDoc собирает документ «Заказ поставщику». Id документа — id нашего заказа, поэтому
// повтор доставки (не дошёл ack) заменяет документ в iiko, а не создаёт второй.
func (a *adapter) purchaseOrderDoc(p supplyOrderPayload) (iiko.PurchaseOrder, error) {
	if a.mode == modeLive && (a.supplierID == "" || a.storeID == "") {
		return iiko.PurchaseOrder{}, errors.New("не заданы ADAPTER_SUPPLIER_ID и ADAPTER_STORE_ID")
	}
	short := p.ID
	if len(short) > 8 {
		short = short[:8]
	}
	doc := iiko.PurchaseOrder{ID: p.ID, DocumentNumber: "ЗП-" + short, DateIncoming: p.CreatedAt.In(a.loc).Format(iiko.DateTimeLayout),
		Supplier: a.supplierID, DefaultStore: a.storeID}
	for i, it := range p.Items {
		k := 1.0
		if it.Unit == "g" || it.Unit == "ml" { // основная единица iiko — кг и л
			k = 1000
		}
		doc.Items = append(doc.Items, iiko.PurchaseOrderItem{Num: i + 1, Product: it.ProductID, Amount: float64(it.Qty) / k})
	}
	return doc, nil
}

// processSupplyOrders доставляет заказы поставщику по одному. Боевой режим — документ в iiko; теневой —
// только строка в журнале адаптера «отправил бы», сообщение подтверждается. Неудача — ack с ошибкой и пауза с удвоением.
func (a *adapter) processSupplyOrders(ctx context.Context, orders []outboxEntry) error {
	var errs []error
	for _, e := range orders {
		if at, ok := a.retryAt[e.ID]; ok && a.now().Before(at) {
			continue
		}
		var p supplyOrderPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			errs = append(errs, a.failOutbox(ctx, e, fmt.Errorf("payload: %w", err)))
			continue
		}
		doc, err := a.purchaseOrderDoc(p)
		if err == nil {
			if a.mode == modeLive {
				err = a.iiko.ImportPurchaseOrder(ctx, doc)
			} else {
				a.log.Printf("теневой режим: заказ поставщику не отправлен в iiko, отправил бы: заказ %s, %d позиций (outbox %d)", p.ID, len(doc.Items), e.ID)
			}
		}
		if err != nil {
			errs = append(errs, a.failOutbox(ctx, e, err))
			continue
		}
		if a.mode == modeLive {
			a.log.Printf("заказ поставщику %s в iiko: %d позиций", p.ID, len(doc.Items))
		}
		if err := a.api.ackOutbox(ctx, e.ID, true, ""); err != nil {
			errs = append(errs, fmt.Errorf("ack %d: %w", e.ID, err))
			continue
		}
		delete(a.retryAt, e.ID)
	}
	return errors.Join(errs...)
}
