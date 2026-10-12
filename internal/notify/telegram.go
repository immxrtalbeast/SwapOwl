package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Telegram отправляет сообщения через Bot API. Токен нигде не печатается: ошибки транспорта
// содержат URL с токеном, поэтому они очищаются.
type Telegram struct {
	Base, Token, ChatID string
	HC                  *http.Client
}

// Send отправляет текст в чат.
func (t *Telegram) Send(ctx context.Context, text string) error {
	return t.SendMarkup(ctx, text, nil)
}

// Button — кнопка под сообщением (callback_data до 64 байт).
type Button struct{ Text, Data string }

// SendMarkup отправляет текст с рядом кнопок под ним (buttons может быть пустым).
func (t *Telegram) SendMarkup(ctx context.Context, text string, buttons []Button) error {
	p := map[string]any{"chat_id": t.ChatID, "text": text}
	if len(buttons) > 0 {
		row := make([]map[string]string, 0, len(buttons))
		for _, b := range buttons {
			row = append(row, map[string]string{"text": b.Text, "callback_data": b.Data})
		}
		p["reply_markup"] = map[string]any{"inline_keyboard": [][]map[string]string{row}}
	}
	return t.call(ctx, "sendMessage", p, nil)
}

// AnswerCallback закрывает «часики» на нажатой кнопке.
func (t *Telegram) AnswerCallback(ctx context.Context, id, text string) error {
	return t.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
}

// Update — одно обновление getUpdates (нужны только сообщения и нажатия кнопок).
type Update struct {
	ID      int64 `json:"update_id"`
	Message *struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
	Callback *struct {
		ID      string `json:"id"`
		Data    string `json:"data"`
		Message *struct {
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	} `json:"callback_query"`
}

// Updates — long polling getUpdates; timeout в секундах, http-клиент должен жить дольше.
func (t *Telegram) Updates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	var r struct {
		Result []Update `json:"result"`
	}
	err := t.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": timeout, "allowed_updates": []string{"message", "callback_query"},
	}, &r)
	return r.Result, err
}

func (t *Telegram) call(ctx context.Context, method string, payload any, out any) error {
	base := t.Base
	if base == "" {
		base = "https://api.telegram.org"
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(base, "/")+"/bot"+t.Token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return errors.New("telegram: " + t.redact(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.HC.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return errors.New("telegram: " + t.redact(err.Error()))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("telegram: статус %d: %s", resp.StatusCode, t.redact(strings.TrimSpace(string(b))))
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (t *Telegram) redact(s string) string {
	if t.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, t.Token, "***")
}
