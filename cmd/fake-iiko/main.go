// Command fake-iiko — фейковый iikoServer для разработки адаптера (docs/iiko.md).
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"umirhack/internal/iiko/fake"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fake-iiko:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("fake-iiko", flag.ContinueOnError)
	addr := fs.String("addr", ":8081", "адрес")
	seed := fs.Int64("seed", 1, "seed данных")
	origin := fs.String("origin", "2026-01-01", "начало истории, yyyy-MM-dd (Москва)")
	login := fs.String("login", os.Getenv("FAKE_IIKO_LOGIN"), "логин (или FAKE_IIKO_LOGIN)")
	pass := fs.String("pass", os.Getenv("FAKE_IIKO_PASSWORD"), "пароль (или FAKE_IIKO_PASSWORD)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return err
	}
	o, err := time.ParseInLocation("2006-01-02", *origin, loc)
	if err != nil {
		return fmt.Errorf("origin: %w", err)
	}
	s, err := fake.New(fake.Options{Seed: *seed, Origin: o, Login: *login, Password: *pass})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr: *addr, Handler: s.Handler(),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sc)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
