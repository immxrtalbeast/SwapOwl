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
	"sync/atomic"
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
	"umirhack/internal/metrics"
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
		// clockDone закрывается после финального сохранения часов; nil — часы не ускоренные.
		clockDone chan struct{}
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
		clockDone = make(chan struct{})
		go func() { // сохранение раз в 5 с и финальное при остановке (2.4)
			defer close(clockDone)
			accl.Run(ctx, 5*time.Second)
		}()
	}
	db.LateEvents = time.Duration(cfg.LateEventsHours) * time.Hour // до «дворника» и роутера (2.4а)
	gate := &stand.Gate{}
	bus := events.New(now)
	pub := newPublisher(bus, db, now, log)
	pub.outbox = db
	pub.prime(ctx)
	sweeps := &sweepStamp{Store: stand.GatedJanitorStore{Inner: db, Gate: gate}}
	sweeps.last.Store(time.Now().UnixNano()) // до первого прохода отсчёт идёт от старта
	jan := janitor.New(sweeps, now, pub.onSweep)
	expiry := domain.ExpiryConfig{Share: cfg.ExpiringShare, MinHours: cfg.ExpiringMinHours, MaxShare: cfg.ExpiringMaxShare}
	db.Expiry = expiry
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
	schedule := domain.Schedule{Location: cfg.Location, OpenAt: cfg.OpenAt, CloseAt: cfg.CloseAt, Days: cfg.Days}
	monitor := store.Monitor{DB: db, Params: store.AlertParams{Expiry: expiry, Schedule: schedule, WMin: cfg.HotWMinHours}}
	reg := metrics.New()
	reg.Gauge("sse_subscribers", "Открытые SSE-подключения.", func() float64 { return float64(bus.Subscribers()) })
	reg.Gauge("janitor_last_sweep_age_seconds", "Секунд с последнего успешного прохода «дворника» (реальное время).",
		func() float64 { return time.Since(time.Unix(0, sweeps.last.Load())).Seconds() })
	reg.Gauge("outbox_undelivered", "Недоставленные исходящие сообщения.", func() float64 {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return float64(countOutbox(c, db))
	})
	router := api.NewRouter(api.Deps{Metrics: reg, MetricsToken: os.Getenv("METRICS_TOKEN"), Log: log, DB: db, Now: now, Auth: authSvc, Audit: db, Clock: clockInfo(st), Demo: demo, Sim: sim, DemoToken: cfg.DemoToken,
		NotSimulated: stand.ErrNotSimulated, Gate: gate,
		Stock: pubStock{db, pub}, Receiving: pubReceiving{db, pub}, Catalog: pubCatalog{db, pub}, Orders: orders, Corrections: pubCorrections{db, pub}, Menu: db, Outbox: db, SupplyOrders: db, Monitor: monitor, Analytics: db, Location: cfg.Location, Events: bus,
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
		err = fmt.Errorf("shutdown: %w", err)
	} else {
		err = nil
	}
	// Финальное сохранение часов — до закрытия пула БД (defer db.Close).
	if clockDone != nil {
		select {
		case <-clockDone:
		case <-time.After(10 * time.Second):
			log.Warn("clock: final persist timed out")
		}
	}
	return err
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

// pubCorrections публикует stock.changed (и stoplist.changed по разности стоп-листа) после
// новой корректировки остатка; replay событий не даёт.
type pubCorrections struct {
	api.Corrections
	p *publisher
}

func (c pubCorrections) WriteOffBatch(ctx context.Context, in store.WriteoffInput, now func() time.Time) (store.WriteoffResult, error) {
	res, err := c.Corrections.WriteOffBatch(ctx, in, now)
	if err == nil && !res.Replayed {
		c.p.stockChanged(ctx, []string{res.ProductID})
	}
	return res, err
}

func (c pubCorrections) Inventory(ctx context.Context, in store.InventoryInput, now func() time.Time) (store.InventoryResult, error) {
	res, err := c.Corrections.Inventory(ctx, in, now)
	if err == nil && !res.Replayed && (len(res.Shortage) > 0 || res.Surplus != nil) {
		c.p.stockChanged(ctx, []string{res.ProductID})
	}
	return res, err
}

func (c pubCorrections) CancelOrder(ctx context.Context, in store.CancelInput, now func() time.Time) (store.CancelResult, error) {
	res, err := c.Corrections.CancelOrder(ctx, in, now)
	if err != nil || res.Replayed {
		return res, err
	}
	var products []string
	for _, r := range res.Returned {
		products = append(products, r.ProductID)
	}
	if len(products) > 0 {
		c.p.stockChanged(ctx, uniqSorted(products))
	}
	return res, nil
}

// pubReceiving публикует stock.changed после подтверждения срока: от срока зависят доступность
// партии и порог «истекает».
type pubReceiving struct {
	api.Receiving
	p *publisher
}

func (r pubReceiving) ConfirmExpiry(ctx context.Context, in store.ExpiryInput, now func() time.Time, loc *time.Location) (store.ExpiryResult, error) {
	res, err := r.Receiving.ConfirmExpiry(ctx, in, now, loc)
	if err == nil {
		r.p.stockChanged(ctx, []string{res.ProductID})
	}
	return res, err
}

// sweepStamp запоминает время последнего успешного прохода «дворника» для метрики отставания.
type sweepStamp struct {
	janitor.Store
	last atomic.Int64 // UnixNano реального времени
}

func (s *sweepStamp) Sweep(ctx context.Context, now func() time.Time, cfg *domain.ExpiryConfig) (store.SweepResult, error) {
	res, err := s.Store.Sweep(ctx, now, cfg)
	if err == nil {
		s.last.Store(time.Now().UnixNano())
	}
	return res, err
}

// countOutbox считает недоставленные исходящие; нет таблицы или ошибка БД — 0 (8.0а).
func countOutbox(ctx context.Context, db *store.DB) int {
	n, after := 0, int64(0)
	for {
		list, err := db.ListOutbox(ctx, after, store.MaxOutboxLimit)
		if err != nil || len(list) == 0 {
			return n
		}
		n += len(list)
		after = list[len(list)-1].ID
	}
}
