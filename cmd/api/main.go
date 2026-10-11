// Command api — HTTP + SSE сервер.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"umirhack/internal/api"
	"umirhack/internal/auth"
	"umirhack/internal/clock"
	"umirhack/internal/config"
	"umirhack/internal/domain"
	"umirhack/internal/events"
	"umirhack/internal/hotoffers"
	"umirhack/internal/janitor"
	"umirhack/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if cfg.ClockMode != "real" {
		return fmt.Errorf("CLOCK_MODE=%s is not supported yet", cfg.ClockMode)
	}
	log := newLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DatabaseURL, cfg.DBPoolSize)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return err
	}

	users, err := db.CountUsers(ctx)
	if err != nil {
		return err
	}
	if err := cfg.CheckAdminBootstrap(users == 0); err != nil {
		return err
	}
	authSvc := auth.New(db, auth.Options{
		Required:     cfg.AuthRequired,
		SessionTTL:   time.Duration(cfg.SessionTTLHours) * time.Hour,
		SecureCookie: cfg.SiteDomain != "", // локальный HTTP — cookie без Secure (2.9)
	})
	if err := authSvc.Bootstrap(ctx, cfg.AdminLogin, cfg.AdminPassword); err != nil {
		return fmt.Errorf("auth bootstrap: %w", err)
	}

	clk := clock.Real{}
	bus := events.New(clk.Now)
	pub := newPublisher(bus, db, clk.Now, log)
	pub.prime(ctx)
	go janitor.New(db, clk.Now, pub.onSweep).Run(ctx) // Kick — в тикете управления часами
	schedule := domain.Schedule{Location: cfg.Location, OpenAt: cfg.OpenAt, CloseAt: cfg.CloseAt}
	router := api.NewRouter(api.Deps{Log: log, DB: db, Now: clk.Now, Auth: authSvc,
		Stock: pubStock{db, pub}, Catalog: pubCatalog{db, pub}, Orders: pubOrders{db, pub}, Menu: db, Location: cfg.Location, Events: bus,
		HotOffers: store.Snapshotter{DB: db, Params: store.SnapshotParams{
			WindowDays: float64(cfg.DemandWindowDays), PriorDays: float64(cfg.DemandPriorDays), Schedule: schedule}},
		HotConfig: api.HotOffersConfig{DefaultAlgo: cfg.HotOffersAlgo, Rank: hotoffers.Config{
			TopN: cfg.HotTopN, WMin: cfg.HotWMinHours, CoverHours: cfg.HotCoverHours, MaxIter: cfg.HotMaxIter}}})

	srv := &http.Server{
		Addr:              ":" + envOr("PORT", "8080"),
		Handler:           router.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", srv.Addr))
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	bus.Close() // иначе открытые SSE-соединения держат Shutdown до таймаута
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
