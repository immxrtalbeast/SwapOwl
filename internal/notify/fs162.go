package notify

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultStopWindow — окно склейки залпа stoplist.changed в одно сообщение.
const DefaultStopWindow = 30 * time.Second

// HallConfig — рассылка подсказок залу к смене.
type HallConfig struct {
	At  string // "ЧЧ:ММ" по времени ресторана
	Out Sender
}

func stopKey(d StopDish) string {
	if d.ID != "" {
		return d.ID
	}
	return d.Name
}

// SeedStop запоминает стоп-лист при старте без сообщений.
func (n *Notifier) SeedStop(ctx context.Context) {
	n.stopMu.Lock()
	defer n.stopMu.Unlock()
	n.stopBaseline(ctx)
}

func (n *Notifier) stopBaseline(ctx context.Context) bool {
	if n.stopInit {
		return true
	}
	ds, err := n.API.StopList(ctx)
	if err != nil {
		n.logf("стоп-лист: %v", err)
		return false
	}
	n.stopSnap = map[string]StopDish{}
	for _, d := range ds {
		n.stopSnap[stopKey(d)] = d
	}
	n.stopInit = true
	return true
}

// onStopChanged запускает окно склейки; события внутри окна дают одно сообщение.
func (n *Notifier) onStopChanged(ctx context.Context) {
	n.stopMu.Lock()
	defer n.stopMu.Unlock()
	if n.stopTimer != nil {
		return
	}
	w := n.StopWindow
	if w <= 0 {
		w = DefaultStopWindow
	}
	n.stopTimer = time.AfterFunc(w, func() {
		n.stopMu.Lock()
		n.stopTimer = nil
		n.stopMu.Unlock()
		n.FlushStop(ctx)
	})
}

// FlushStop перечитывает стоп-лист и шлёт одно сообщение со всеми изменениями с прошлого снимка.
func (n *Notifier) FlushStop(ctx context.Context) {
	n.stopMu.Lock()
	defer n.stopMu.Unlock()
	if !n.stopInit {
		n.stopBaseline(ctx) // первый снимок — без сообщения
		return
	}
	ds, err := n.API.StopList(ctx)
	if err != nil {
		n.logf("стоп-лист: %v", err)
		return
	}
	cur := map[string]StopDish{}
	var lines []string
	for _, d := range ds {
		k := stopKey(d)
		cur[k] = d
		if _, was := n.stopSnap[k]; !was {
			lines = append(lines, formatStopped(d))
		}
	}
	for k, d := range n.stopSnap {
		if _, still := cur[k]; !still {
			lines = append(lines, "✅ "+d.Name+" снова в продаже")
		}
	}
	if len(lines) == 0 {
		n.stopSnap = cur
		n.stopPending = false
		return
	}
	sort.Strings(lines)
	switch send, done := n.allowed(ctx, KindStop); {
	case done: // тип выключен — изменения не нужны, снимок продвигаем
		n.stopSnap, n.stopPending = cur, false
		return
	case !send: // тишина или пауза — снимок не трогаем, разницу отправит Tick, когда она кончится
		n.stopPending = true
		return
	}
	if err := n.deliver(ctx, KindStop, "", strings.Join(lines, "\n"), n.Out); err != nil {
		n.logf("стоп-лист: %v", err)
		return // снимок не обновлён — изменения уйдут со следующим событием
	}
	n.stopSnap = cur
	n.stopPending = false
}

func formatStopped(d StopDish) string {
	var why []string
	for _, m := range d.Missing {
		why = append(why, fmt.Sprintf("нет %s (нужно %s, есть %s)", genitive(m.ProductName), FormatQty(m.Required), availQty(m.Available)))
	}
	s := "⛔ " + d.Name + " в стопе"
	if len(why) > 0 {
		s += ": " + strings.Join(why, "; ")
	}
	return s
}

// ---- «Принять предложенное» ----

func nowSuggestions(ss []Suggestion) []Suggestion {
	var out []Suggestion
	for _, s := range ss {
		if s.Urgency == "now" {
			out = append(out, s)
		}
	}
	return out
}

// SendSuggestions шлёт строки «сейчас» из /api/supply-suggestions по сообщению с кнопкой «Принять».
// Возвращает текст для ответа, если слать нечего или API недоступен (пустая строка — всё отправлено);
// none=true — предложений «сейчас» нет (не ошибка).
func (n *Notifier) SendSuggestions(ctx context.Context) (msg string, none bool) {
	ss, err := n.API.SupplySuggestions(ctx)
	if err != nil {
		return n.apiFail(err), false
	}
	ss = nowSuggestions(ss)
	if len(ss) == 0 {
		return "📦 Заказывать прямо сейчас нечего.", true
	}
	if n.Buttons == nil {
		return "", false
	}
	for _, s := range ss {
		text := fmt.Sprintf("📦 Заказать сейчас: %s — %s", s.ProductName, FormatQty(s.Quantity))
		if s.Explanation != "" {
			text += "\n" + s.Explanation
		}
		if err := n.Buttons.SendButtons(ctx, text, []Button{{Text: "Принять", Data: "acc:" + s.ProductID}}); err != nil {
			n.logf("предложение %s: %v", s.ProductID, err)
			return "", false
		}
	}
	return "", false
}

// AcceptPrompt — вопрос о сроке после нажатия «Принять»; ok=false — предложение уже неактуально.
func (n *Notifier) AcceptPrompt(ctx context.Context, productID string) (string, bool) {
	s, ok := n.findSuggestion(ctx, productID)
	if !ok {
		return "Предложение уже неактуально — ничего не принято.", false
	}
	text := fmt.Sprintf("Поставка: %s, %s. Укажите срок годности: «годен до ДД.ММ» или «72 ч».", s.ProductName, FormatQty(s.Quantity))
	if s.ShelfLifeHours != nil && *s.ShelfLifeHours > 0 {
		text += fmt.Sprintf(" Обычно — %d ч.", *s.ShelfLifeHours)
	}
	return text, true
}

func (n *Notifier) findSuggestion(ctx context.Context, productID string) (Suggestion, bool) {
	ss, err := n.API.SupplySuggestions(ctx)
	if err != nil {
		n.logf("предложения: %v", err)
		return Suggestion{}, false
	}
	for _, s := range nowSuggestions(ss) {
		if s.ProductID == productID {
			return s, true
		}
	}
	return Suggestion{}, false
}

// AcceptSuggestion принимает поставку по предложению с явным сроком. id поставки строится из id нажатия
// кнопки «Принять» (callback_query.id): две настоящие поставки одного продукта за день не склеиваются,
// а повторная доставка того же нажатия даёт тот же id, и API отвечает 200 без второй партии.
func (n *Notifier) AcceptSuggestion(ctx context.Context, productID, callbackID, expiresOn string, hours int) string {
	s, ok := n.findSuggestion(ctx, productID)
	if !ok {
		return "Предложение уже принято или неактуально — вторая поставка не создана."
	}
	clk, err := n.API.Clock(ctx)
	if err != nil {
		return n.apiFail(err)
	}
	id := "TG-" + productID + "-" + callbackID
	created, err := n.API.PostDelivery(ctx, id, clk.Now, productID, s.Quantity, expiresOn, hours)
	if err == nil {
		until := expiresOn
		if until == "" {
			until = clk.Now.In(n.loc(ctx)).Add(time.Duration(hours) * time.Hour).Format("2006-01-02")
		}
		if t, perr := time.Parse("2006-01-02", until); perr == nil {
			until = t.Format("02.01.2006")
		}
		if !created {
			return fmt.Sprintf("ℹ️ Эта поставка уже была принята ранее (повтор не создал партию): %s, %s, годен до %s.", s.ProductName, FormatQty(s.Quantity), until)
		}
		return fmt.Sprintf("✅ Поставка принята: %s, %s, годен до %s.", s.ProductName, FormatQty(s.Quantity), until)
	}
	var ae *APIError
	if errors.As(err, &ae) {
		switch ae.Status {
		case 401, 403, 405:
			n.logf("поставка %s: нет прав: %v", productID, err)
			return "🚫 У ключа notifier нет прав на приём поставок (нужна роль operator). API не изменён — передайте в тикет."
		case 400:
			return "Поставка не принята: проверьте срок годности."
		case 409:
			return "Поставка с таким номером уже есть — вторая не создана."
		}
	}
	return n.apiFail(err)
}

var (
	reHours = regexp.MustCompile(`^(\d{1,5})\s*(ч|ч\.|час|часа|часов|h)?$`)
	reDate  = regexp.MustCompile(`^(?:годен\s+)?(?:до\s+)?(\d{1,2})\.(\d{1,2})(?:\.(\d{4}))?$`)
)

// ParseSupplyExpiry разбирает «годен до ДД.ММ[.ГГГГ]», «ДД.ММ» или «72 ч». Дата без года, которая уже прошла, считается
// датой следующего года только при переходе декабрь→январь; иначе возвращается эта же (прошедшая) дата,
// и вызывающий отвечает «дата в прошлом».
func ParseSupplyExpiry(s string, now time.Time) (expiresOn string, hours int, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if m := reHours.FindStringSubmatch(s); m != nil {
		h, _ := strconv.Atoi(m[1])
		if h >= 1 && h <= 87600 {
			return "", h, true
		}
		return "", 0, false
	}
	m := reDate.FindStringSubmatch(s)
	if m == nil {
		return "", 0, false
	}
	d, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	mk := func(y int) (time.Time, bool) {
		t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
		return t, t.Day() == d && int(t.Month()) == mo
	}
	if m[3] != "" {
		y, _ := strconv.Atoi(m[3])
		if t, ok := mk(y); ok {
			return t.Format("2006-01-02"), 0, true
		}
		return "", 0, false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	t, ok := mk(now.Year())
	if !ok {
		return "", 0, false
	}
	if t.Before(today) && now.Month() == time.December && mo == int(time.January) {
		if t, ok = mk(now.Year() + 1); !ok {
			return "", 0, false
		}
	}
	return t.Format("2006-01-02"), 0, true
}

// ---- подсказки залу ----

// HallText — текст «Для зала»: закреплённые и горячие блюда без скрытых и без стоп-листа.
func (n *Notifier) HallText(ctx context.Context) (string, error) {
	hd, err := n.API.HotData(ctx)
	if err != nil {
		return "", err
	}
	stop, err := n.API.StopList(ctx)
	if err != nil {
		return "", err
	}
	clk, err := n.API.Clock(ctx)
	if err != nil {
		return "", err
	}
	skip := map[string]bool{}
	for _, h := range hd.Hidden {
		skip[h.DishID] = true
	}
	for _, d := range stop {
		skip[d.ID] = true
		skip[d.Name] = true
	}
	var lines []string
	for _, o := range hd.Offers {
		if skip[o.DishID] || skip[o.DishName] {
			continue
		}
		why := o.ManagerNote
		if why == "" && len(o.Reasons) > 0 {
			r := o.Reasons[0]
			why = strings.ToLower(r.ProductName) + " нужно продать " + untilWord(r.ExpiresAt, clk.Now)
		}
		lines = append(lines, "• "+o.DishName+hallWhy(why))
	}
	for _, p := range hd.ManagerPicks {
		if skip[p.DishID] || skip[p.DishName] {
			continue
		}
		note := p.Note
		if note == "" {
			note = "советует менеджер"
		}
		lines = append(lines, "• "+p.DishName+" — "+note)
	}
	if len(lines) == 0 {
		return "🍽 Сегодня без особых предложений. Рекомендуйте блюда как обычно.", nil
	}
	return "🍽 Предложите гостям сегодня\n" + strings.Join(lines, "\n"), nil
}

func hallWhy(s string) string {
	if s == "" {
		return ""
	}
	return " — " + s
}

// untilWord — как на экране «Для зала».
func untilWord(exp, now time.Time) string {
	h := exp.Sub(now).Hours()
	switch {
	case h <= 12:
		return "сегодня"
	case h <= 36:
		return "до завтра"
	}
	return "в ближайшие дни"
}

// HallTick шлёт подсказки залу раз в день в Hall.At по времени ресторана.
func (n *Notifier) HallTick(ctx context.Context) {
	if n.Hall == nil || n.Hall.Out == nil {
		return
	}
	c, err := n.API.Clock(ctx)
	if err != nil {
		return
	}
	loc := n.loc(ctx)
	now := c.Now.In(loc)
	hh, mm, ok := parseHM(n.Hall.At)
	if !ok {
		return
	}
	date := now.Format("2006-01-02")
	due := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, loc)
	n.mu.Lock()
	skip := n.hallDate == date || now.Before(due)
	n.mu.Unlock()
	if skip {
		return
	}
	switch send, done := n.allowed(ctx, KindHall); {
	case done: // тип выключен — день обработан
		n.mu.Lock()
		n.hallDate = date
		n.mu.Unlock()
		return
	case !send: // тишина или пауза — подсказки залу не копим, повторим на следующем тике
		return
	}
	text, err := n.HallText(ctx)
	if err != nil {
		n.logf("подсказки залу: %v", err)
		return
	}
	if err := n.deliver(ctx, KindHall, "hall:"+date, text, n.Hall.Out); err != nil {
		n.logf("подсказки залу: %v", err)
		return
	}
	n.mu.Lock()
	n.hallDate = date
	n.mu.Unlock()
}

// MarkHallStartup: стартовали после времени смены — за сегодня подсказок не шлём.
func (n *Notifier) MarkHallStartup(ctx context.Context) {
	if n.Hall == nil {
		return
	}
	c, err := n.API.Clock(ctx)
	if err != nil {
		return
	}
	loc := n.loc(ctx)
	now := c.Now.In(loc)
	if hh, mm, ok := parseHM(n.Hall.At); ok && !now.Before(time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, loc)) {
		n.mu.Lock()
		n.hallDate = now.Format("2006-01-02")
		n.mu.Unlock()
	}
}

// FlushStopBaseline берёт первый снимок стоп-листа, если при старте API был недоступен.
func (n *Notifier) FlushStopBaseline(ctx context.Context) {
	n.stopMu.Lock()
	defer n.stopMu.Unlock()
	if !n.stopInit {
		n.stopBaseline(ctx)
	}
}

func availQty(q Qty) string {
	if q.Qty == 0 {
		return "0"
	}
	return FormatQty(q)
}

// genitive — родительный падеж названия продукта для частых окончаний; прочее остаётся как есть.
func genitive(name string) string {
	r := []rune(strings.ToLower(strings.TrimSpace(name)))
	if len(r) < 2 {
		return string(r)
	}
	last, prev := r[len(r)-1], r[len(r)-2]
	switch last {
	case 'а':
		if strings.ContainsRune("кгхчшщж", prev) {
			return string(r[:len(r)-1]) + "и"
		}
		return string(r[:len(r)-1]) + "ы"
	case 'я':
		return string(r[:len(r)-1]) + "и"
	case 'б', 'в', 'д', 'з', 'к', 'л', 'м', 'н', 'п', 'р', 'с', 'т', 'ф', 'х', 'ц', 'ч', 'ш', 'щ':
		return string(r) + "а"
	}
	return string(r)
}
