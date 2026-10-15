package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"umirhack/internal/assistant"
	"umirhack/internal/store"
)

func askRouter() *Router {
	st := productsStock{&fakeStock{}, []store.ProductStock{
		{ID: "veg", Name: "Овощи", Unit: "g", Available: store.QtyJSON{Qty: 350, Unit: "g"}, BatchesCount: 1},
		{ID: "beef", Name: "Говядина", Unit: "g", Available: store.QtyJSON{Unit: "g"}},
	}}
	return NewRouter(Deps{
		Stock: st, Menu: fakeMenu{testSnap()}, Monitor: &fakeMonitor{},
		Now: func() time.Time { return time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC) },
	})
}

func ask(rt *Router, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/assistant/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rt.Handler().ServeHTTP(rec, req)
	return rec
}

// Помощник отвечает по тем же данным, что экраны: стоп-лист блюд и остаток продукта.
func TestAssistantAnswersFromSameSources(t *testing.T) {
	rt := askRouter()
	for _, c := range []struct{ q, intent, want string }{
		{"Что в стопе?", assistant.IntentStop, "Стейк"},
		{"сколько осталось овощей", assistant.IntentProduct, "Овощи: на складе 350 г"},
		{"можно ли приготовить суп", assistant.IntentDish, "Суп: можно приготовить 3 порции"},
		{"что истекает", assistant.IntentExpiring, "Истекающих партий нет"},
	} {
		rec := ask(rt, `{"question":"`+c.q+`"}`)
		var got assistant.Reply
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
			t.Fatalf("%q: %d %s", c.q, rec.Code, rec.Body)
		}
		if got.Intent != c.intent || !strings.Contains(got.Answer, c.want) {
			t.Errorf("%q: %+v, ждали %s / %q", c.q, got, c.intent, c.want)
		}
	}
}

func TestAssistantValidatesQuestion(t *testing.T) {
	rt := askRouter()
	for name, body := range map[string]string{
		"пустой":   `{"question":""}`,
		"длинный":  `{"question":"` + strings.Repeat("я", maxQuestionRunes+1) + `"}`,
		"без поля": `{}`,
	} {
		rec := ask(rt, body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "question") {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}

// Вопрос ничего не меняет: два одинаковых запроса — один ответ.
func TestAssistantIsReadOnlyAndDeterministic(t *testing.T) {
	rt := askRouter()
	a, b := ask(rt, `{"question":"что в стопе"}`), ask(rt, `{"question":"что в стопе"}`)
	if a.Body.String() != b.Body.String() {
		t.Fatalf("ответы различаются:\n%s\n%s", a.Body, b.Body)
	}
}
