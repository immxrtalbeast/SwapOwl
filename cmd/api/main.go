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
	"umirhack/internal/clock"
	"umirhack/internal/config"
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

	clk := clock.Real{}
	router := api.NewRouter(api.Deps{Log: log, DB: db, Now: clk.Now})
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
