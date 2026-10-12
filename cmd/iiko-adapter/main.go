// Command iiko-adapter переносит данные из iiko в наш API и стоп-лист обратно (docs/iiko.md, «Адаптер»).
// О БД процесс ничего не знает: только HTTP к iiko и к нашему API по ключу.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"umirhack/internal/iiko"
	"umirhack/internal/iiko/client"
	"umirhack/internal/iiko/mapping"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "iiko-adapter:", err)
		os.Exit(1)
	}
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func envInt(name string, def int64) (int64, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return n, nil
}

func run(args []string) error {
	fs := flag.NewFlagSet("iiko-adapter", flag.ContinueOnError)
	once := fs.Bool("once", false, "один проход и выход")
	rec := len(args) > 0 && args[0] == "reconcile"
	rep := len(args) > 0 && args[0] == "shadow-report"
	asJSON := fs.Bool("json", false, "reconcile, shadow-report: вывод в JSON")
	all := fs.Bool("all", false, "reconcile, shadow-report: все продукты сверки, не только расхождения")
	day := fs.String("date", "", "shadow-report: день yyyy-MM-dd (по умолчанию сегодня)")
	if rec || rep {
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	var missing []string
	for _, n := range []string{"IIKO_URL", "IIKO_LOGIN", "IIKO_PASSWORD", "API_URL", "API_KEY"} {
		if os.Getenv(n) == "" {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("не заданы переменные окружения: %v", missing)
	}
	loc, err := time.LoadLocation(env("ADAPTER_TZ", "Europe/Moscow"))
	if err != nil {
		return fmt.Errorf("ADAPTER_TZ: %w", err)
	}
	interval, err := time.ParseDuration(env("ADAPTER_INTERVAL", "5m"))
	if err != nil || interval <= 0 {
		return fmt.Errorf("ADAPTER_INTERVAL: нужна положительная длительность")
	}
	var start time.Time
	if v := os.Getenv("ADAPTER_START_DATE"); v != "" {
		if start, err = time.ParseInLocation(iiko.DateLayout, v, loc); err != nil {
			return fmt.Errorf("ADAPTER_START_DATE: %w", err)
		}
	}
	price, err := envInt("ADAPTER_DEFAULT_PRICE_MINOR_PER_1000", 10000)
	if err != nil {
		return err
	}
	lead, err := envInt("ADAPTER_DEFAULT_LEAD_DAYS", 2)
	if err != nil {
		return err
	}
	pack, err := envInt("ADAPTER_DEFAULT_PACK_SIZE", 1000)
	if err != nil {
		return err
	}
	prior, err := strconv.ParseFloat(env("ADAPTER_DEFAULT_PRIOR_DAILY", "10"), 64)
	if err != nil {
		return fmt.Errorf("ADAPTER_DEFAULT_PRIOR_DAILY: %w", err)
	}
	mode := env("ADAPTER_MODE", modeShadow)
	if mode != modeShadow && mode != modeLive {
		return fmt.Errorf("ADAPTER_MODE: %q, нужно shadow или live", mode)
	}
	outboxEvery, err := time.ParseDuration(env("ADAPTER_OUTBOX_INTERVAL", "10s"))
	if err != nil || outboxEvery <= 0 {
		return fmt.Errorf("ADAPTER_OUTBOX_INTERVAL: нужна положительная длительность")
	}
	retryBase, err := time.ParseDuration(env("ADAPTER_RETRY_BASE", "10s"))
	if err != nil || retryBase <= 0 {
		return fmt.Errorf("ADAPTER_RETRY_BASE: нужна положительная длительность")
	}
	retryMax, err := time.ParseDuration(env("ADAPTER_RETRY_MAX", "10m"))
	if err != nil || retryMax < retryBase {
		return fmt.Errorf("ADAPTER_RETRY_MAX: длительность не меньше ADAPTER_RETRY_BASE")
	}
	hc := &http.Client{Timeout: 60 * time.Second}
	a := &adapter{
		iiko:      client.New(os.Getenv("IIKO_URL"), os.Getenv("IIKO_LOGIN"), os.Getenv("IIKO_PASSWORD"), hc),
		api:       &apiClient{base: os.Getenv("API_URL"), key: os.Getenv("API_KEY"), hc: hc},
		loc:       loc,
		now:       time.Now,
		statePath: env("ADAPTER_STATE_FILE", "iiko-adapter-state.json"),
		start:     start,
		defaults:  mapping.Defaults{PriceMinorPer1000: price, LeadDays: int(lead), PackSize: pack, PriorDaily: prior},
		log:       log.New(os.Stderr, "iiko-adapter: ", log.LstdFlags),

		mode:       mode,
		shadowPath: env("ADAPTER_SHADOW_FILE", "iiko-adapter-shadow.json"),
		retryBase:  retryBase,
		retryMax:   retryMax,
		retryAt:    map[int64]time.Time{},
		supplierID: os.Getenv("ADAPTER_SUPPLIER_ID"),
		storeID:    os.Getenv("ADAPTER_STORE_ID"),
	}
	a.log.Printf("режим: %s", mode)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if rec {
		return a.runReconcile(ctx, os.Stdout, *asJSON, *all)
	}
	if rep {
		return a.runShadowReport(ctx, os.Stdout, *asJSON, *all, *day)
	}
	if *once {
		return a.runOnce(ctx)
	}
	// Полный проход — раз в interval; outbox (стоп-лист) опрашивается чаще. Первый проход
	// при старте сразу дочитывает недоставленное.
	var nextSync time.Time
	for {
		var err error
		if now := a.now(); !now.Before(nextSync) {
			err = a.runOnce(ctx)
			nextSync = now.Add(interval)
		} else {
			err = a.processOutbox(ctx)
		}
		if err != nil {
			a.log.Printf("проход с ошибками: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(outboxEvery):
		}
	}
}
