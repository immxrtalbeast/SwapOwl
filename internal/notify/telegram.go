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
	base := t.Base
	if base == "" {
		base = "https://api.telegram.org"
	}
	body, _ := json.Marshal(map[string]any{"chat_id": t.ChatID, "text": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(base, "/")+"/bot"+t.Token+"/sendMessage", bytes.NewReader(body))
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
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("telegram: статус %d: %s", resp.StatusCode, t.redact(strings.TrimSpace(string(b))))
	}
	return nil
}

func (t *Telegram) redact(s string) string {
	if t.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, t.Token, "***")
}
