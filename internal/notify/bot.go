package notify

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Bot принимает команды и нажатия кнопок из одного разрешённого чата (long polling getUpdates).
type Bot struct {
	N      *Notifier
	TG     *Telegram
	ChatID int64 // единственный чат, из которого принимаются команды
	Logf   func(string, ...any)
	// MiniAppURL — адрес mini-app; задан — /start отвечает кнопкой «Открыть SwapOwl».
	MiniAppURL string

	mu         sync.Mutex
	pending    int64  // партия, для которой ждём новый срок («Изменить срок»)
	pendingSup string // продукт, для которого ждём срок годности поставки («Принять»)
}

func (b *Bot) logf(f string, a ...any) {
	if b.Logf != nil {
		b.Logf(f, a...)
	}
}

// Run крутит long polling до отмены ctx.
func (b *Bot) Run(ctx context.Context, retry time.Duration) {
	var offset int64
	for ctx.Err() == nil {
		ups, err := b.TG.GetUpdates(ctx, offset, 25)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.logf("getUpdates: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(retry):
			}
			continue
		}
		for _, u := range ups {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			b.HandleUpdate(ctx, u)
		}
	}
}

// HandleUpdate обрабатывает одно обновление. Чужие чаты игнорируются и пишутся в лог (без текста).
func (b *Bot) HandleUpdate(ctx context.Context, u Update) {
	switch {
	case u.Callback != nil:
		if u.Callback.Message == nil || u.Callback.Message.Chat.ID != b.ChatID {
			b.logf("игнорирую нажатие из чужого чата")
			return
		}
		_ = b.TG.AnswerCallback(ctx, u.Callback.ID)
		b.onButton(ctx, u.Callback.Data)
	case u.Message != nil:
		if u.Message.Chat.ID != b.ChatID {
			b.logf("игнорирую сообщение из чужого чата %d", u.Message.Chat.ID)
			return
		}
		b.onText(ctx, strings.TrimSpace(u.Message.Text))
	}
}

func (b *Bot) reply(ctx context.Context, text string) {
	if err := b.TG.Send(ctx, text); err != nil {
		b.logf("ответ: %v", err)
	}
}

func (b *Bot) onText(ctx context.Context, text string) {
	if strings.HasPrefix(text, "/") {
		cmd := strings.ToLower(strings.SplitN(strings.Fields(text + " ")[0], "@", 2)[0])
		b.mu.Lock()
		b.pending = 0
		b.pendingSup = ""
		b.mu.Unlock()
		switch cmd {
		case "/hot":
			b.reply(ctx, b.N.HotText(ctx))
		case "/stop":
			b.reply(ctx, b.N.StopText(ctx))
		case "/expiring":
			b.reply(ctx, b.N.ExpiringTodayText(ctx))
		case "/summary":
			b.reply(ctx, b.N.SummaryText(ctx))
		case "/week":
			b.week(ctx)
		case "/order":
			if msg := b.N.SendSuggestions(ctx); msg != "" {
				b.reply(ctx, msg)
			}
		case "/start", "/help":
			b.start(ctx, cmd == "/start")
		default:
			b.reply(ctx, "Неизвестная команда. Доступно: "+commandList)
		}
		return
	}
	b.mu.Lock()
	id, sup := b.pending, b.pendingSup
	b.mu.Unlock()
	if sup != "" {
		now := time.Now()
		if c, err := b.N.API.Clock(ctx); err == nil {
			now = c.Now.In(b.N.loc(ctx))
		}
		on, hours, ok := ParseSupplyExpiry(text, now)
		if !ok {
			b.reply(ctx, "Не понял срок. Напишите «годен до ДД.ММ» или «72 ч».")
			return
		}
		b.mu.Lock()
		b.pendingSup = ""
		b.mu.Unlock()
		b.reply(ctx, b.N.AcceptSuggestion(ctx, sup, on, hours))
		return
	}
	if id == 0 {
		return
	}
	on, hours, ok := ParseExpiryInput(text, b.N.loc(ctx))
	if !ok {
		b.reply(ctx, "Не понял срок. Пришлите число часов (например 48) или дату ДД.ММ.ГГГГ.")
		return
	}
	b.mu.Lock()
	b.pending = 0
	b.mu.Unlock()
	b.reply(ctx, b.N.ApplyExpiry(ctx, id, on, hours))
}

const commandList = "/hot, /stop, /expiring, /summary, /week, /order"

func (b *Bot) start(ctx context.Context, withApp bool) {
	text := "Команды: " + commandList
	if withApp && b.MiniAppURL != "" {
		err := b.TG.SendButtons(ctx, text, []Button{{Text: MiniAppText, WebApp: &WebAppInfo{URL: b.MiniAppURL}}})
		if err != nil {
			b.logf("/start: %v", err)
		}
		return
	}
	b.reply(ctx, text)
}

func (b *Bot) onButton(ctx context.Context, data string) {
	if pid, ok := strings.CutPrefix(data, "acc:"); ok && pid != "" {
		text, ok := b.N.AcceptPrompt(ctx, pid)
		if !ok {
			b.reply(ctx, text)
			return
		}
		b.mu.Lock()
		b.pending, b.pendingSup = 0, pid
		b.mu.Unlock()
		if err := b.TG.ForceReply(ctx, text); err != nil {
			b.logf("запрос срока: %v", err)
		}
		return
	}
	kind, rest, _ := strings.Cut(data, ":")
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id < 1 {
		return
	}
	switch kind {
	case "ok":
		b.reply(ctx, b.N.ConfirmExpiry(ctx, id))
	case "ed":
		b.mu.Lock()
		b.pending, b.pendingSup = id, ""
		b.mu.Unlock()
		if err := b.TG.ForceReply(ctx, fmt.Sprintf("Партия №%d: пришлите новый срок — число часов от приёмки или дату ДД.ММ.ГГГГ.", id)); err != nil {
			b.logf("запрос срока: %v", err)
		}
	}
}

// ParseExpiryInput разбирает ответ: целое число часов (1…87600) либо дата ДД.ММ.ГГГГ / ГГГГ-ММ-ДД.
func ParseExpiryInput(s string, _ *time.Location) (expiresOn string, hours int, ok bool) {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		if n >= 1 && n <= 87600 {
			return "", n, true
		}
		return "", 0, false
	}
	for _, layout := range []string{"02.01.2006", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), 0, true
		}
	}
	return "", 0, false
}

const (
	msgAlreadyConfirmed = "✅ Срок уже подтверждён (или партия закрыта) — ничего не изменено."
	msgNoRights         = "🚫 У ключа notifier нет прав на подтверждение срока (нужна роль operator) или маршрута нет. API не изменён — передайте в тикет."
)

// ConfirmExpiry подтверждает текущий срок партии. Повторное нажатие безопасно:
// партии уже нет в /api/receiving — отвечаем «уже подтверждено».
func (n *Notifier) ConfirmExpiry(ctx context.Context, id int64) string {
	items, err := n.API.Receiving(ctx)
	if err != nil {
		return n.apiFail(err)
	}
	for _, it := range items {
		if it.ID != id {
			continue
		}
		h := int((it.ExpiresAt.Sub(it.ReceivedAt) + 30*time.Minute) / time.Hour)
		if h < 1 {
			h = 1
		}
		return n.ApplyExpiry(ctx, id, "", h)
	}
	return msgAlreadyConfirmed
}

// ApplyExpiry вызывает PATCH /api/batches/{id}/expiry и переводит результат в текст.
func (n *Notifier) ApplyExpiry(ctx context.Context, id int64, expiresOn string, hours int) string {
	err := n.API.PatchExpiry(ctx, id, expiresOn, hours)
	if err == nil {
		return fmt.Sprintf("✅ Срок партии №%d подтверждён.", id)
	}
	var ae *APIError
	if errors.As(err, &ae) {
		switch {
		case ae.Status == 401 || ae.Status == 403 || ae.Status == 405:
			n.logf("партия %d: нет прав или маршрута: %v", id, err)
			return msgNoRights
		case ae.Status == 404 && !strings.Contains(ae.Body, "unknown_batch"):
			n.logf("партия %d: маршрута нет: %v", id, err)
			return msgNoRights
		case ae.Status == 404:
			return fmt.Sprintf("Партия №%d не найдена.", id)
		case ae.Status == 409 && strings.Contains(ae.Body, "expiry_not_future"):
			return "Новый срок должен быть позже текущего времени."
		case ae.Status == 409:
			return msgAlreadyConfirmed
		case ae.Status == 400:
			return "Срок не принят: проверьте число часов или дату."
		}
	}
	return n.apiFail(err)
}

func (n *Notifier) apiFail(err error) string {
	var ae *APIError
	if errors.As(err, &ae) && (ae.Status == 401 || ae.Status == 403 || ae.Status == 404) {
		n.logf("доступ: %v", err)
		return msgNoRights
	}
	n.logf("API: %v", err)
	return "Не удалось связаться с API, попробуйте позже."
}

// notifyReceiving шлёт сообщение с кнопками по партиям со сроком «по умолчанию», о которых не писали.
func (n *Notifier) notifyReceiving(ctx context.Context) {
	if n.Buttons == nil {
		return
	}
	items, err := n.API.Receiving(ctx)
	if err != nil {
		n.logf("приёмка: %v", err)
		return
	}
	n.sendMu.Lock()
	defer n.sendMu.Unlock()
	if n.recvSent == nil {
		n.recvSent = map[int64]bool{}
	}
	loc := n.loc(ctx)
	for _, it := range items {
		if it.ExpirySource != "product_default" || n.recvSent[it.ID] {
			continue
		}
		text := fmt.Sprintf("📦 Партия пришла без срока: %s\nПартия №%d, поставка %s, %s\nСрок по умолчанию: %s\nПодтвердите или измените срок.",
			it.ProductName, it.ID, it.DeliveryNumber, FormatQty(it.Quantity), it.ExpiresAt.In(loc).Format("02.01 15:04"))
		btns := []Button{{Text: "Подтвердить срок", Data: fmt.Sprintf("ok:%d", it.ID)}, {Text: "Изменить срок", Data: fmt.Sprintf("ed:%d", it.ID)}}
		if err := n.Buttons.SendButtons(ctx, text, btns); err != nil {
			n.logf("партия %d: %v", it.ID, err)
			return
		}
		n.recvSent[it.ID] = true
	}
}

// seedReceiving помечает уже неподтверждённые партии отправленными (перезапуск без повторов).
func (n *Notifier) seedReceiving(ctx context.Context) {
	items, err := n.API.Receiving(ctx)
	if err != nil {
		return
	}
	n.sendMu.Lock()
	defer n.sendMu.Unlock()
	if n.recvSent == nil {
		n.recvSent = map[int64]bool{}
	}
	for _, it := range items {
		n.recvSent[it.ID] = true
	}
}

// HotText — /hot: топ-3 горячих предложения с целью в порциях и партией.
func (n *Notifier) HotText(ctx context.Context) string {
	offers, err := n.API.HotOffers(ctx)
	if err != nil {
		return n.apiFail(err)
	}
	if len(offers) == 0 {
		return "🔥 Горячих предложений сейчас нет."
	}
	if len(offers) > 3 {
		offers = offers[:3]
	}
	var b strings.Builder
	b.WriteString("🔥 Горячие предложения\n")
	for i, o := range offers {
		fmt.Fprintf(&b, "%d. %s", i+1, o.DishName)
		if o.TargetPortions > 0 {
			fmt.Fprintf(&b, " — цель %d порц.", o.TargetPortions)
		}
		var batches []string
		for _, r := range o.Reasons {
			batches = append(batches, fmt.Sprintf("№%d", r.BatchID))
		}
		if len(batches) > 0 {
			b.WriteString(", партия " + strings.Join(batches, ", "))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// StopText — /stop: стоп-лист с причинами.
func (n *Notifier) StopText(ctx context.Context) string {
	ds, err := n.API.StopList(ctx)
	if err != nil {
		return n.apiFail(err)
	}
	if len(ds) == 0 {
		return "⛔ Стоп-лист пуст."
	}
	var b strings.Builder
	b.WriteString("⛔ Стоп-лист\n")
	for _, d := range ds {
		b.WriteString("• " + d.Name)
		var why []string
		for _, m := range d.Missing {
			why = append(why, fmt.Sprintf("не хватает %s: %s", m.ProductName, FormatQty(m.Missing)))
		}
		if len(why) > 0 {
			b.WriteString(" — " + strings.Join(why, "; "))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ExpiringTodayText — /expiring: истекающее сегодня (по времени ресторана) с суммой в ₽.
func (n *Notifier) ExpiringTodayText(ctx context.Context) string {
	alerts, err := n.API.Alerts(ctx)
	if err != nil {
		return n.apiFail(err)
	}
	clk, err := n.API.Clock(ctx)
	if err != nil {
		return n.apiFail(err)
	}
	loc := n.loc(ctx)
	now := clk.Now.In(loc)
	prices := n.prices(ctx)
	var b strings.Builder
	var total int64
	var unknown int
	for _, a := range alerts {
		e := a.ExpiresAt.In(loc)
		if a.Status == "expired" || !a.ExpiresAt.After(clk.Now) ||
			e.Year() != now.Year() || e.YearDay() != now.YearDay() {
			continue
		}
		fmt.Fprintf(&b, "• %s, партия №%d: %s, до %s", a.ProductName, a.BatchID, FormatQty(a.Remaining), e.Format("15:04"))
		if v, ok := value(prices, a); ok {
			total += v
			fmt.Fprintf(&b, " (%s)", Rub(v))
		} else {
			unknown++
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return "⏳ Сегодня ничего не истекает."
	}
	out := "⏳ Истекает сегодня\n" + b.String() + "Итого: " + Rub(total)
	if unknown > 0 {
		out += fmt.Sprintf(" (без оценки цены: %d)", unknown)
	}
	return out
}

// SummaryText — /summary: дневная сводка по запросу.
func (n *Notifier) SummaryText(ctx context.Context) string {
	clk, err := n.API.Clock(ctx)
	if err != nil {
		return n.apiFail(err)
	}
	loc := n.loc(ctx)
	now := clk.Now.In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	wo, err := n.API.Writeoffs(ctx, start, now)
	if err != nil {
		return n.apiFail(err)
	}
	sf, err := n.API.Shortfalls(ctx, start, now)
	if err != nil {
		return n.apiFail(err)
	}
	alerts, err := n.API.Alerts(ctx)
	if err != nil {
		return n.apiFail(err)
	}
	return FormatDigest(now.Format("2006-01-02"), wo, sf, alerts, n.prices(ctx))
}

// week — /week: недельный отчёт по запросу в чат, откуда пришла команда.
func (b *Bot) week(ctx context.Context) {
	if err := b.N.SendWeekly(ctx, b.TG, b.TG); err != nil {
		b.logf("/week: %v", err)
		b.reply(ctx, b.N.apiFail(err))
	}
}
