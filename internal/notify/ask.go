package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Ask — POST /api/assistant/ask: ответ «Помощника» на вопрос своими словами (FS-176). Разбор правилами на стороне API,
// бот только передаёт вопрос и показывает текст — так ответ в боте и на сайте совпадает слово в слово.
func (c *APIClient) Ask(ctx context.Context, question string) (string, error) {
	body, _ := json.Marshal(map[string]string{"question": question})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Base, "/")+"/api/assistant/ask", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HC.Do(req)
	if err != nil {
		return "", fmt.Errorf("api assistant: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return "", &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	var out struct {
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	return out.Answer, nil
}

// AskText — ответ помощника для сообщения боту; ошибка превращается в понятный текст (токен и адреса в него не попадают).
func (n *Notifier) AskText(ctx context.Context, question string) string {
	question = strings.TrimSpace(question)
	if question == "" {
		return "Спросите словами, например: «что в стопе?», «что истекает?», «сколько осталось лосося?», «что заказать?»."
	}
	if r := []rune(question); len(r) > 300 {
		question = string(r[:300])
	}
	ans, err := n.API.Ask(ctx, question)
	if err != nil {
		n.logf("помощник: %v", err)
		return "Не получилось спросить склад — попробуйте ещё раз через минуту."
	}
	return ans
}
