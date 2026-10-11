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
	"umirhack/internal/stand"
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
	if cfg.DemoMode && cfg.DemoToken == "" {
		return errors.New("DEMO_MODE=true requires DEMO_TOKEN")
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

	// Сид на пустую БД (2.8): до часов, чтобы начальное время ускоренных часов было T0.
	t0 := stand.T0(time.Now(), cfg.Location)
	if cfg.SeedOnEmpty {
		loaded, err := stand.Load(ctx, db, t0)
		if err != nil {
			return fmt.Errorf("seed on empty: %w", err)
		}
		if loaded {
			log.Info("seed loaded", slog.Time("t0", t0))
		}
	}

	var (
		now  func() time.Time = clock.Real{}.Now
		accl *clock.Accelerated
	)
	if cfg.ClockMode == "simulated" {
		last, err := db.LastEventTime(ctx)
		if err != nil {
			return err
		}
		// Нет сохранённого состояния — старт с T0; иначе время не идёт назад (2.4).
		accl, err = clock.NewAccelerated(ctx, db.ClockStore(), t0, last)
		if err != nil {
			return fmt.Errorf("clock: %w", err)
		}
		now = accl.Now
		go accl.Run(ctx, 5*time.Second) // сохранение раз в 5 с (2.4)
	}
	gate := &stand.Gate{}
	bus := events.New(now)
	pub := newPublisher(bus, db, now, log)
	pub.prime(ctx)
	jan := janitor.New(stand.GatedJanitorStore{Inner: db, Gate: gate}, now, pub.onSweep)
	expiry := domain.ExpiryConfig{Share: cfg.ExpiringShare, MinHours: cfg.ExpiringMinHours, MaxShare: cfg.ExpiringMaxShare}
	jan.Expiry, jan.OnExpiring = &expiry, pub.onExpiring
	go jan.Run(ctx)
	st := &stand.Stand{DB: db, Clock: accl, Gate: gate, Sweep: jan, Bus: bus, Loc: cfg.Location,
		AfterChange: pub.recheckStopList}
	var demo api.Demo
	if cfg.DemoMode {
		demo = st
	}
	schedule := domain.Schedule{Location: cfg.Location, OpenAt: cfg.OpenAt, CloseAt: cfg.CloseAt}
	monitor := store.Monitor{DB: db, Params: store.AlertParams{Expiry: expiry, Schedule: schedule, WMin: cfg.HotWMinHours}}
	router := api.NewRouter(api.Deps{Log: log, DB: db, Now: now, Auth: authSvc, Clock: clockInfo(st), Demo: demo, DemoToken: cfg.DemoToken,
		NotSimulated: stand.ErrNotSimulated, Gate: gate,
		Stock: pubStock{db, pub}, Catalog: pubCatalog{db, pub}, Orders: pubOrders{db, pub}, Menu: db, Monitor: monitor, Location: cfg.Location, Events: bus,
		HotOffers: store.Snapshotter{DB: db, Params: store.SnapshotParams{
			WindowDays: float64(cfg.DemandWindowDays), PriorDays: float64(cfg.DemandPriorDays), Schedule: schedule}},
		Supply: store.SupplyReader{DB: db, CoverDays: cfg.SupplyCoverDays, MinPortions: int64(cfg.SupplyMinPortions),
			Snap: store.Snapshotter{DB: db, Params: store.SnapshotParams{
				WindowDays: float64(cfg.DemandWindowDays), PriorDays: float64(cfg.DemandPriorDays), Schedule: schedule}}},
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

// clockInfo даёт GET /api/clock данные ускоренных часов; для реальных — nil (speed=1, simulated=false).
func clockInfo(st *stand.Stand) api.ClockInfo {
	if st.Clock == nil {
		return nil
	}
	return st
}
