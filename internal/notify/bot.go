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

// Bot принимает команды и нажатия кнопок из одного чата (long polling getUpdates)
// и отправляет уведомления о партиях без подтверждённого срока (2.1а).
// Чтение идёт ключом Notifier.API (viewer), запись срока — только ключом Act (operator и выше).
type Bot struct {
	N      *Notifier
	TG     *Telegram  // для getUpdates: http-клиент с таймаутом больше long poll
	Act    *APIClient // nil — подтверждение срока выключено
	ChatID string

	mu      sync.Mutex
	pending int64           // партия, для которой ждём новый срок
	seenCB  map[string]bool // id нажатий, уже обработанных
	asked   map[int64]bool  // партии, о которых уже спросили
}

const pollSeconds = 25

func (b *Bot) logf(f string, a ...any) { b.N.logf(f, a...) }

// Run крутит long polling до отмены ctx.
func (b *Bot) Run(ctx context.Context) {
	var offset int64
	for ctx.Err() == nil {
		ups, err := b.TG.Updates(ctx, offset, pollSeconds)
		if err != nil {
			b.logf("getUpdates: %v", err)
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, u := range ups {
			if u.ID >= offset {
				offset = u.ID + 1
			}
			b.HandleUpdate(ctx, u)
		}
	}
}

// HandleUpdate обрабатывает одно обновление; чужие чаты игнорируются и пишутся в журнал.
func (b *Bot) HandleUpdate(ctx context.Context, u Update) {
	switch {
	case u.Callback != nil:
		chat := ""
		if u.Callback.Message != nil {
			chat = strconv.FormatInt(u.Callback.Message.Chat.ID, 10)
		}
		if chat != b.ChatID {
			b.logf("нажатие кнопки из чужого чата %q проигнорировано", chat)
			return
		}
		b.handleCallback(ctx, u.Callback.ID, u.Callback.Data)
	case u.Message != nil:
		chat := strconv.FormatInt(u.Message.Chat.ID, 10)
		if chat != b.ChatID {
			b.logf("сообщение из чужого чата %q проигнорировано", chat)
			return
		}
		b.handleText(ctx, strings.TrimSpace(u.Message.Text))
	}
}

func (b *Bot) say(ctx context.Context, text string) {
	if err := b.N.Out.Send(ctx, text); err != nil {
		b.logf("ответ: %v", err)
	}
}

func (b *Bot) handleText(ctx context.Context, text string) {
	if strings.HasPrefix(text, "/") {
		cmd := strings.SplitN(strings.Fields(text + " ")[0], "@", 2)[0]
		switch cmd {
		case "/hot":
			b.say(ctx, b.hot(ctx))
		case "/stop":
			b.say(ctx, b.stop(ctx))
		case "/expiring":
			b.say(ctx, b.expiring(ctx))
		case "/summary":
			b.say(ctx, b.summary(ctx))
		case "/start", "/help":
			b.say(ctx, "Команды: /hot, /stop, /expiring, /summary")
		default:
			b.say(ctx, "Неизвестная команда. Доступно: /hot, /stop, /expiring, /summary")
		}
		return
	}
	b.mu.Lock()
	id := b.pending
	b.mu.Unlock()
	if id == 0 {
		return
	}
	expiresOn, hours, ok := ParseExpiryInput(text, b.N.loc(ctx), b.now(ctx))
	if !ok {
		b.say(ctx, "Не понял срок. Пришлите число часов (например, 48) или дату ДД.ММ.ГГГГ.")
		return
	}
	b.mu.Lock()
	b.pending = 0
	b.mu.Unlock()
	b.say(ctx, b.setExpiry(ctx, id, expiresOn, hours, false))
}

func (b *Bot) now(ctx context.Context) time.Time {
	if c, err := b.N.API.Clock(ctx); err == nil {
		return c.Now
	}
	return time.Now()
}

// ParseExpiryInput разбирает ответ «Изменить срок»: целое число часов либо дату
// (ДД.ММ.ГГГГ или ГГГГ-ММ-ДД). Дата в прошлом не принимается.
func ParseExpiryInput(s string, loc *time.Location, now time.Time) (expiresOn string, hours int, ok bool) {
	s = strings.TrimSpace(s)
	if h, err := strconv.Atoi(s); err == nil {
		return "", h, h >= 1 && h <= 24*365
	}
	for _, layout := range []string{"02.01.2006", "2006-01-02"} {
		if d, err := time.ParseInLocation(layout, s, loc); err == nil {
			today := now.In(loc)
			if d.Before(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)) {
				return "", 0, false
			}
			return d.Format("2006-01-02"), 0, true
		}
	}
	return "", 0, false
}

func (b *Bot) handleCallback(ctx context.Context, cbID, data string) {
	b.mu.Lock()
	if b.seenCB == nil {
		b.seenCB = map[string]bool{}
	}
	dup := b.seenCB[cbID]
	b.seenCB[cbID] = true
	b.mu.Unlock()
	if dup {
		return
	}
	kind, idStr, _ := strings.Cut(data, ":")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id < 1 || (kind != "ok" && kind != "ed") {
		_ = b.TG.AnswerCallback(ctx, cbID, "Неизвестное действие")
		return
	}
	if kind == "ed" {
		_ = b.TG.AnswerCallback(ctx, cbID, "")
		b.mu.Lock()
		b.pending = id
		b.mu.Unlock()
		b.say(ctx, fmt.Sprintf("Партия №%d: пришлите новый срок — число часов от приёмки (например, 48) или дату ДД.ММ.ГГГГ.", id))
		return
	}
	msg := b.setExpiry(ctx, id, "", 0, true)
	_ = b.TG.AnswerCallback(ctx, cbID, firstLine(msg))
	b.say(ctx, msg)
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// setExpiry подтверждает срок партии. confirm=true — оставить текущий срок как есть.
// Без ключа или прав API не трогаем: в ответе — просьба завести тикет.
func (b *Bot) setExpiry(ctx context.Context, id int64, expiresOn string, hours int, confirm bool) string {
	if b.Act == nil {
		return "Подтверждение срока выключено: не задан ключ API_KEY_OPERATOR. Заведите тикет на выдачу ключа notifier с ролью operator."
	}
	role, err := b.Act.Me(ctx)
	if err != nil {
		return b.apiFail("проверка прав ключа", err)
	}
	if role != "operator" && role != "admin" {
		return fmt.Sprintf("У ключа notifier роль %q: для подтверждения срока нужна operator. API не тронут, заведите тикет на выдачу прав.", role)
	}
	list, err := b.Act.Receiving(ctx)
	if err != nil {
		return b.apiFail("список приёмки", err)
	}
	var cur *ReceivingBatch
	for i := range list {
		if list[i].ID == id {
			cur = &list[i]
		}
	}
	if cur == nil && confirm {
		return fmt.Sprintf("Партия №%d: уже подтверждено.", id)
	}
	if confirm {
		hours = int((cur.ExpiresAt.Sub(cur.ReceivedAt) + 30*time.Minute) / time.Hour)
		if hours < 1 {
			hours = 1
		}
	}
	if err := b.Act.PatchExpiry(ctx, id, expiresOn, hours); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == 409 {
			return fmt.Sprintf("Партия №%d: срок не изменён (%s).", id, conflictReason(ae.Body))
		}
		return b.apiFail("подтверждение срока", err)
	}
	if expiresOn != "" {
		return fmt.Sprintf("✅ Партия №%d: срок изменён и подтверждён, до конца дня %s.", id, expiresOn)
	}
	return fmt.Sprintf("✅ Партия №%d: срок подтверждён (%d ч от приёмки).", id, hours)
}

func conflictReason(body string) string {
	switch {
	case strings.Contains(body, "batch_closed"):
		return "партия уже просрочена или списана"
	case strings.Contains(body, "expiry_not_future"):
		return "новый срок не позже текущего времени"
	}
	return "конфликт"
}

// apiFail: маршрута нет или нет прав — не повторяем, просим тикет; иначе общая ошибка.
func (b *Bot) apiFail(what string, err error) string {
	b.logf("%s: %v", what, err)
	var ae *APIError
	if errors.As(err, &ae) {
		switch ae.Status {
		case 401, 403:
			return "У ключа notifier нет прав на это действие. API не тронут, заведите тикет на выдачу прав."
		case 404, 405:
			return "В API нет нужного маршрута приёмки (" + ae.Path + "). Заведите тикет."
		}
	}
	return "Не получилось (" + what + "), попробуйте позже."
}

// NotifyReceiving шлёт сообщение с кнопками по каждой партии с неподтверждённым сроком, о которой ещё не спрашивали.
// Нет ключа записи — молчит: кнопки всё равно не сработали бы.
func (b *Bot) NotifyReceiving(ctx context.Context) {
	if b.Act == nil {
		return
	}
	list, err := b.N.API.Receiving(ctx)
	if err != nil {
		b.logf("список приёмки: %v", err)
		return
	}
	loc := b.N.loc(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.asked == nil {
		b.asked = map[int64]bool{}
	}
	for _, r := range list {
		if b.asked[r.ID] {
			continue
		}
		txt := fmt.Sprintf("📦 Партия пришла без срока годности: %s\nПартия №%d, поставка %s, %s\nСрок по умолчанию: до %s",
			r.ProductName, r.ID, r.DeliveryNumber, FormatQty(r.Remaining), r.ExpiresAt.In(loc).Format("02.01 15:04"))
		err := b.TG.SendMarkup(ctx, txt, []Button{
			{"Подтвердить срок", fmt.Sprintf("ok:%d", r.ID)},
			{"Изменить срок", fmt.Sprintf("ed:%d", r.ID)},
		})
		if err != nil {
			b.logf("партия %d: %v", r.ID, err)
			continue
		}
		b.asked[r.ID] = true
	}
}

// SeedReceiving при старте помечает текущие неподтверждённые партии известными, не отправляя сообщений.
func (b *Bot) SeedReceiving(ctx context.Context) {
	list, err := b.N.API.Receiving(ctx)
	if err != nil {
		b.logf("список приёмки: %v", err)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.asked == nil {
		b.asked = map[int64]bool{}
	}
	for _, r := range list {
		b.asked[r.ID] = true
	}
}

func (b *Bot) hot(ctx context.Context) string {
	offers, err := b.N.API.HotOffers(ctx)
	if err != nil {
		return b.apiFail("горячие предложения", err)
	}
	if len(offers) == 0 {
		return "Горячих предложений сейчас нет."
	}
	if len(offers) > 3 {
		offers = offers[:3]
	}
	var s strings.Builder
	s.WriteString("🔥 Горячие предложения\n")
	for i, o := range offers {
		fmt.Fprintf(&s, "%d. %s — цель %d порц.", i+1, o.DishName, o.TargetPortions)
		var parts []string
		for _, r := range o.Reasons {
			name := r.ProductName
			if name == "" {
				name = r.ProductID
			}
			parts = append(parts, fmt.Sprintf("%s, партия №%d", name, r.BatchID))
		}
		if len(parts) > 0 {
			s.WriteString(" (" + strings.Join(parts, "; ") + ")")
		}
		s.WriteString("\n")
	}
	return strings.TrimRight(s.String(), "\n")
}

func (b *Bot) stop(ctx context.Context) string {
	dishes, err := b.N.API.StopList(ctx)
	if err != nil {
		return b.apiFail("стоп-лист", err)
	}
	if len(dishes) == 0 {
		return "Стоп-лист пуст."
	}
	var s strings.Builder
	s.WriteString("⛔ Стоп-лист\n")
	for _, d := range dishes {
		fmt.Fprintf(&s, "• %s", d.Name)
		var parts []string
		for _, m := range d.Missing {
			parts = append(parts, fmt.Sprintf("не хватает %s: %s", m.ProductName, FormatQty(m.Missing)))
		}
		if len(parts) > 0 {
			s.WriteString(" — " + strings.Join(parts, ", "))
		}
		s.WriteString("\n")
	}
	return strings.TrimRight(s.String(), "\n")
}

func (b *Bot) expiring(ctx context.Context) string {
	alerts, err := b.N.API.Alerts(ctx)
	if err != nil {
		return b.apiFail("лента предупреждений", err)
	}
	now := b.now(ctx)
	loc := b.N.loc(ctx)
	day := now.In(loc).Format("2006-01-02")
	prices := b.N.prices(ctx)
	var s strings.Builder
	var total int64
	var n, unknown int
	for _, a := range alerts {
		if a.Status == "expired" || a.ExpiresAt.In(loc).Format("2006-01-02") != day {
			continue
		}
		n++
		fmt.Fprintf(&s, "• %s, партия №%d: %s, до %s", a.ProductName, a.BatchID, FormatQty(a.Remaining), a.ExpiresAt.In(loc).Format("15:04"))
		if v, ok := value(prices, a); ok {
			total += v
			fmt.Fprintf(&s, " (%s)", Rub(v))
		} else {
			unknown++
		}
		s.WriteString("\n")
	}
	if n == 0 {
		return "Сегодня ничего не истекает."
	}
	head := fmt.Sprintf("⏳ Истекает сегодня: %d парт., около %s", n, Rub(total))
	if unknown > 0 {
		head += fmt.Sprintf(" (без оценки цены: %d)", unknown)
	}
	return head + "\n" + strings.TrimRight(s.String(), "\n")
}

func (b *Bot) summary(ctx context.Context) string {
	c, err := b.N.API.Clock(ctx)
	if err != nil {
		return b.apiFail("часы стенда", err)
	}
	loc := b.N.loc(ctx)
	now := c.Now.In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	wo, err := b.N.API.Writeoffs(ctx, start, now)
	if err != nil {
		return b.apiFail("списания", err)
	}
	sf, err := b.N.API.Shortfalls(ctx, start, now)
	if err != nil {
		return b.apiFail("расхождения", err)
	}
	alerts, err := b.N.API.Alerts(ctx)
	if err != nil {
		return b.apiFail("лента предупреждений", err)
	}
	return FormatDigest(now.Format("2006-01-02"), wo, sf, alerts, b.N.prices(ctx))
}
