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

	kindStoplist = "stoplist" // store.OutboxStoplist; адаптер store не импортирует
	outboxBatch  = 1000       // store.MaxOutboxLimit
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
	var stops []outboxEntry
	for _, e := range entries {
		if e.Kind != kindStoplist {
			a.log.Printf("outbox %d: вид %q адаптер не обрабатывает, пропущено", e.ID, e.Kind)
			continue
		}
		stops = append(stops, e)
	}
	if len(stops) == 0 {
		return nil
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
		err = a.sendStopList(ctx, ids)
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
