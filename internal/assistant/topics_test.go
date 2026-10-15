package assistant

import (
	"strings"
	"testing"
	"time"
)

func topicFacts() Facts {
	f := facts()
	f.Risk = &Risk{TotalMinor: 482050, Items: []MoneyItem{{"Лосось", 300000}, {"Сливки", 120000}, {"Булочка", 50000}, {"Зелень", 12050}}}
	f.Writeoffs = func(from, to time.Time) (Money, bool) {
		days := int(to.Sub(from).Hours()/24 + 0.5)
		switch {
		case days == 0:
			return Money{}, true
		case days == 1 && to.Hour() == 0:
			return Money{TotalMinor: 123400, Items: []MoneyItem{{"Лосось", 100000}, {"Лосось", 10000}, {"Булочка", 13400}}}, true
		}
		return Money{TotalMinor: 987600, Items: []MoneyItem{{"Лосось", 500000}, {"Говяжья вырезка", 300000}, {"Сливки", 100000}, {"Булочка", 87600}}}, true
	}
	f.Ladder = &Ladder{Batches: []LadderBatch{{"Лосось", "скидка 20 % на блюдо", 85000}, {"Сливки", "перенести в другой зал", 12000}}}
	f.Margin = &Margin{Dishes: []MarginDish{
		{"Стейк из лосося", 45000, 180000}, {"Паста с лососем", 30000, 90000}, {"Крем-суп", 20000, 60000}, {"Салат", 15000, 20000}}}
	f.Accuracy = func(days int) (Accuracy, bool) {
		if days == 3 {
			return Accuracy{DaysUsed: 1}, true
		}
		return Accuracy{DaysUsed: days, EnoughData: true, WAPEPct: 18.2, BaselinePct: 31}, true
	}
	return f
}

// Вопрос → намерение → обязательные фрагменты ответа (опечатки и периоды — тоже).
func TestTopicTable(t *testing.T) {
	cases := []struct {
		q      string
		intent string
		has    []string
	}{
		{"сколько денег под риском", IntentMoney, []string{"4 820,50 ₽", "Лосось — 3 000 ₽", "Сливки — 1 200 ₽", "Булочка — 500 ₽"}},
		{"Сколько денег под риском?", IntentMoney, []string{"Под риском на ближайшие 7 дней"}},
		{"сколько списали за неделю", IntentMoney, []string{"Списано за 7 дней — 9 876 ₽", "Лосось — 5 000 ₽"}},
		{"сколько потеряли вчера", IntentMoney, []string{"Списано вчера — 1 234 ₽", "Лосось — 1 100 ₽", "Булочка — 134 ₽"}},
		{"сколько списали сегодня", IntentMoney, []string{"Списаний сегодня не было"}},
		{"сколько списали за месяц", IntentMoney, []string{"за 30 дней"}},
		{"сколько списали за 3 дня", IntentMoney, []string{"за 3 дня"}},
		{"сколько рублей мы потеряли за 14 дней", IntentMoney, []string{"за 14 дней"}},
		{"сколько денег", IntentMoney, []string{"Под риском", "Списано за 7 дней"}},
		{"сколько денег потеряли", IntentMoney, []string{"Списано за 7 дней"}},
		{"сколько потерялли вчера", IntentMoney, []string{"Списано вчера"}},
		{"что делать с лососем", IntentLadder, []string{"Лосось — скидка 20 % на блюдо, не спишем ≈ 850 ₽"}},
		{"как спасти партию", IntentLadder, []string{"Лосось", "Сливки — перенести в другой зал, не спишем ≈ 120 ₽"}},
		{"что с истекающим", IntentLadder, []string{"Что делать с партиями"}},
		{"что делать со сливками", IntentLadder, []string{"Сливки — перенести"}},
		{"что делать с говядиной", IntentLadder, []string{"Говяжья вырезка сейчас не под риском"}},
		{"покажи лестницу спасения", IntentLadder, []string{"Лосось"}},
		{"что выгоднее продавать", IntentMargin, []string{"Выгоднее всего продавать:", "Стейк из лосося — 450 ₽ с порции, ожидаем 1 800 ₽"}},
		{"на чём заработаем", IntentMargin, []string{"Стейк из лосося", "Паста с лососем", "Крем-суп"}},
		{"маржа стейка", IntentMargin, []string{"Стейк из лосося — 450 ₽ с порции"}},
		{"что выгодне продавать", IntentMargin, []string{"Выгоднее всего"}},
		{"насколько точен прогноз", IntentAccuracy, []string{"18,2 %", "31,0 %", "обучение помогает", "за 7 дней"}},
		{"можно ли верить прогнозу", IntentAccuracy, []string{"WAPE"}},
		{"насколько точный прогноз за 3 дня", IntentAccuracy, []string{"Пока мало истории"}},
		{"можно ли вериить прогнозу", IntentAccuracy, []string{"WAPE"}},
		{"что истекает", IntentExpiring, []string{"Лосось"}},
		{"что истикает", IntentExpiring, []string{"Лосось"}},
		{"что нужно закзать", IntentOrder, []string{"Сливки"}},
		{"что запланированно на неделю", IntentPlan, []string{"Что заказать:"}},
		{"что предложть гостям", IntentHot, nil},
	}
	for _, c := range cases {
		r := Answer(c.q, topicFacts())
		if r.Intent != c.intent {
			t.Errorf("%q: intent %s, ждали %s\n%s", c.q, r.Intent, c.intent, r.Answer)
			continue
		}
		for _, w := range c.has {
			if !strings.Contains(r.Answer, w) {
				t.Errorf("%q: нет %q в:\n%s", c.q, w, r.Answer)
			}
		}
	}
}

func TestTopicsUnavailableAndEmpty(t *testing.T) {
	bare := facts() // источники не подключены
	for q, want := range map[string]string{
		"сколько денег под риском": "недоступны",
		"сколько списали вчера":    "недоступны",
		"что делать с лососем":     "недоступны",
		"на чём заработаем":        "недоступны",
		"насколько точен прогноз":  "недоступны",
	} {
		if r := Answer(q, bare); !strings.Contains(r.Answer, want) {
			t.Errorf("%q: %s", q, r.Answer)
		}
	}
	e := facts()
	e.Risk = &Risk{}
	e.Ladder = &Ladder{}
	e.Margin = &Margin{}
	e.Accuracy = func(int) (Accuracy, bool) { return Accuracy{}, false }
	e.Writeoffs = func(_, _ time.Time) (Money, bool) { return Money{}, true }
	for q, want := range map[string]string{
		"сколько денег под риском": "ничего нет",
		"сколько списали вчера":    "Списаний вчера не было",
		"что делать с лососем":     "Лосось сейчас не под риском",
		"как спасти партию":        "нет партий под риском",
		"на чём заработаем":        "маржи пока нет",
		"насколько точен прогноз":  "недоступны",
	} {
		if r := Answer(q, e); !strings.Contains(r.Answer, want) {
			t.Errorf("%q: %s", q, r.Answer)
		}
	}
}

func TestTopicsDeterministic(t *testing.T) {
	for _, q := range Examples {
		a, b := Answer(q, topicFacts()), Answer(q, topicFacts())
		if a != b {
			t.Errorf("%q: ответы различаются", q)
		}
	}
}

func TestDamerau(t *testing.T) {
	for _, c := range []struct {
		a, b string
		d    int
	}{{"истекает", "истекает", 0}, {"истикает", "истекает", 1}, {"закзать", "заказать", 1}, {"заказть", "заказать", 1}, {"заазкть", "заказать", 2}, {"ab", "ba", 1}} {
		if got := damerau([]rune(c.a), []rune(c.b)); got != c.d {
			t.Errorf("%s/%s: %d, ждали %d", c.a, c.b, got, c.d)
		}
	}
	if stemMatch("стоит", "стоп") || stemMatch("сто", "стоп") {
		t.Error("короткие корни не должны искаться с опечатками")
	}
}

func TestFmtRub(t *testing.T) {
	for in, want := range map[int64]string{0: "0 ₽", 99: "0,99 ₽", 100: "1 ₽", 123456789: "1 234 567,89 ₽", -50000: "−500 ₽"} {
		if got := fmtRub(in); got != want {
			t.Errorf("%d: %s, ждали %s", in, got, want)
		}
	}
}
