package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"umirhack/internal/iiko"
	"umirhack/internal/iiko/client"
	"umirhack/internal/iiko/mapping"
)

const (
	chunkDays   = 30 // iiko ограничивает интервал отчёта; читаем кусками
	priceWindow = 30 // дней накладных для цен каталога
)

// state — курсоры по времени (локальный файл). Даты yyyy-MM-dd в часовом поясе ресторана.
type state struct {
	Invoices string `json:"invoices"` // первый день, накладные которого ещё читаем
	Sales    string `json:"sales"`    // первый день, продажи которого ещё не переданы
}

func loadState(path string) (state, error) {
	var s state
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

func saveState(path string, s state) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// adapter — один проход синхронизации iiko ⇄ наш API.
type adapter struct {
	iiko      *client.Client
	api       *apiClient
	loc       *time.Location
	now       func() time.Time
	statePath string
	start     time.Time // с какого дня читать при пустых курсорах
	defaults  mapping.Defaults
	log       *log.Logger
	dishes    map[string]bool // блюда, принятые в последний каталог

	mode         string        // modeShadow | modeLive
	shadowPath   string        // журнал «что было бы отправлено» теневого режима
	retryBase    time.Duration // первая пауза после ошибки iiko; дальше удваивается
	retryMax     time.Duration
	retryAt      map[int64]time.Time // id строки outbox → не повторять раньше этого момента
	bootstrapped bool                // в боевом режиме стоп-лист iiko приведён к текущему (раз за запуск)
}

// skippable — ответ API, который повтором не исправить: 4xx, кроме 401/403/429.
// Такой элемент пишется в журнал и пропускается, чтобы не стопорить курсор.
func skippable(err error) (*apiError, bool) {
	var ae *apiError
	if errors.As(err, &ae) && ae.Status >= 400 && ae.Status < 500 &&
		ae.Status != http.StatusUnauthorized && ae.Status != http.StatusForbidden && ae.Status != http.StatusTooManyRequests {
		return ae, true
	}
	return nil, false
}

func dayStart(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// runOnce: справочники, накладные, продажи, исходящие сообщения (стоп-лист). Сбой этапа не мешает остальным;
// курсор двигается только после успешной передачи, повтор безопасен (идемпотентность API).
func (a *adapter) runOnce(ctx context.Context) error {
	st, err := loadState(a.statePath)
	if err != nil {
		return fmt.Errorf("состояние: %w", err)
	}
	today := dayStart(a.now(), a.loc)
	var errs []error

	// Накладные и продажи зависят от каталога этого прохода: без него 400 от API
	// выглядело бы неисправимым и данные были бы пропущены навсегда.
	a.dishes = nil
	units, shelf, err := a.syncCatalog(ctx, today)
	if err != nil {
		errs = append(errs, fmt.Errorf("справочники: %w", err))
	} else {
		if err := a.syncInvoices(ctx, &st, today, units, shelf); err != nil {
			errs = append(errs, fmt.Errorf("накладные: %w", err))
		}
		if err := a.syncSales(ctx, &st, today); err != nil {
			errs = append(errs, fmt.Errorf("продажи: %w", err))
		}
	}
	if err := saveState(a.statePath, st); err != nil {
		errs = append(errs, fmt.Errorf("состояние: %w", err))
	}
	if err := a.processOutbox(ctx); err != nil {
		errs = append(errs, fmt.Errorf("outbox: %w", err))
	}
	return errors.Join(errs...)
}

func (a *adapter) syncCatalog(ctx context.Context, today time.Time) (map[string]mapping.Unit, map[string]int, error) {
	products, err := a.iiko.Products(ctx)
	if err != nil {
		return nil, nil, err
	}
	charts, err := a.iiko.Charts(ctx)
	if err != nil {
		return nil, nil, err
	}
	invs, err := a.iiko.Invoices(ctx, today.AddDate(0, 0, -priceWindow), today)
	if err != nil {
		return nil, nil, err
	}
	cat, units, skipped := mapping.BuildCatalog(products, charts, mapping.Prices(invs), a.defaults)
	for _, s := range skipped {
		a.log.Printf("справочник: пропущено: %s", s)
	}
	if _, err := a.api.do(ctx, http.MethodPut, "/api/catalog", cat); err != nil {
		return nil, nil, err
	}
	shelf := map[string]int{}
	for _, p := range products {
		if p.ShelfLifeHours > 0 {
			shelf[p.ID] = p.ShelfLifeHours
		}
	}
	a.dishes = map[string]bool{}
	for _, d := range cat.Dishes {
		a.dishes[d.ID] = true
	}
	a.log.Printf("справочники: продуктов %d, блюд %d", len(cat.Products), len(cat.Dishes))
	return units, shelf, nil
}

func (a *adapter) cursor(v string, today time.Time) time.Time {
	if t, err := time.ParseInLocation(iiko.DateLayout, v, a.loc); err == nil {
		return t
	}
	if a.start.IsZero() {
		return today.AddDate(0, 0, -1)
	}
	return a.start
}

func (a *adapter) syncInvoices(ctx context.Context, st *state, today time.Time, units map[string]mapping.Unit, shelf map[string]int) error {
	from := a.cursor(st.Invoices, today)
	for from.Before(today) || from.Equal(today) {
		to := from.AddDate(0, 0, chunkDays-1)
		if to.After(today) {
			to = today
		}
		invs, err := a.iiko.Invoices(ctx, from, to)
		if err != nil {
			return err
		}
		sort.SliceStable(invs, func(i, j int) bool { return invs[i].DateIncoming < invs[j].DateIncoming })
		for _, inv := range invs {
			d, why, ok := mapping.BuildDelivery(inv, units, shelf, a.loc)
			if !ok {
				a.log.Printf("накладная %s пропущена: %s", inv.ID, why)
				continue
			}
			if _, err := a.api.do(ctx, http.MethodPost, "/api/deliveries", d); err != nil {
				if ae, ok := skippable(err); ok {
					a.log.Printf("накладная %s пропущена: статус %d: %s", inv.ID, ae.Status, ae.Body)
					continue
				}
				return fmt.Errorf("накладная %s: %w", inv.ID, err)
			}
		}
		// Сегодняшний день читаем и в следующий раз: накладные могут появиться позже.
		st.Invoices = to.Format(iiko.DateLayout)
		from = to.AddDate(0, 0, 1)
	}
	return nil
}

func (a *adapter) syncSales(ctx context.Context, st *state, today time.Time) error {
	from := a.cursor(st.Sales, today)
	for from.Before(today) { // только закрытые дни
		to := from.AddDate(0, 0, chunkDays)
		if to.After(today) {
			to = today
		}
		sales, err := a.iiko.Sales(ctx, from, to)
		if err != nil {
			return err
		}
		kept := sales[:0:0]
		for _, s := range sales {
			if !a.dishes[s.DishID] {
				a.log.Printf("продажа %s: блюдо %s (%d порц.) не в каталоге, пропущена", s.Date, s.DishID, s.Portions)
				continue
			}
			kept = append(kept, s)
		}
		for _, o := range mapping.BuildOrders(kept) {
			if _, err := a.api.do(ctx, http.MethodPost, "/api/orders", o); err != nil {
				if ae, ok := skippable(err); ok {
					a.log.Printf("заказ %s пропущен: статус %d: %s", o.ID, ae.Status, ae.Body)
					continue
				}
				return fmt.Errorf("заказ %s: %w", o.ID, err)
			}
		}
		st.Sales = to.Format(iiko.DateLayout)
		from = to
	}
	return nil
}

func sameStop(a, b []iiko.StopItem) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]bool{}
	for _, x := range a {
		m[x.ProductID] = true
	}
	for _, x := range b {
		if !m[x.ProductID] {
			return false
		}
	}
	return true
}
