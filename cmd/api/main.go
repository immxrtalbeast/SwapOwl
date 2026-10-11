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
	"umirhack/internal/generator"
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
	db.LateEvents = time.Duration(cfg.LateEventsHours) * time.Hour // до «дворника» и роутера (2.4а)
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
	orders := pubOrders{db, pub}
	var (
		demo api.Demo
		sim  api.SimControl
	)
	if cfg.DemoMode {
		demo = st
		// Генератор идёт в процессе через тот же путь, что POST /api/orders (2.8).
		st.Sim = stand.NewSimulator(ctx, simSink{api.OrderPlacer{Orders: orders, Now: now, Gate: gate}}, now, simSeed, log)
		sim = st.Sim
	}
	schedule := domain.Schedule{Location: cfg.Location, OpenAt: cfg.OpenAt, CloseAt: cfg.CloseAt}
	monitor := store.Monitor{DB: db, Params: store.AlertParams{Expiry: expiry, Schedule: schedule, WMin: cfg.HotWMinHours}}
	router := api.NewRouter(api.Deps{Log: log, DB: db, Now: now, Auth: authSvc, Clock: clockInfo(st), Demo: demo, Sim: sim, DemoToken: cfg.DemoToken,
		NotSimulated: stand.ErrNotSimulated, Gate: gate,
		Stock: pubStock{db, pub}, Catalog: pubCatalog{db, pub}, Orders: orders, Menu: db, Monitor: monitor, Location: cfg.Location, Events: bus,
		HotOffers: store.Snapshotter{DB: db, Params: store.SnapshotParams{
			WindowDays: float64(cfg.DemandWindowDays), PriorDays: float64(cfg.DemandPriorDays), Schedule: schedule}},
		Supply: store.SupplyReader{DB: db, CoverDays: cfg.SupplyCoverDays, MinPortions: int64(cfg.SupplyMinPortions),
			Snap: store.Snapshotter{DB: db, Params: store.SnapshotParams{
				WindowDays: float64(cfg.DemandWindowDays), PriorDays: float64(cfg.DemandPriorDays), Schedule: schedule}}},
		HotConfig: api.HotOffersConfig{DefaultAlgo: cfg.HotOffersAlgo, Rank: hotoffers.Config{
			TopN: cfg.HotTopN, WMin: cfg.HotWMinHours, CoverHours: cfg.HotCoverHours, MaxIter: cfg.HotMaxIter}}})
	go newHotWatcher(bus, store.Snapshotter{DB: db, Params: store.SnapshotParams{
		WindowDays: float64(cfg.DemandWindowDays), PriorDays: float64(cfg.DemandPriorDays), Schedule: schedule}},
		hotoffers.Config{TopN: cfg.HotTopN, WMin: cfg.HotWMinHours, CoverHours: cfg.HotCoverHours, MaxIter: cfg.HotMaxIter},
		now, log).run(ctx)

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
	if st.Clock == nil && st.Sim == nil {
		return nil
	}
	return st
}

// simSeed — seed генератора заказов на стенде.
const simSeed = 2026

// simSink кладёт заказы генератора через OrderPlacer.
type simSink struct{ p api.OrderPlacer }

func (s simSink) PlaceOrder(ctx context.Context, o generator.Order) error {
	items := make([]store.OrderItem, 0, len(o.Items))
	for _, it := range o.Items {
		items = append(items, store.OrderItem{DishID: it.DishID, Portions: it.Portions})
	}
	_, err := s.p.Place(ctx, o.ID, store.SourceAPI, items)
	return err
}
