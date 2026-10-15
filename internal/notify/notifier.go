package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Sender — получатель сообщений (Telegram).
type Sender interface {
	Send(ctx context.Context, text string) error
}

// Notifier превращает события склада в сообщения управляющему.
type Notifier struct {
	API      *APIClient
	Out      Sender
	DigestAt string // "HH:MM" по времени ресторана
	Logf     func(string, ...any)
	Buttons  ButtonSender  // необязательно: сообщения с кнопками о партиях без срока
	Weekly   *WeeklyConfig // необязательно: недельный отчёт собственнику
	Hall     *HallConfig   // необязательно: подсказки залу к смене

	Settings *Settings           // необязательно: настройки /settings, тихие часы, дедупликация
	Attempts int                 // попытки отправки в Telegram; 0 — DefaultSendAttempts
	Sleep    func(time.Duration) // пауза между повторами; nil — реальное ожидание

	StopWindow time.Duration // окно склейки stoplist.changed; 0 — DefaultStopWindow
	stopMu     sync.Mutex
	stopTimer  *time.Timer
	stopSnap   map[string]StopDish
	stopInit   bool
	hallDate   string

	recvSent   map[int64]bool // партии без срока, о которых уже написали
	sendMu     sync.Mutex     // сериализует отправку и защищает sent
	sent       map[int64]bool // партии, о которых уже написали
	mu         sync.Mutex
	digestDate string // дата последней сводки
	weeklyDate string // дата последнего недельного отчёта
}

func (n *Notifier) logf(f string, a ...any) {
	if n.Logf != nil {
		n.Logf(f, a...)
	}
}

// Handle обрабатывает одно событие SSE. Нужны только batch.expiring.
func (n *Notifier) Handle(ctx context.Context, ev Event) {
	if ev.Type == "delivery.accepted" {
		n.notifyReceiving(ctx)
		return
	}
	if ev.Type == "stoplist.changed" {
		n.onStopChanged(ctx)
		return
	}
	if ev.Type != "batch.expiring" {
		return
	}
	var d struct {
		BatchID int64 `json:"batch_id"`
	}
	if err := json.Unmarshal([]byte(ev.Data), &d); err != nil || d.BatchID == 0 {
		n.logf("batch.expiring: не разобрать данные")
		return
	}
	alerts, err := n.API.Alerts(ctx)
	if err != nil {
		n.logf("партия %d: %v", d.BatchID, err)
		return
	}
	for _, a := range alerts {
		if a.BatchID == d.BatchID {
			n.notifyAlert(ctx, a)
		}
	}
}

// CatchUp шлёт сообщения по истекающим партиям из /api/alerts, о которых ещё не писали:
// сервер присылает batch.expiring один раз, поэтому упавшая отправка или разрыв SSE
// догоняются здесь (на каждом тике и после переподключения).
func (n *Notifier) CatchUp(ctx context.Context) {
	n.notifyReceiving(ctx)
	alerts, err := n.API.Alerts(ctx)
	if err != nil {
		n.logf("лента предупреждений: %v", err)
		return
	}
	for _, a := range alerts {
		if a.Status == "expiring" {
			n.notifyAlert(ctx, a)
		}
	}
}

// SeedSent при старте помечает уже истекающие партии отправленными, ничего не отправляя,
// чтобы перезапуск не завалил чат повторами.
func (n *Notifier) SeedSent(ctx context.Context) {
	n.seedReceiving(ctx)
	alerts, err := n.API.Alerts(ctx)
	if err != nil {
		n.logf("лента предупреждений: %v", err)
		return
	}
	n.sendMu.Lock()
	defer n.sendMu.Unlock()
	if n.sent == nil {
		n.sent = map[int64]bool{}
	}
	for _, a := range alerts {
		if a.Status == "expiring" {
			n.sent[a.BatchID] = true
		}
	}
}

func (n *Notifier) notifyAlert(ctx context.Context, a Alert) {
	n.sendMu.Lock()
	defer n.sendMu.Unlock()
	if n.sent == nil {
		n.sent = map[int64]bool{}
	}
	if n.sent[a.BatchID] {
		return
	}
	offers, err := n.API.HotOffers(ctx)
	if err != nil {
		n.logf("горячие предложения недоступны: %v", err)
	}
	prices := n.prices(ctx)
	key := fmt.Sprintf("expiring:%d:%d", a.BatchID, a.ExpiresAt.Unix())
	if err := n.deliver(ctx, KindExpiring, key, FormatExpiring(a, offers, prices, n.loc(ctx)), n.Out); err != nil {
		n.logf("партия %d: %v", a.BatchID, err)
		return // повторим на следующем проходе CatchUp
	}
	n.sent[a.BatchID] = true
}

// prices — оценка цены за базовую единицу по недавним потерям (в API нет прямого чтения цен).
func (n *Notifier) prices(ctx context.Context) map[string]float64 {
	now := time.Now()
	if c, err := n.API.Clock(ctx); err == nil {
		now = c.Now
	}
	p := map[string]float64{}
	for _, rep := range []func() (MoneyReport, error){
		func() (MoneyReport, error) { return n.API.Writeoffs(ctx, now.AddDate(0, 0, -30), now) },
		func() (MoneyReport, error) { return n.API.Shortfalls(ctx, now.AddDate(0, 0, -30), now) },
	} {
		r, err := rep()
		if err != nil {
			continue
		}
		for _, row := range r.ByProduct {
			if _, ok := p[row.ProductID]; !ok && row.Quantity.Qty > 0 && row.CostMinor > 0 {
				p[row.ProductID] = float64(row.CostMinor) / float64(row.Quantity.Qty)
			}
		}
	}
	return p
}

func (n *Notifier) loc(ctx context.Context) *time.Location {
	if c, err := n.API.Clock(ctx); err == nil {
		if l, err := time.LoadLocation(c.Timezone); err == nil {
			return l
		}
	}
	return time.UTC
}

// Tick проверяет, не пора ли отправить дневную сводку; вызывается периодически.
func (n *Notifier) Tick(ctx context.Context) {
	n.CatchUp(ctx)
	n.WeeklyTick(ctx)
	n.HallTick(ctx)
	n.FlushStopBaseline(ctx)
	n.FlushQuiet(ctx)
	c, err := n.API.Clock(ctx)
	if err != nil {
		n.logf("часы стенда: %v", err)
		return
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := c.Now.In(loc)
	hh, mm, ok := parseHM(n.DigestAt)
	if !ok {
		return
	}
	date := now.Format("2006-01-02")
	due := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, loc)
	n.mu.Lock()
	skip := n.digestDate == date || now.Before(due)
	n.mu.Unlock()
	if skip {
		return
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if err := n.sendDigest(ctx, start, now, date); err != nil {
		n.logf("сводка: %v", err)
		return
	}
	n.mu.Lock()
	n.digestDate = date
	n.mu.Unlock()
}

// MarkDigestStartup: если процесс стартовал после времени сводки, за сегодня её не шлём.
func (n *Notifier) MarkDigestStartup(ctx context.Context) {
	c, err := n.API.Clock(ctx)
	if err != nil {
		return
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := c.Now.In(loc)
	if hh, mm, ok := parseHM(n.DigestAt); ok &&
		!now.Before(time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, loc)) {
		n.mu.Lock()
		n.digestDate = now.Format("2006-01-02")
		n.mu.Unlock()
	}
}

func (n *Notifier) sendDigest(ctx context.Context, from, to time.Time, date string) error {
	wo, err := n.API.Writeoffs(ctx, from, to)
	if err != nil {
		return err
	}
	sf, err := n.API.Shortfalls(ctx, from, to)
	if err != nil {
		return err
	}
	alerts, err := n.API.Alerts(ctx)
	if err != nil {
		return err
	}
	if err := n.deliver(ctx, KindDigest, "digest:"+date, FormatDigest(date, wo, sf, alerts, n.prices(ctx)), n.Out); err != nil {
		return err
	}
	if msg, none := n.SendSuggestions(ctx); msg != "" && !none {
		n.logf("предложения поставок: %s", msg)
	}
	return nil
}

func parseHM(s string) (h, m int, ok bool) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, false
	}
	return t.Hour(), t.Minute(), true
}

// Rub форматирует копейки как «1 234,50 ₽».
func Rub(minor int64) string {
	neg := minor < 0
	if neg {
		minor = -minor
	}
	whole := fmt.Sprintf("%d", minor/100)
	var parts []string
	for len(whole) > 3 {
		parts = append([]string{whole[len(whole)-3:]}, parts...)
		whole = whole[:len(whole)-3]
	}
	parts = append([]string{whole}, parts...)
	s := strings.Join(parts, " ") + fmt.Sprintf(",%02d ₽", minor%100)
	if neg {
		s = "-" + s
	}
	return s
}

// FormatQty печатает количество в удобных единицах.
func FormatQty(q Qty) string {
	switch q.Unit {
	case "g":
		if q.Qty >= 1000 {
			return trim(float64(q.Qty)/1000) + " кг"
		}
		return fmt.Sprintf("%d г", q.Qty)
	case "ml":
		if q.Qty >= 1000 {
			return trim(float64(q.Qty)/1000) + " л"
		}
		return fmt.Sprintf("%d мл", q.Qty)
	case "pcs":
		return fmt.Sprintf("%d шт", q.Qty)
	}
	return fmt.Sprintf("%d %s", q.Qty, q.Unit)
}

func trim(f float64) string {
	return strings.Replace(strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", f), "0"), "."), ".", ",", 1)
}

func value(prices map[string]float64, a Alert) (int64, bool) {
	p, ok := prices[a.ProductID]
	if !ok {
		return 0, false
	}
	return int64(p * float64(a.Remaining.Qty)), true
}

// FormatExpiring — сообщение об истекающей партии.
func FormatExpiring(a Alert, offers []Offer, prices map[string]float64, loc *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "⚠️ Истекает срок: %s\n", a.ProductName)
	fmt.Fprintf(&b, "Партия №%d, под угрозой %s\n", a.BatchID, FormatQty(a.Remaining))
	fmt.Fprintf(&b, "Истекает: %s\n", a.ExpiresAt.In(loc).Format("02.01 15:04"))
	if v, ok := value(prices, a); ok {
		fmt.Fprintf(&b, "Это около %s\n", Rub(v))
	} else {
		b.WriteString("Стоимость: нет данных о цене\n")
	}
	if a.Doomed {
		b.WriteString("Продать уже не успеют.\n")
	}
	var dishes []string
	for _, o := range offers {
		for _, r := range o.Reasons {
			if r.BatchID == a.BatchID {
				s := o.DishName
				if o.TargetPortions > 0 {
					s += fmt.Sprintf(" (%d порц.)", o.TargetPortions)
				}
				dishes = append(dishes, s)
				break
			}
		}
	}
	if len(dishes) > 0 {
		b.WriteString("Продвигать: " + strings.Join(dishes, ", "))
	} else {
		b.WriteString("Подходящих блюд для продвижения нет.")
	}
	return b.String()
}

// FormatDigest — дневная сводка.
func FormatDigest(date string, wo, sf MoneyReport, alerts []Alert, prices map[string]float64) string {
	var risk int64
	var unknown, count int
	for _, a := range alerts {
		if a.Status == "expired" {
			continue
		}
		count++
		if v, ok := value(prices, a); ok {
			risk += v
		} else {
			unknown++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📊 Сводка за %s\n", date)
	fmt.Fprintf(&b, "Списано за день: %s\n", Rub(wo.TotalMinor))
	fmt.Fprintf(&b, "Под риском: %s (партий: %d", Rub(risk), count)
	if unknown > 0 {
		fmt.Fprintf(&b, ", без оценки цены: %d", unknown)
	}
	b.WriteString(")\n")
	fmt.Fprintf(&b, "Расхождения кассы: %s", Rub(sf.TotalMinor))
	return b.String()
}
