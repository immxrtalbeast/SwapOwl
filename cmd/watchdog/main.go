// Command watchdog оповещает инженера в Telegram, когда API или синхронизация адаптера
// не работают (docs/operations.md, «Оповещения»). О БД не знает: только HTTP.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"umirhack/internal/clock"
	"umirhack/internal/watchdog"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "watchdog:", err)
		os.Exit(1)
	}
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func envDur(name, def string) (time.Duration, error) {
	d, err := time.ParseDuration(env(name, def))
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s: нужна положительная длительность, например %s", name, def)
	}
	return d, nil
}

func run() error {
	for _, n := range []string{"WATCHDOG_API_URL", "WATCHDOG_TG_TOKEN", "WATCHDOG_TG_CHAT"} {
		if os.Getenv(n) == "" {
			return fmt.Errorf("не задана переменная окружения %s", n)
		}
	}
	cfg := watchdog.Config{
		APIURL:       os.Getenv("WATCHDOG_API_URL"),
		MetricsToken: os.Getenv("WATCHDOG_METRICS_TOKEN"),
		AdapterURL:   os.Getenv("WATCHDOG_ADAPTER_URL"),
		DataDir:      os.Getenv("WATCHDOG_DATA_DIR"),
	}
	var err error
	var every time.Duration
	for _, p := range []struct {
		dst  *time.Duration
		name string
		def  string
	}{{&every, "WATCHDOG_EVERY", "30s"}, {&cfg.SyncMax, "WATCHDOG_SYNC_MAX", "15m"},
		{&cfg.OutboxMax, "WATCHDOG_OUTBOX_MAX", "10m"}, {&cfg.Repeat, "WATCHDOG_REPEAT", "1h"}} {
		if *p.dst, err = envDur(p.name, p.def); err != nil {
			return err
		}
	}
	if v := os.Getenv("WATCHDOG_DISK_MIN_FREE"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 || f >= 1 {
			return fmt.Errorf("WATCHDOG_DISK_MIN_FREE: доля от 0 до 1, например 0.1")
		}
		cfg.DiskMinFree = f
	}
	tg := watchdog.Telegram{Token: os.Getenv("WATCHDOG_TG_TOKEN"), Chat: os.Getenv("WATCHDOG_TG_CHAT")}
	w := watchdog.New(cfg, nil, clock.Real{}, tg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	w.Run(ctx, every)
	return nil
}
