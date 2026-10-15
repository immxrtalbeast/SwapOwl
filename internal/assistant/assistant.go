// Package assistant — «Помощник» (FS-176): ответы на вопросы своими словами по правилам, без модели.
// Разбор детерминированный: один и тот же вопрос на тех же данных всегда даёт тот же ответ. Данные приходят готовыми
// (Facts), пакет ничего не читает и не пишет.
package assistant

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Qty — количество в базовых единицах API: g, ml, pcs.
type Qty struct {
	Qty  int64
	Unit string
}

// Product — остаток продукта.
type Product struct {
	ID, Name         string
	Available        Qty
	Batches          int
	NearestExpiresAt *time.Time
}

// Alert — истекающая или просроченная партия.
type Alert struct {
	ProductName string
	Remaining   Qty
	ExpiresAt   time.Time
	Expired     bool
	Doomed      bool
}

// Missing — чего не хватает блюду в стоп-листе.
type Missing struct {
	ProductName       string
	Required, Present Qty
}

// Dish — блюдо меню с порциями и причинами стопа.
type Dish struct {
	Name      string
	Available bool
	Servings  int64
	Missing   []Missing
}

// Supply — предложение поставки.
type Supply struct {
	ProductName  string
	Quantity     Qty
	Urgency      string // now | soon | planned
	BlocksDishes int
}

// Hot — горячее предложение.
type Hot struct {
	DishName string
	Products []string
	Note     string
}

// Facts — всё, что помощник знает о ресторане на момент Now.
type Facts struct {
	Now      time.Time
	Loc      *time.Location
	Products []Product
	Alerts   []Alert
	Dishes   []Dish
	Supply   []Supply
	Hot      []Hot
}

// Reply — ответ: распознанное намерение и текст.
type Reply struct {
	Intent string `json:"intent"`
	Answer string `json:"answer"`
}

// Намерения.
const (
	IntentHelp     = "help"
	IntentStop     = "stop"
	IntentExpiring = "expiring"
	IntentOrder    = "order"
	IntentHot      = "hot"
	IntentPlan     = "plan"
	IntentProduct  = "product"
	IntentDish     = "dish"
	IntentUnknown  = "unknown"
)

// Examples — подсказки для пустого ввода и непонятого вопроса.
var Examples = []string{"Что в стопе?", "Что истекает?", "Сколько осталось лосося?", "Что заказать?", "Что предложить гостям?", "Что запланировано?"}

const maxLines = 8

var stopWords = map[string]bool{
	"что": true, "в": true, "на": true, "из": true, "и": true, "а": true, "у": true, "нас": true, "сегодня": true,
	"сколько": true, "осталось": true, "остаток": true, "есть": true, "ли": true, "какие": true, "какой": true, "какая": true,
	"мне": true, "нужно": true, "надо": true, "можно": true, "про": true, "по": true, "о": true, "об": true, "покажи": true,
	"скажи": true, "дай": true, "сейчас": true, "с": true, "для": true, "еще": true, "порций": true,
	"порции": true, "порция": true, "хватит": true, "хватает": true, "где": true, "как": true, "много": true,
}

func words(s string) []string {
	s = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, "ё", "е"), "Ё", "е"))
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func hasPrefix(ws []string, prefixes ...string) bool {
	for _, w := range ws {
		for _, p := range prefixes {
			if strings.HasPrefix(w, p) {
				return true
			}
		}
	}
	return false
}

// matchWord — слова одного корня: общий префикс не короче четырёх букв (лосося ~ лосось, сливок ~ сливки).
func matchWord(a, b string) bool {
	ra, rb := []rune(a), []rune(b)
	if len(ra) < 4 || len(rb) < 4 {
		return a == b
	}
	n := 0
	for n < len(ra) && n < len(rb) && ra[n] == rb[n] {
		n++
	}
	return n >= 4
}

// cover — сколько значимых слов названия встретилось в вопросе и сколько их всего.
func cover(name string, q []string) (matched, total int) {
	for _, nw := range words(name) {
		if stopWords[nw] || len([]rune(nw)) < 3 {
			continue
		}
		total++
		for _, qw := range q {
			if !stopWords[qw] && matchWord(nw, qw) {
				matched++
				break
			}
		}
	}
	return matched, total
}

// Answer разбирает вопрос и отвечает по фактам.
func Answer(question string, f Facts) Reply {
	if f.Loc == nil {
		f.Loc = time.UTC
	}
	q := words(question)
	if len(q) == 0 || hasPrefix(q, "умеешь", "помощь", "помоги", "команды", "примеры", "привет", "help") {
		return Reply{IntentHelp, helpText()}
	}
	if r, ok := entity(q, f); ok {
		return r
	}
	switch {
	case hasPrefix(q, "стоп", "недоступ"):
		return Reply{IntentStop, stopText(f)}
	case hasPrefix(q, "истек", "истеч", "срок", "испорт", "портит", "портят", "просроч", "списа", "свеж"):
		return Reply{IntentExpiring, expiringText(f)}
	case hasPrefix(q, "заказ", "закуп", "докуп", "поставк", "привез", "купить"):
		return Reply{IntentOrder, orderText(f)}
	case hasPrefix(q, "горяч", "предлож", "продв", "продава", "рекоменд", "акци", "скидк", "продат"):
		return Reply{IntentHot, hotText(f)}
	// «запл» ловит и «запланировано», и «заплонировано» с опечаткой.
	case hasPrefix(q, "будущ", "запл", "планир", "план", "завтр", "недел", "ближайш", "грядущ"):
		return Reply{IntentPlan, planText(f)}
	}
	return Reply{IntentUnknown, unknownText(q)}
}

// nearest — подсказка для непонятого вопроса: тема, к которой вопрос ближе всего (по первым буквам слов).
var nearest = []struct {
	stems    []string
	topic    string
	question string
}{
	{[]string{"сто", "недос"}, "стоп-лист", "Что в стопе?"},
	{[]string{"ист", "сро", "спи", "пор", "прос", "гни"}, "сроки годности", "Что истекает?"},
	{[]string{"зак", "пос", "куп", "нехв", "довез"}, "заказ", "Что заказать?"},
	{[]string{"гор", "пред", "акц", "ски", "реко"}, "предложения гостям", "Что предложить гостям?"},
	{[]string{"пла", "буд", "зав", "неде", "бли"}, "план на ближайшие дни", "Что запланировано?"},
}

func unknownText(q []string) string {
	for _, n := range nearest {
		if hasPrefix(q, n.stems...) {
			return fmt.Sprintf("Не поняла вопрос. Похоже, вы про %s — спросите «%s».\nИли выберите:\n%s", n.topic, n.question, bullets(Examples))
		}
	}
	return "Не поняла вопрос. Спросите, например:\n" + bullets(Examples)
}

func helpText() string {
	return "Я отвечаю по данным склада — без догадок. Спросите, например:\n" + bullets(Examples) +
		"\nМожно назвать продукт («сколько сливок?») или блюдо («можно ли приготовить пасту?»)."
}

type cand struct {
	reply   Reply
	full    bool
	matched int
	product bool
}

// entity — вопрос про конкретный продукт или блюдо: берём название, целиком покрытое вопросом; при равенстве
// «осталось/есть» тянет к продукту, «порции/приготовить» — к блюду.
func entity(q []string, f Facts) (Reply, bool) {
	var best *cand
	consider := func(c cand) {
		if best == nil || better(c, *best) {
			cc := c
			best = &cc
		}
	}
	for _, p := range f.Products {
		if m, t := cover(p.Name, q); t > 0 && m > 0 {
			consider(cand{Reply{IntentProduct, productText(p, f)}, m == t, m, true})
		}
	}
	for _, d := range f.Dishes {
		if m, t := cover(d.Name, q); t > 0 && m > 0 {
			consider(cand{Reply{IntentDish, dishText(d)}, m == t, m, false})
		}
	}
	if best == nil {
		return Reply{}, false
	}
	if best.product && hasPrefix(q, "порци", "приготов", "готов") {
		for _, d := range f.Dishes {
			if m, t := cover(d.Name, q); t > 0 && m > 0 {
				return Reply{IntentDish, dishText(d)}, true
			}
		}
	}
	return best.reply, true
}

func productText(p Product, f Facts) string {
	if p.Available.Qty <= 0 {
		return fmt.Sprintf("%s: на складе нет.", p.Name)
	}
	s := fmt.Sprintf("%s: на складе %s", p.Name, fmtQty(p.Available))
	if p.Batches > 0 {
		s += fmt.Sprintf(" (%s)", batchesWord(p.Batches))
	}
	s += "."
	if p.NearestExpiresAt != nil {
		s += " Ближайший срок — " + when(*p.NearestExpiresAt, f) + "."
	}
	for _, a := range f.Alerts {
		if a.ProductName == p.Name && !a.Expired {
			s += fmt.Sprintf(" Истекает %s — %s.", fmtQty(a.Remaining), when(a.ExpiresAt, f))
			break
		}
	}
	return s
}

func dishText(d Dish) string {
	if d.Available {
		return fmt.Sprintf("%s: можно приготовить %s.", d.Name, portionsWord(d.Servings))
	}
	if len(d.Missing) == 0 {
		return fmt.Sprintf("%s: в стоп-листе.", d.Name)
	}
	return fmt.Sprintf("%s: в стоп-листе — %s.", d.Name, missingText(d.Missing))
}

func missingText(ms []Missing) string {
	parts := make([]string, 0, len(ms))
	for _, m := range ms {
		parts = append(parts, fmt.Sprintf("не хватает «%s» (нужно %s, есть %s)", strings.ToLower(m.ProductName), fmtQty(m.Required), fmtQty(m.Present)))
	}
	return strings.Join(parts, "; ")
}

func stopText(f Facts) string {
	var lines []string
	for _, d := range f.Dishes {
		if !d.Available {
			lines = append(lines, fmt.Sprintf("%s — %s", d.Name, missingOrStop(d)))
		}
	}
	if len(lines) == 0 {
		return "Стоп-лист пуст: всё меню доступно."
	}
	return fmt.Sprintf("В стоп-листе %d:\n%s", len(lines), bullets(limit(lines)))
}

func missingOrStop(d Dish) string {
	if len(d.Missing) == 0 {
		return "в стопе"
	}
	return missingText(d.Missing)
}

// expiringLines — истекающие партии по возрастанию срока и уже списанные за сутки.
func expiringLines(f Facts) (soon, lost []string) {
	alerts := append([]Alert(nil), f.Alerts...)
	sort.SliceStable(alerts, func(i, j int) bool { return alerts[i].ExpiresAt.Before(alerts[j].ExpiresAt) })
	for _, a := range alerts {
		line := fmt.Sprintf("%s — %s, %s", a.ProductName, fmtQty(a.Remaining), when(a.ExpiresAt, f))
		switch {
		case a.Expired:
			lost = append(lost, fmt.Sprintf("%s — %s", a.ProductName, fmtQty(a.Remaining)))
		case a.Doomed:
			soon = append(soon, line+" (не успеем продать)")
		default:
			soon = append(soon, line)
		}
	}
	return soon, lost
}

func expiringText(f Facts) string {
	soon, lost := expiringLines(f)
	if len(soon) == 0 && len(lost) == 0 {
		return "Истекающих партий нет."
	}
	var out []string
	if len(soon) > 0 {
		out = append(out, fmt.Sprintf("Истекает %s:\n%s", batchesWord(len(soon)), bullets(limit(soon))))
	}
	if len(lost) > 0 {
		out = append(out, "Уже списано за сутки:\n"+bullets(limit(lost)))
	}
	return strings.Join(out, "\n")
}

// orderLines — что заказать: сначала срочное.
func orderLines(f Facts) []string {
	rank := map[string]int{"now": 0, "soon": 1, "planned": 2}
	rows := append([]Supply(nil), f.Supply...)
	sort.SliceStable(rows, func(i, j int) bool { return rank[rows[i].Urgency] < rank[rows[j].Urgency] })
	var lines []string
	for _, s := range rows {
		line := fmt.Sprintf("%s — %s", s.ProductName, fmtQty(s.Quantity))
		switch s.Urgency {
		case "now":
			line += ", срочно"
		case "soon":
			line += ", в ближайшие дни"
		}
		if s.BlocksDishes > 0 {
			line += fmt.Sprintf(", без него в стопе %s", dishesWord(s.BlocksDishes))
		}
		lines = append(lines, line)
	}
	return lines
}

func orderText(f Facts) string {
	lines := orderLines(f)
	if len(lines) == 0 {
		return "Заказывать прямо сейчас нечего."
	}
	return "Что заказать:\n" + bullets(limit(lines))
}

// planText — ближайшие дни: что истекает (в том числе завтра) и что заказать. Данных о поставках «в пути» у склада
// нет, поэтому о них помощник не говорит.
func planText(f Facts) string {
	soon, _ := expiringLines(f)
	lines := orderLines(f)
	var out []string
	if len(soon) > 0 {
		out = append(out, "Скоро истекает:\n"+bullets(limit(soon)))
	}
	if len(lines) > 0 {
		out = append(out, "Что заказать:\n"+bullets(limit(lines)))
	}
	if len(out) == 0 {
		return "На ближайшие дни всё спокойно: ничего не истекает, заказывать нечего."
	}
	return strings.Join(out, "\n")
}

func hotText(f Facts) string {
	var lines []string
	for _, h := range f.Hot {
		line := h.DishName
		switch {
		case h.Note != "":
			line += " — " + h.Note
		case len(h.Products) > 0:
			line += " — продать «" + strings.ToLower(strings.Join(h.Products, "», «")) + "» сегодня"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "Особых предложений нет: продавайте как обычно."
	}
	return "Что предложить гостям:\n" + bullets(limit(lines))
}

func limit(lines []string) []string {
	if len(lines) <= maxLines {
		return lines
	}
	out := append([]string(nil), lines[:maxLines]...)
	return append(out, fmt.Sprintf("… и ещё %d", len(lines)-maxLines))
}

func bullets(lines []string) string {
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("• " + l)
	}
	return b.String()
}

// fmtQty — «1,4 кг», «600 мл», «3 шт».
func fmtQty(q Qty) string {
	big := func(v int64, small, large string) string {
		if v < 1000 {
			return fmt.Sprintf("%d %s", v, small)
		}
		tenths := (v + 50) / 100
		if tenths%10 == 0 {
			return fmt.Sprintf("%d %s", tenths/10, large)
		}
		return fmt.Sprintf("%d,%d %s", tenths/10, tenths%10, large)
	}
	switch q.Unit {
	case "g":
		return big(q.Qty, "г", "кг")
	case "ml":
		return big(q.Qty, "мл", "л")
	default:
		return fmt.Sprintf("%d шт", q.Qty)
	}
}

// when — «сегодня в 18:00», «завтра в 09:00», «12.10 в 18:00» по времени ресторана.
func when(t time.Time, f Facts) string {
	t = t.In(f.Loc)
	now := f.Now.In(f.Loc)
	hm := t.Format("15:04")
	switch d := dayDiff(now, t); {
	case d < 0:
		return "уже истёк"
	case d == 0:
		if t.Before(now) {
			return "уже истёк"
		}
		return "сегодня в " + hm
	case d == 1:
		return "завтра в " + hm
	default:
		return t.Format("02.01") + " в " + hm
	}
}

func dayDiff(a, b time.Time) int {
	da := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	db := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	return int(db.Sub(da).Hours() / 24)
}

func plural(n int64, one, few, many string) string {
	m10, m100 := n%10, n%100
	switch {
	case m10 == 1 && m100 != 11:
		return one
	case m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14):
		return few
	}
	return many
}

func batchesWord(n int) string {
	return fmt.Sprintf("%d %s", n, plural(int64(n), "партия", "партии", "партий"))
}
func dishesWord(n int) string {
	return fmt.Sprintf("%d %s", n, plural(int64(n), "блюдо", "блюда", "блюд"))
}
func portionsWord(n int64) string {
	return fmt.Sprintf("%d %s", n, plural(n, "порция", "порции", "порций"))
}

// better — полное покрытие названия важнее частичного, затем число совпавших слов, затем продукт перед блюдом.
func better(a, b cand) bool {
	if a.full != b.full {
		return a.full
	}
	if a.matched != b.matched {
		return a.matched > b.matched
	}
	return a.product && !b.product
}
