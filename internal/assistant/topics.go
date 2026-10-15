package assistant

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Risk — деньги под риском: сколько стоит то, что не успеем продать, и по каким продуктам.
type Risk struct {
	TotalMinor int64
	Items      []MoneyItem
}

// Money — потери за период.
type Money struct {
	TotalMinor int64
	Items      []MoneyItem // строки могут повторять продукт (по причинам) — суммируются по названию
}

// MoneyItem — стоимость по продукту, копейки.
type MoneyItem struct {
	ProductName string
	CostMinor   int64
}

// LadderBatch — партия под риском: лучшая ступень лестницы спасения и сколько денег она сохранит.
type LadderBatch struct {
	ProductName string
	Step        string
	SavedMinor  int64
}

// Ladder — лестница спасения по партиям под риском.
type Ladder struct{ Batches []LadderBatch }

// MarginDish — маржа блюда: за порцию и ожидаемая по прогнозу продаж, копейки.
type MarginDish struct {
	DishName            string
	MarginMinor         int64
	ExpectedMarginMinor int64
}

// Margin — маржа блюд из горячих предложений.
type Margin struct{ Dishes []MarginDish }

// Accuracy — точность прогноза: ошибка (WAPE) с обучением и у стартового априора.
type Accuracy struct {
	DaysUsed    int
	EnoughData  bool
	WAPEPct     float64
	BaselinePct float64
}

// topic — тема вопроса. Словарь синонимов здесь же: stems — корни слов, по которым тема узнаётся, hint — короткие
// начала слов для подсказки при непонятном вопросе. early — тема проверяется до названий продуктов и блюд
// («что делать с лососем» — про лестницу, а не про остаток).
type topic struct {
	intent   string
	early    bool
	stems    []string
	hint     []string
	topic    string
	question string
	extra    func(q []string) bool // дополнительное условие вместо/сверх корней
	answer   func(q []string, f Facts) string
}

func (t topic) matches(q []string, _ Facts) bool {
	if t.extra != nil && t.extra(q) {
		return true
	}
	return hasPrefix(q, t.stems...)
}

var topics = []topic{
	{intent: IntentMoney, early: true, topic: "деньги", question: "Сколько денег под риском?",
		stems: []string{"денег", "деньг", "рубл", "потер", "убыт", "риск", "выброс", "потрат"},
		hint:  []string{"ден", "руб", "пот", "рис", "убы"},
		extra: func(q []string) bool {
			return hasPrefix(q, "списа") && (hasPrefix(q, "сколько") || periodWord(q))
		},
		answer: moneyText},
	{intent: IntentLadder, early: true, topic: "лестницу спасения", question: "Что делать с лососем?",
		stems: []string{"лестниц", "спаст", "спаса", "спасе", "истекающ"},
		hint:  []string{"лес", "спа", "дел"},
		// «что делать с лососем» — про лестницу, а «что делать завтра» — про план.
		extra: func(q []string) bool {
			return hasPrefix(q, "делат") && !hasPrefix(q, "завтр", "недел", "запл", "планир", "сегодн", "вчера")
		},
		answer: ladderText},
	{intent: IntentMargin, early: true, topic: "маржу", question: "На чём заработаем?",
		stems:  []string{"марж", "выгодн", "заработ", "прибыл", "доход", "наварим"},
		hint:   []string{"мар", "выг", "зар"},
		answer: marginText},
	{intent: IntentAccuracy, early: true, topic: "точность прогноза", question: "Насколько точен прогноз?",
		stems:  []string{"точен", "точн", "верит", "доверя", "ошибк", "wape", "угадыв", "надежн", "прогноз"},
		hint:   []string{"точ", "вер", "прог"},
		answer: accuracyText},
	{intent: IntentStop, stems: []string{"стоп", "недоступ"}, hint: []string{"сто", "недос"}, topic: "стоп-лист", question: "Что в стопе?",
		answer: func(_ []string, f Facts) string { return stopText(f) }},
	{intent: IntentExpiring, stems: []string{"истек", "истеч", "срок", "испорт", "портит", "портят", "просроч", "списа", "свеж"},
		hint: []string{"ист", "сро", "спи", "пор", "прос", "гни"}, topic: "сроки годности", question: "Что истекает?",
		answer: func(_ []string, f Facts) string { return expiringText(f) }},
	{intent: IntentOrder, stems: []string{"заказ", "закуп", "докуп", "поставк", "привез", "купить"},
		hint: []string{"зак", "пос", "куп", "нехв", "довез"}, topic: "заказ", question: "Что заказать?",
		answer: func(_ []string, f Facts) string { return orderText(f) }},
	{intent: IntentHot, stems: []string{"горяч", "предлож", "продв", "продава", "рекоменд", "акци", "скидк", "продат"},
		hint: []string{"гор", "пред", "акц", "ски", "реко"}, topic: "предложения гостям", question: "Что предложить гостям?",
		answer: func(_ []string, f Facts) string { return hotText(f) }},
	// «запл» ловит и «запланировано», и «заплонировано» с опечаткой.
	{intent: IntentPlan, stems: []string{"будущ", "запл", "планир", "план", "завтр", "недел", "ближайш", "грядущ"},
		hint: []string{"пла", "буд", "зав", "неде", "бли"}, topic: "план на ближайшие дни", question: "Что запланировано?",
		answer: func(_ []string, f Facts) string { return planText(f) }},
}

// MaxAccuracyDays — самый длинный период проверки точности прогноза.
const MaxAccuracyDays = 30

const (
	unavailable    = "Эти данные сейчас недоступны."
	unavailableLow = "эти данные сейчас недоступны."
)

// --- нечёткое совпадение ---

// stemMatch — слово w начинается с корня stem либо отличается от начала такой же длины не более чем одной
// правкой (вставка, пропуск, замена или перестановка соседних букв). Опечатки ищем только в словах от 5 букв и
// у корней от 5 букв: короче — слишком много ложных срабатываний («стоит» ≠ «стоп»).
func stemMatch(w, stem string) bool {
	if strings.HasPrefix(w, stem) {
		return true
	}
	rw, rs := []rune(w), []rune(stem)
	if len(rw) < 5 || len(rs) < 5 {
		return false
	}
	for l := len(rs) - 1; l <= len(rs)+1; l++ {
		if l <= len(rw) && damerau(rw[:l], rs) <= 1 {
			return true
		}
	}
	return false
}

// damerau — расстояние Дамерау–Левенштейна (с перестановкой соседних букв).
func damerau(a, b []rune) int {
	d := make([][]int, len(a)+1)
	for i := range d {
		d[i] = make([]int, len(b)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(a)][len(b)]
}

// --- периоды ---

// Period — период из вопроса. Label — как он назван в ответе: «вчера», «за 7 дней».
type Period struct {
	From, To time.Time
	Label    string
	Days     int
}

func periodWord(q []string) bool {
	_, ok := parsePeriod(q, Facts{Now: time.Unix(0, 0), Loc: time.UTC}, false)
	return ok
}

// parsePeriod — «сегодня», «вчера», «на этой неделе»/«за неделю», «за месяц», «за 3 дня». Нет периода — ok=false,
// а при def=true возвращается период по умолчанию «за 7 дней».
func parsePeriod(q []string, f Facts, def bool) (Period, bool) {
	now := f.Now.In(f.Loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, f.Loc)
	days := func(n int) Period {
		return Period{From: start.AddDate(0, 0, -(n - 1)), To: f.Now, Label: "за " + daysWord(n), Days: n}
	}
	for i, w := range q {
		switch {
		case strings.HasPrefix(w, "вчера"):
			return Period{From: start.AddDate(0, 0, -1), To: start, Label: "вчера", Days: 1}, true
		case strings.HasPrefix(w, "сегодн"):
			return Period{From: start, To: f.Now, Label: "сегодня", Days: 1}, true
		case strings.HasPrefix(w, "недел"):
			return days(7), true
		case strings.HasPrefix(w, "месяц"):
			return days(30), true
		}
		if n, err := strconv.Atoi(w); err == nil && n >= 1 && n <= 90 && i+1 < len(q) &&
			(strings.HasPrefix(q[i+1], "дн") || strings.HasPrefix(q[i+1], "дней") || strings.HasPrefix(q[i+1], "сут")) {
			return days(n), true
		}
	}
	if def {
		return days(7), false
	}
	return Period{}, false
}

func daysWord(n int) string {
	return fmt.Sprintf("%d %s", n, plural(int64(n), "день", "дня", "дней"))
}

// --- ответы ---

// fmtRub — «1 200 ₽», «12,50 ₽» из копеек.
func fmtRub(minor int64) string {
	sign := ""
	if minor < 0 {
		sign, minor = "−", -minor
	}
	r := strconv.FormatInt(minor/100, 10)
	var b strings.Builder
	for i, c := range r {
		if i > 0 && (len(r)-i)%3 == 0 {
			b.WriteString(" ")
		}
		b.WriteRune(c)
	}
	if k := minor % 100; k != 0 {
		return fmt.Sprintf("%s%s,%02d ₽", sign, b.String(), k)
	}
	return sign + b.String() + " ₽"
}

// top3 — три самые дорогие позиции (одинаковые названия суммируются), при равенстве — по названию.
func top3(items []MoneyItem) []string {
	sum := map[string]int64{}
	for _, it := range items {
		sum[it.ProductName] += it.CostMinor
	}
	rows := make([]MoneyItem, 0, len(sum))
	for n, c := range sum {
		if c > 0 {
			rows = append(rows, MoneyItem{n, c})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CostMinor != rows[j].CostMinor {
			return rows[i].CostMinor > rows[j].CostMinor
		}
		return rows[i].ProductName < rows[j].ProductName
	})
	var out []string
	for i, r := range rows {
		if i == 3 {
			break
		}
		out = append(out, fmt.Sprintf("%s — %s", r.ProductName, fmtRub(r.CostMinor)))
	}
	return out
}

func moneyText(q []string, f Facts) string {
	wantRisk := hasPrefix(q, "риск")
	wantLoss := hasPrefix(q, "списа", "потер", "убыт", "выброс", "потрат")
	if !wantRisk && !wantLoss {
		wantRisk, wantLoss = true, true
	}
	var out []string
	if wantRisk {
		out = append(out, riskText(f))
	}
	if wantLoss {
		out = append(out, lossText(q, f))
	}
	return strings.Join(out, "\n")
}

func riskText(f Facts) string {
	if f.Risk == nil {
		return "Деньги под риском: " + unavailableLow
	}
	if f.Risk.TotalMinor <= 0 {
		return "Под риском на ближайшие 7 дней ничего нет."
	}
	s := fmt.Sprintf("Под риском на ближайшие 7 дней — %s.", fmtRub(f.Risk.TotalMinor))
	if t := top3(f.Risk.Items); len(t) > 0 {
		s += "\nБольше всего:\n" + bullets(t)
	}
	return s
}

func lossText(q []string, f Facts) string {
	if f.Writeoffs == nil {
		return "Списания: " + unavailableLow
	}
	p, _ := parsePeriod(q, f, true)
	m, ok := f.Writeoffs(p.From, p.To)
	if !ok {
		return "Списания: " + unavailableLow
	}
	if m.TotalMinor <= 0 {
		return fmt.Sprintf("Списаний %s не было.", p.Label)
	}
	s := fmt.Sprintf("Списано %s — %s.", p.Label, fmtRub(m.TotalMinor))
	if t := top3(m.Items); len(t) > 0 {
		s += "\nБольше всего:\n" + bullets(t)
	}
	return s
}

func ladderText(q []string, f Facts) string {
	if f.Ladder == nil {
		return "Лестница спасения: " + unavailableLow
	}
	var named string
	best := 0
	names := map[string]bool{}
	for _, p := range f.Products {
		names[p.Name] = true
	}
	for _, b := range f.Ladder.Batches {
		names[b.ProductName] = true
	}
	keys := make([]string, 0, len(names))
	for n := range names {
		keys = append(keys, n)
	}
	sort.Strings(keys)
	for _, n := range keys {
		if m, t := cover(n, q); t > 0 && m > best {
			named, best = n, m
		}
	}
	var lines []string
	for _, b := range f.Ladder.Batches {
		if named != "" && b.ProductName != named {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s — %s, не спишем ≈ %s", b.ProductName, b.Step, fmtRub(b.SavedMinor)))
	}
	if len(lines) == 0 {
		if named != "" {
			return fmt.Sprintf("%s сейчас не под риском.", named)
		}
		return "Сейчас нет партий под риском."
	}
	return "Что делать с партиями:\n" + bullets(limit(lines))
}

func marginText(q []string, f Facts) string {
	if f.Margin == nil {
		return "Маржа: " + unavailableLow
	}
	var named []MarginDish
	for _, d := range f.Margin.Dishes {
		if m, t := cover(d.DishName, q); t > 0 && m == t {
			named = append(named, d)
		}
	}
	line := func(d MarginDish) string {
		return fmt.Sprintf("%s — %s с порции, ожидаем %s", d.DishName, fmtRub(d.MarginMinor), fmtRub(d.ExpectedMarginMinor))
	}
	rows := named
	head := "Маржа:"
	if len(rows) == 0 {
		rows = append([]MarginDish(nil), f.Margin.Dishes...)
		head = "Выгоднее всего продавать:"
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].ExpectedMarginMinor != rows[j].ExpectedMarginMinor {
				return rows[i].ExpectedMarginMinor > rows[j].ExpectedMarginMinor
			}
			return rows[i].DishName < rows[j].DishName
		})
		if len(rows) > 3 {
			rows = rows[:3]
		}
	}
	if len(rows) == 0 {
		return "По горячим предложениям маржи пока нет."
	}
	lines := make([]string, len(rows))
	for i, d := range rows {
		lines[i] = line(d)
	}
	return head + "\n" + bullets(lines)
}

func fmtPct(v float64) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', 1, 64), ".", ",", 1) + " %"
}

func accuracyText(q []string, f Facts) string {
	if f.Accuracy == nil {
		return "Точность прогноза: " + unavailableLow
	}
	p, _ := parsePeriod(q, f, true)
	if p.Days > MaxAccuracyDays { // дольше проверка не считается — честно называем фактический период
		p.Days, p.Label = MaxAccuracyDays, "за "+daysWord(MaxAccuracyDays)
	}
	a, ok := f.Accuracy(p.Days)
	if !ok {
		return "Точность прогноза: " + unavailableLow
	}
	if !a.EnoughData {
		return fmt.Sprintf("Пока мало истории: прогноз проверен на %s из %d — верить цифрам рано.", daysWord(a.DaysUsed), p.Days)
	}
	verdict := "обучение пока не лучше априора"
	switch {
	case a.WAPEPct < a.BaselinePct:
		verdict = "обучение помогает"
	case a.WAPEPct > a.BaselinePct:
		verdict = "обучение пока хуже априора"
	}
	return fmt.Sprintf("Прогноз спроса %s: ошибка (WAPE) %s против %s у стартового априора — %s.",
		p.Label, fmtPct(a.WAPEPct), fmtPct(a.BaselinePct), verdict)
}
