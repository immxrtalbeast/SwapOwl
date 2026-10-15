package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Button — инлайн-кнопка под сообщением.
type Button struct {
	Text   string      `json:"text"`
	Data   string      `json:"callback_data,omitempty"`
	WebApp *WebAppInfo `json:"web_app,omitempty"`
}

// WebAppInfo — адрес mini-app для кнопки web_app.
type WebAppInfo struct {
	URL string `json:"url"`
}

// SetChatMenuButton ставит в чате кнопку меню, открывающую mini-app.
func (t *Telegram) SetChatMenuButton(ctx context.Context, url string) error {
	chat, err := strconv.ParseInt(t.ChatID, 10, 64)
	if err != nil {
		return errors.New("telegram: нужен числовой id чата")
	}
	return t.call(ctx, "setChatMenuButton", map[string]any{
		"chat_id":     chat,
		"menu_button": map[string]any{"type": "web_app", "text": MiniAppText, "web_app": WebAppInfo{URL: url}},
	}, nil)
}

// MiniAppText — подпись кнопки открытия mini-app.
const MiniAppText = "Открыть SwapOwl"

// ButtonSender шлёт сообщение с кнопками (один ряд).
type ButtonSender interface {
	SendButtons(ctx context.Context, text string, row []Button) error
}

// Chat, Message, Callback, Update — часть ответа getUpdates.
type Chat struct {
	ID int64 `json:"id"`
}

type Message struct {
	Chat Chat   `json:"chat"`
	Text string `json:"text"`
}

type Callback struct {
	ID      string   `json:"id"`
	Data    string   `json:"data"`
	Message *Message `json:"message"`
}

type Update struct {
	UpdateID int64     `json:"update_id"`
	Message  *Message  `json:"message"`
	Callback *Callback `json:"callback_query"`
}

// call выполняет метод Bot API. Токен из ошибок вырезается.
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

// SendButtons отправляет текст с рядом инлайн-кнопок в чат.
func (t *Telegram) SendButtons(ctx context.Context, text string, row []Button) error {
	return t.call(ctx, "sendMessage", map[string]any{
		"chat_id": t.ChatID, "text": text,
		"reply_markup": map[string]any{"inline_keyboard": [][]Button{row}},
	}, nil)
}

// GetUpdates — long polling: ждёт новых обновлений до timeoutSec секунд.
func (t *Telegram) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	var r struct {
		Result []Update `json:"result"`
	}
	err := t.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": timeoutSec, "allowed_updates": []string{"message", "callback_query"},
	}, &r)
	return r.Result, err
}

// AnswerCallback гасит «часики» на кнопке.
func (t *Telegram) AnswerCallback(ctx context.Context, id string) error {
	return t.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id}, nil)
}

// ForceReply просит ответить на сообщение (для «Изменить срок»).
func (t *Telegram) ForceReply(ctx context.Context, text string) error {
	return t.call(ctx, "sendMessage", map[string]any{
		"chat_id": t.ChatID, "text": text, "reply_markup": map[string]any{"force_reply": true},
	}, nil)
}

// SendPhoto отправляет PNG с подписью (multipart sendPhoto).
func (t *Telegram) SendPhoto(ctx context.Context, img []byte, caption string) error {
	base := t.Base
	if base == "" {
		base = "https://api.telegram.org"
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("chat_id", t.ChatID)
	_ = mw.WriteField("caption", caption)
	fw, err := mw.CreateFormFile("photo", "week.png")
	if err != nil {
		return err
	}
	_, _ = fw.Write(img)
	_ = mw.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(base, "/")+"/bot"+t.Token+"/sendPhoto", &body)
	if err != nil {
		return errors.New("telegram: " + t.redact(err.Error()))
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := t.HC.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return errors.New("telegram: " + t.redact(err.Error()))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("telegram: статус %d: %s", resp.StatusCode, t.redact(strings.TrimSpace(string(b))))
	}
	return nil
}
