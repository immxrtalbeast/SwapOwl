// Команда hub — сервис-сводка по точкам сети (docs/network.md).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"umirhack/internal/hub"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfgPath := os.Getenv("HUB_CONFIG")
	token := os.Getenv("HUB_TOKEN")
	if cfgPath == "" || token == "" {
		log.Fatal("hub: HUB_CONFIG and HUB_TOKEN are required")
	}
	cfg, err := hub.LoadConfig(cfgPath)
	if err != nil {
		return err
	}
	poll, err := hub.PollInterval(os.Getenv("HUB_POLL"))
	if err != nil {
		return err
	}
	abPoll, err := hub.ABPollInterval(os.Getenv("HUB_AB_POLL"))
	if err != nil {
		return err
	}
	addr := os.Getenv("HUB_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	timeout := poll / 2
	if timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	col := hub.NewCollector(cfg, timeout, nil)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go col.Run(ctx, poll)
	go col.RunAB(ctx, abPoll) // точки группы ab — часто, ради живого сравнения

	srv := &http.Server{Addr: addr, Handler: hub.NewServer(col, token), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sc)
	}()
	log.Printf("hub: %d points, poll %s, listening on %s", len(cfg.Points), poll, addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}
