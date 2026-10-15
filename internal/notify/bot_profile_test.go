package notify

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBotProfileIsValid(t *testing.T) {
	if err := ValidateBotProfile(); err != nil {
		t.Fatal(err)
	}
}

// Каждая команда меню «/» реально обрабатывается ботом: нельзя показать пользователю то, чего нет.
func TestEveryMenuCommandIsHandled(t *testing.T) {
	_, b, tgf, _ := newN(t)
	for _, c := range BotCommands {
		before := len(tgf.sent("sendMessage"))
		b.HandleUpdate(context.Background(), msgUpdate(botChat, "/"+c.Command))
		got := tgf.sent("sendMessage")
		if len(got) == before {
			t.Errorf("/%s: ответа нет", c.Command)
			continue
		}
		if strings.Contains(got[len(got)-1], "Неизвестная команда") {
			t.Errorf("/%s: бот считает команду неизвестной", c.Command)
		}
	}
}

func TestStartShowsAllCommands(t *testing.T) {
	_, b, tgf, _ := newN(t)
	b.HandleUpdate(context.Background(), msgUpdate(botChat, "/start"))
	m := tgf.sent("sendMessage")
	if len(m) == 0 {
		t.Fatal("нет ответа на /start")
	}
	for _, c := range BotCommands {
		if !strings.Contains(m[0], "/"+c.Command) {
			t.Errorf("/start не упоминает /%s: %s", c.Command, m[0])
		}
	}
}

// Профиль бота уходит в Telegram: список команд, описания и кнопка mini-app по умолчанию (без chat_id).
func TestSetupBotProfileCalls(t *testing.T) {
	_, b, tgf, _ := newN(t)
	if err := b.TG.SetupBotProfile(context.Background(), "https://app.example/mini"); err != nil {
		t.Fatal(err)
	}
	cmds := tgf.sent("setMyCommands")
	if len(cmds) != 1 {
		t.Fatalf("setMyCommands: %v", cmds)
	}
	var p struct {
		Commands []BotCommand `json:"commands"`
	}
	_, body, _ := strings.Cut(cmds[0], "|")
	if err := json.Unmarshal([]byte(body), &p); err != nil || len(p.Commands) != len(BotCommands) || p.Commands[0].Command != "stop" {
		t.Fatalf("%v %s", err, cmds[0])
	}
	if d := tgf.sent("setMyDescription"); len(d) != 1 || !strings.Contains(d[0], "SwapOwl") {
		t.Fatalf("описание: %v", d)
	}
	if d := tgf.sent("setMyShortDescription"); len(d) != 1 {
		t.Fatalf("короткое описание: %v", d)
	}
	mb := tgf.sent("setChatMenuButton")
	if len(mb) != 1 || strings.Contains(mb[0], "chat_id") || !strings.Contains(mb[0], `"type":"web_app"`) {
		t.Fatalf("кнопка по умолчанию: %v", mb)
	}
	// Без mini-app кнопка не трогается.
	if err := b.TG.SetupBotProfile(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if mb := tgf.sent("setChatMenuButton"); len(mb) != 1 {
		t.Fatalf("кнопка без MINIAPP_URL: %v", mb)
	}
}
