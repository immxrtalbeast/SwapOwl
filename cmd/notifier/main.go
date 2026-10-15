// Command notifier шлёт управляющему в Telegram истекающие партии и дневную сводку (docs/notify.md).
// О БД процесс ничего не знает: только наш API по ключу viewer и Telegram Bot API.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"umirhack/internal/notify"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "notifier:", err)
		os.Exit(1)
	}
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func run() error {
	var missing []string
	for _, n := range []string{"API_URL", "API_KEY", "TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID"} {
		if os.Getenv(n) == "" && !(n == "TELEGRAM_CHAT_ID" && os.Getenv("NOTIFY_TG_CHAT") != "") {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("не заданы переменные окружения: %v", missing)
	}
	digestAt := env("NOTIFY_DIGEST_AT", "22:00")
	if _, err := time.Parse("15:04", digestAt); err != nil {
		return fmt.Errorf("NOTIFY_DIGEST_AT: нужен формат ЧЧ:ММ")
	}
	shiftAt := env("NOTIFY_SHIFT_AT", "10:00")
	if _, err := time.Parse("15:04", shiftAt); err != nil {
		return fmt.Errorf("NOTIFY_SHIFT_AT: нужен формат ЧЧ:ММ")
	}
	hallChat := os.Getenv("NOTIFY_HALL_CHAT")
	if hallChat != "" {
		if _, err := strconv.ParseInt(hallChat, 10, 64); err != nil {
			return fmt.Errorf("NOTIFY_HALL_CHAT: нужен числовой id чата")
		}
	}
	miniApp := os.Getenv("MINIAPP_URL")
	if miniApp != "" {
		if u, err := url.Parse(miniApp); err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("MINIAPP_URL: нужен адрес https://…")
		}
	}
	lg := log.New(os.Stderr, "notifier: ", log.LstdFlags)
	hc := &http.Client{Timeout: 30 * time.Second}
	api := &notify.APIClient{Base: os.Getenv("API_URL"), Key: os.Getenv("API_KEY"), HC: hc}
	chat := env("NOTIFY_TG_CHAT", os.Getenv("TELEGRAM_CHAT_ID"))
	chatID, err := strconv.ParseInt(chat, 10, 64)
	if err != nil {
		return fmt.Errorf("NOTIFY_TG_CHAT: нужен числовой id чата")
	}
	tg := &notify.Telegram{Base: os.Getenv("TELEGRAM_API_URL"), Token: os.Getenv("TELEGRAM_BOT_TOKEN"), ChatID: chat, HC: &http.Client{Timeout: 60 * time.Second}}
	n := &notify.Notifier{
		API:      api,
		Out:      tg,
		Buttons:  tg,
		DigestAt: digestAt,
		Logf:     lg.Printf,
	}
	if owner := os.Getenv("NOTIFY_OWNER_CHAT"); owner != "" {
		weeklyAt := env("NOTIFY_WEEKLY_AT", "09:00")
		if _, err := time.Parse("15:04", weeklyAt); err != nil {
			return fmt.Errorf("NOTIFY_WEEKLY_AT: нужен формат ЧЧ:ММ")
		}
		otg := &notify.Telegram{Base: tg.Base, Token: tg.Token, ChatID: owner, HC: tg.HC}
		n.Weekly = &notify.WeeklyConfig{At: weeklyAt, Text: otg, Photo: otg, StandURL: os.Getenv("NOTIFY_STAND_URL")}
	}
	if hallChat != "" {
		n.Hall = &notify.HallConfig{At: shiftAt, Out: &notify.Telegram{Base: tg.Base, Token: tg.Token, ChatID: hallChat, HC: tg.HC}}
	}
	bot := &notify.Bot{N: n, TG: tg, ChatID: chatID, Logf: lg.Printf, MiniAppURL: miniApp}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if miniApp != "" {
		if err := tg.SetChatMenuButton(ctx, miniApp); err != nil {
			lg.Printf("кнопка mini-app: %v", err)
		}
	}
	n.MarkHallStartup(ctx)
	n.SeedStop(ctx)
	n.MarkDigestStartup(ctx)
	n.MarkWeeklyStartup(ctx)
	n.SeedSent(ctx)
	go bot.Run(ctx, 5*time.Second)
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				n.Tick(ctx)
			}
		}
	}()
	api.Subscribe(ctx, 5*time.Second, func() { go n.CatchUp(ctx) }, func(ev notify.Event) { n.Handle(ctx, ev) }, lg.Printf)
	return nil
}
