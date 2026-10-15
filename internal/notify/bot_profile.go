package notify

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// BotCommand — команда в меню «/» Telegram: она видна сразу, как человек открыл чат с ботом.
type BotCommand struct {
	Command     string `json:"command"`     // без «/», a-z, 0-9, _; до 32 символов
	Description string `json:"description"` // 3–256 символов
}

// BotCommands — единственный источник списка команд: меню «/», текст /start и /help берутся отсюда.
// Порядок — по частоте использования в смене.
var BotCommands = []BotCommand{
	{"stop", "Стоп-лист: что нельзя заказать"},
	{"expiring", "Что истекает сегодня"},
	{"hot", "Что продвигать, чтобы не списать"},
	{"order", "Что и сколько заказать"},
	{"ask", "Вопрос помощнику своими словами"},
	{"summary", "Сводка за день"},
	{"week", "Отчёт за неделю"},
	{"settings", "Настройки уведомлений"},
	{"quiet", "Тихие часы: /quiet 22:00-07:00"},
	{"help", "Все команды"},
}

// commandListText — «/stop, /expiring, …» для ответов бота.
func commandListText() string {
	parts := make([]string, len(BotCommands))
	for i, c := range BotCommands {
		parts[i] = "/" + c.Command
	}
	return strings.Join(parts, ", ")
}

// helpText — подробный список: команда и что она делает.
func helpText() string {
	var b strings.Builder
	b.WriteString("Что я умею:\n")
	for _, c := range BotCommands {
		fmt.Fprintf(&b, "/%s — %s\n", c.Command, c.Description)
	}
	b.WriteString("\nМожно просто написать вопрос: «что истекает?», «сколько осталось лосося?».")
	return b.String()
}

// Описание бота: что видит человек в пустом чате до нажатия «Старт» (до 512 символов) и в карточке бота (до 120).
const (
	BotDescription = "SwapOwl следит за остатками и сроками годности: пишет, что истекает, что в стоп-листе и что заказать, " +
		"и подсказывает, как не списать продукты. Нажмите «Старт» — и увидите все команды."
	BotShortDescription = "Остатки, сроки годности и стоп-лист ресторана"
)

// ValidateBotProfile проверяет ограничения Telegram на команды и описания: ошибка — до обращения к API.
func ValidateBotProfile() error {
	if len(BotCommands) == 0 || len(BotCommands) > 100 {
		return fmt.Errorf("команд: %d, нужно 1–100", len(BotCommands))
	}
	seen := map[string]bool{}
	for _, c := range BotCommands {
		if n := len(c.Command); n < 1 || n > 32 || strings.ToLower(c.Command) != c.Command || strings.ContainsAny(c.Command, " /@-") {
			return fmt.Errorf("команда %q: 1–32 символа a-z, 0-9, _", c.Command)
		}
		if n := utf8.RuneCountInString(c.Description); n < 3 || n > 256 {
			return fmt.Errorf("описание команды %q: %d символов, нужно 3–256", c.Command, n)
		}
		if seen[c.Command] {
			return fmt.Errorf("команда %q повторяется", c.Command)
		}
		seen[c.Command] = true
	}
	if n := utf8.RuneCountInString(BotDescription); n > 512 {
		return fmt.Errorf("описание бота: %d символов, максимум 512", n)
	}
	if n := utf8.RuneCountInString(BotShortDescription); n > 120 {
		return fmt.Errorf("короткое описание бота: %d символов, максимум 120", n)
	}
	return nil
}

// SetupBotProfile делает так, чтобы человек, зашедший в бота, сразу видел все команды: меню «/», описание
// в пустом чате и карточке, кнопка mini-app по умолчанию для всех чатов (не только владельца). Вызывается при старте.
func (t *Telegram) SetupBotProfile(ctx context.Context, miniAppURL string) error {
	if err := ValidateBotProfile(); err != nil {
		return err
	}
	var errs []string
	run := func(name string, err error) {
		if err != nil {
			errs = append(errs, name+": "+err.Error())
		}
	}
	run("setMyCommands", t.call(ctx, "setMyCommands", map[string]any{"commands": BotCommands}, nil))
	run("setMyDescription", t.call(ctx, "setMyDescription", map[string]any{"description": BotDescription}, nil))
	run("setMyShortDescription", t.call(ctx, "setMyShortDescription", map[string]any{"short_description": BotShortDescription}, nil))
	if miniAppURL != "" {
		// Без chat_id — кнопка по умолчанию для всех пользователей бота.
		run("setChatMenuButton", t.call(ctx, "setChatMenuButton", map[string]any{
			"menu_button": map[string]any{"type": "web_app", "text": MiniAppText, "web_app": WebAppInfo{URL: miniAppURL}},
		}, nil))
	}
	if len(errs) > 0 {
		return fmt.Errorf("telegram: %s", strings.Join(errs, "; "))
	}
	return nil
}
