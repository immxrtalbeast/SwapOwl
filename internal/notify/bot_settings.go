package notify

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Данные кнопок /settings: st:t:<тип> — переключатель, st:q — тихие часы, st:p:<часы> — пауза, st:p:0 — «Включить сейчас».
const settingsPrefix = "st:"

func (b *Bot) settingsOn() bool { return b.N != nil && b.N.Settings != nil }

// settingsView строит текст и кнопки /settings по текущему состоянию.
func (n *Notifier) settingsView(ctx context.Context) (string, [][]Button) {
	s := n.Settings
	now := n.now(ctx)
	loc := n.loc(ctx)
	var rows [][]Button
	for _, k := range Kinds {
		mark := "✅ "
		if !s.Enabled(k) {
			mark = "🚫 "
		}
		rows = append(rows, []Button{{Text: mark + KindTitle[k], Data: settingsPrefix + "t:" + string(k)}})
	}
	on, from, to := s.QuietInfo()
	state := "выкл"
	if on {
		state = "вкл"
	}
	rows = append(rows,
		[]Button{{Text: fmt.Sprintf("🌙 Тихие часы %s–%s: %s", from, to, state), Data: settingsPrefix + "q"}},
		[]Button{{Text: "⏸ 1 ч", Data: settingsPrefix + "p:1"}, {Text: "⏸ 4 ч", Data: settingsPrefix + "p:4"}, {Text: "⏸ 8 ч", Data: settingsPrefix + "p:8"}},
		[]Button{{Text: "▶️ Включить сейчас", Data: settingsPrefix + "p:0"}},
	)
	var t strings.Builder
	t.WriteString("⚙️ Настройки уведомлений\nНажмите, чтобы включить или отключить тип.")
	if until := s.PausedUntil(now); !until.IsZero() {
		fmt.Fprintf(&t, "\n⏸ Пауза до %s", until.In(loc).Format("02.01 15:04"))
	} else if s.Muted(now, loc) {
		t.WriteString("\n🌙 Сейчас тихие часы: события копятся до утра, стоп-лист не копится.")
	}
	if q := s.QueueLen(); q > 0 {
		fmt.Fprintf(&t, "\nНакоплено: %d", q)
	}
	t.WriteString("\nИзменить границы тихих часов: /quiet 23:00 08:00")
	return t.String(), rows
}

func (b *Bot) showSettings(ctx context.Context) {
	if !b.settingsOn() {
		b.reply(ctx, "Настройки уведомлений не подключены (не задан NOTIFY_STATE_FILE).")
		return
	}
	text, rows := b.N.settingsView(ctx)
	if err := b.TG.SendKeyboard(ctx, text, rows); err != nil {
		b.logf("/settings: %v", err)
	}
}

// onSettingsButton обрабатывает нажатие и перерисовывает то же сообщение.
func (b *Bot) onSettingsButton(ctx context.Context, msgID int64, data string) {
	if !b.settingsOn() {
		return
	}
	s := b.N.Settings
	kind, arg, _ := strings.Cut(strings.TrimPrefix(data, settingsPrefix), ":")
	flush := false
	switch kind {
	case "t":
		for _, k := range Kinds {
			if string(k) == arg {
				s.Toggle(k)
			}
		}
	case "q":
		on, _, _ := s.QuietInfo()
		_ = s.SetQuiet(!on, "", "")
		flush = on
	case "p":
		h := map[string]int{"1": 1, "4": 4, "8": 8}[arg]
		if arg != "0" && h == 0 {
			return
		}
		s.Pause(b.N.now(ctx), time.Duration(h)*time.Hour)
		flush = h == 0
	default:
		return
	}
	if flush {
		b.N.FlushQuiet(ctx)
	}
	text, rows := b.N.settingsView(ctx)
	if msgID != 0 {
		if err := b.TG.EditKeyboard(ctx, msgID, text, rows); err != nil {
			b.logf("/settings: %v", err)
		}
		return
	}
	if err := b.TG.SendKeyboard(ctx, text, rows); err != nil {
		b.logf("/settings: %v", err)
	}
}

// quietCommand — «/quiet ЧЧ:ММ ЧЧ:ММ» задаёт границы тихих часов и включает их.
func (b *Bot) quietCommand(ctx context.Context, text string) {
	if !b.settingsOn() {
		b.reply(ctx, "Настройки уведомлений не подключены (не задан NOTIFY_STATE_FILE).")
		return
	}
	f := strings.Fields(text)
	if len(f) != 3 {
		b.reply(ctx, "Формат: /quiet 22:00 07:00")
		return
	}
	if err := b.N.Settings.SetQuiet(true, f[1], f[2]); err != nil {
		b.reply(ctx, "Не понял время: "+err.Error()+". Пример: /quiet 22:00 07:00")
		return
	}
	b.reply(ctx, fmt.Sprintf("🌙 Тихие часы включены: %s–%s (время ресторана).", f[1], f[2]))
}
