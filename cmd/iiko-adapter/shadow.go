package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"umirhack/internal/iiko"
	"umirhack/internal/iiko/reconcile"
)

const shadowKeepDays = 60

// shadowEvent — запись журнала теневого режима: что было бы отправлено в iiko.
type shadowEvent struct {
	OutboxID   int64     `json:"outbox_id"`
	CreatedAt  time.Time `json:"created_at"`
	WouldSend  []string  `json:"would_send"` // стоп-лист целиком (id блюд)
	Added      []string  `json:"added"`      // относительно предыдущей записи
	Removed    []string  `json:"removed"`
	Superseded int       `json:"superseded,omitempty"` // сообщений, вытесненных этим
}

func loadShadow(path string) ([]shadowEvent, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ev []shadowEvent
	return ev, json.Unmarshal(b, &ev)
}

func saveShadow(path string, ev []shadowEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func diffIDs(prev, cur []string) (added, removed []string) {
	in := func(s []string) map[string]bool {
		m := map[string]bool{}
		for _, x := range s {
			m[x] = true
		}
		return m
	}
	p, c := in(prev), in(cur)
	added, removed = []string{}, []string{}
	for _, x := range cur {
		if !p[x] {
			added = append(added, x)
		}
	}
	for _, x := range prev {
		if !c[x] {
			removed = append(removed, x)
		}
	}
	return
}

// recordShadow дописывает событие в журнал. Повторная запись той же строки outbox (ack не прошёл)
// заменяет прежнюю, поэтому дублей нет.
func (a *adapter) recordShadow(e outboxEntry, ids []string, superseded int) error {
	ev, err := loadShadow(a.shadowPath)
	if err != nil {
		return fmt.Errorf("журнал теневого режима: %w", err)
	}
	kept := ev[:0:0]
	for _, x := range ev {
		if x.OutboxID != e.ID {
			kept = append(kept, x)
		}
	}
	var prev []string
	if n := len(kept); n > 0 {
		prev = kept[n-1].WouldSend
	}
	added, removed := diffIDs(prev, ids)
	kept = append(kept, shadowEvent{OutboxID: e.ID, CreatedAt: e.CreatedAt.UTC(), WouldSend: ids, Added: added, Removed: removed, Superseded: superseded})
	cut := a.now().AddDate(0, 0, -shadowKeepDays)
	for len(kept) > 1 && kept[0].CreatedAt.Before(cut) {
		kept = kept[1:]
	}
	if err := saveShadow(a.shadowPath, kept); err != nil {
		return fmt.Errorf("журнал теневого режима: %w", err)
	}
	a.log.Printf("теневой режим: в iiko не отправлено (стоп-лист %d позиций, outbox %d)", len(ids), e.ID)
	return nil
}

// ShadowReport — отчёт теневого режима за день.
type ShadowReport struct {
	Date      string          `json:"date"`
	Mode      string          `json:"mode"`
	Events    []shadowEvent   `json:"events"`
	Final     []string        `json:"final_stoplist"`
	Reconcile []reconcile.Row `json:"reconcile"`
}

// buildShadowReport собирает события за день date (в часовом поясе ресторана) и текущую сверку.
func (a *adapter) buildShadowReport(ctx context.Context, date time.Time, all bool) (ShadowReport, error) {
	ev, err := loadShadow(a.shadowPath)
	if err != nil {
		return ShadowReport{}, err
	}
	rep := ShadowReport{Date: date.Format(iiko.DateLayout), Mode: a.mode, Events: []shadowEvent{}, Final: []string{}}
	for _, x := range ev {
		if dayStart(x.CreatedAt, a.loc).Equal(date) {
			rep.Events = append(rep.Events, x)
		}
	}
	sort.SliceStable(rep.Events, func(i, j int) bool { return rep.Events[i].OutboxID < rep.Events[j].OutboxID })
	if n := len(rep.Events); n > 0 {
		rep.Final = rep.Events[n-1].WouldSend
	}
	rows, err := a.reconcileRows(ctx, all)
	if err != nil {
		return rep, err
	}
	rep.Reconcile = rows
	return rep, nil
}

func (a *adapter) runShadowReport(ctx context.Context, w io.Writer, asJSON, all bool, day string) error {
	date := dayStart(a.now(), a.loc)
	if day != "" {
		d, err := time.ParseInLocation(iiko.DateLayout, day, a.loc)
		if err != nil {
			return fmt.Errorf("-date: %w", err)
		}
		date = d
	}
	rep, err := a.buildShadowReport(ctx, date, all)
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	return writeShadowTable(w, rep, a.loc)
}

func writeShadowTable(w io.Writer, rep ShadowReport, loc *time.Location) error {
	fmt.Fprintf(w, "Отчёт теневого режима за %s (режим адаптера: %s)\n\n", rep.Date, rep.Mode)
	if len(rep.Events) == 0 {
		fmt.Fprintln(w, "Стоп-лист: изменений за день не было")
	} else {
		fmt.Fprintln(w, "Стоп-лист: что было бы отправлено в iiko")
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ВРЕМЯ\tOUTBOX\tПОЗИЦИЙ\tДОБАВЛЕНО\tУБРАНО")
		for _, e := range rep.Events {
			fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\n", e.CreatedAt.In(loc).Format("15:04:05"), e.OutboxID, len(e.WouldSend), join(e.Added), join(e.Removed))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintf(w, "Итоговый стоп-лист на конец дня: %s\n", join(rep.Final))
	}
	fmt.Fprintln(w)
	if len(rep.Reconcile) == 0 {
		_, err := fmt.Fprintln(w, "Сверка остатков: расхождений нет")
		return err
	}
	fmt.Fprintln(w, "Сверка остатков с iiko:")
	return reconcile.WriteTable(w, rep.Reconcile)
}

func join(s []string) string {
	if len(s) == 0 {
		return "—"
	}
	out := s[0]
	for _, x := range s[1:] {
		out += ", " + x
	}
	return out
}
