// Command notifier шлёт управляющему в Telegram истекающие партии и дневную сводку (docs/notify.md).
// О БД процесс ничего не знает: только наш API по ключу viewer и Telegram Bot API.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	chat := env("NOTIFY_TG_CHAT", os.Getenv("TELEGRAM_CHAT_ID"))
	for _, n := range []string{"API_URL", "API_KEY", "TELEGRAM_BOT_TOKEN"} {
		if os.Getenv(n) == "" {
			missing = append(missing, n)
		}
	}
	if chat == "" {
		missing = append(missing, "NOTIFY_TG_CHAT")
	}
	if len(missing) > 0 {
		return fmt.Errorf("не заданы переменные окружения: %v", missing)
	}
	digestAt := env("NOTIFY_DIGEST_AT", "22:00")
	if _, err := time.Parse("15:04", digestAt); err != nil {
		return fmt.Errorf("NOTIFY_DIGEST_AT: нужен формат ЧЧ:ММ")
	}
	lg := log.New(os.Stderr, "notifier: ", log.LstdFlags)
	hc := &http.Client{Timeout: 30 * time.Second}
	api := &notify.APIClient{Base: os.Getenv("API_URL"), Key: os.Getenv("API_KEY"), HC: hc}
	n := &notify.Notifier{
		API:      api,
		Out:      &notify.Telegram{Base: os.Getenv("TELEGRAM_API_URL"), Token: os.Getenv("TELEGRAM_BOT_TOKEN"), ChatID: chat, HC: hc},
		DigestAt: digestAt,
		Logf:     lg.Printf,
	}
	bot := &notify.Bot{
		N:      n,
		TG:     &notify.Telegram{Base: os.Getenv("TELEGRAM_API_URL"), Token: os.Getenv("TELEGRAM_BOT_TOKEN"), ChatID: chat, HC: &http.Client{Timeout: 60 * time.Second}},
		ChatID: chat,
	}
	if k := os.Getenv("API_KEY_OPERATOR"); k != "" {
		bot.Act = &notify.APIClient{Base: os.Getenv("API_URL"), Key: k, HC: hc}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	n.MarkDigestStartup(ctx)
	n.SeedSent(ctx)
	bot.SeedReceiving(ctx)
	go bot.Run(ctx)
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				n.Tick(ctx)
				bot.NotifyReceiving(ctx)
			}
		}
	}()
	api.Subscribe(ctx, 5*time.Second, func() { go n.CatchUp(ctx); go bot.NotifyReceiving(ctx) }, func(ev notify.Event) {
		n.Handle(ctx, ev)
		if ev.Type == "delivery.accepted" {
			bot.NotifyReceiving(ctx)
		}
	}, lg.Printf)
	return nil
}
