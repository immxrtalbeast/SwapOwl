// Package config читает конфигурацию из переменных окружения (.ai/solution.md, 7.6)
// и проверяет её при старте.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // зона ресторана загружается и без tzdata в системе
	"umirhack/internal/domain"
)

// Config — конфигурация сервиса.
type Config struct {
	DatabaseURL string

	RestaurantTZ string
	Location     *time.Location
	OpenAt       time.Duration // от начала суток
	CloseAt      time.Duration
	// Days — расписание по дням недели из SCHEDULE (2.6а); nil — OpenAt/CloseAt во все дни.
	Days map[time.Weekday][]domain.Interval

	ExpiringShare    float64
	ExpiringMinHours float64
	ExpiringMaxShare float64

	DemandWindowDays int // N
	DemandPriorDays  int // n0

	SupplyCoverDays   int
	SupplyMinPortions int

	HotOffersAlgo string
	HotTopN       int
	HotWMinHours  float64
	HotCoverHours float64
	HotMaxIter    int
	HotHorizon    float64

	DemoMode     bool
	DemoToken    string
	SeedOnEmpty  bool
	AuthRequired bool

	AdminLogin    string
	AdminPassword string

	SessionTTLHours int
	SiteDomain      string
	LogLevel        string
	DBPoolSize      int
	ClockMode       string
	// LateEventsHours — окно событий задним числом (2.4а): occurred_at ∈ [now − окно, now],
	// «дворник» списывает партию не раньше expires_at + окно.
	LateEventsHours int

	// SimSeed — seed генератора заказов на стенде; SimFollowHot — слушаются ли гости горячих
	// предложений (демо «со SwapOwl и без»); SimHotUplift — подъём веса блюда на первой позиции
	// списка (модель M2, 2.7), величина выдумана.
	SimSeed      int64
	SimFollowHot bool
	SimHotUplift float64
	// SeedProfile — склад и спрос стенда: "demo" (по умолчанию, сценарий показа) или "ab"
	// (bench «скоропорт в трёх блюдах» для живого A/B на сцене).
	SeedProfile string

	// InvoiceModel — модель распознавания фото накладной; InvoicePerHour — лимит распознаваний
	// в час на экземпляр (деньги за вызовы модели). Ключ — ANTHROPIC_API_KEY, в Config не хранится.
	InvoiceModel   string
	InvoicePerHour int
}

// Load читает окружение через getenv (os.Getenv в проде), подставляет значения
// по умолчанию и проверяет результат.
func Load(getenv func(string) string) (Config, error) {
	var errs []error
	p := parser{getenv: getenv, errs: &errs}

	c := Config{
		DatabaseURL:       getenv("DATABASE_URL"),
		RestaurantTZ:      p.str("RESTAURANT_TZ", "Europe/Moscow"),
		ExpiringShare:     p.float("EXPIRING_SHARE", 0.2),
		ExpiringMinHours:  p.float("EXPIRING_MIN_HOURS", 12),
		ExpiringMaxShare:  p.float("EXPIRING_MAX_SHARE", 0.5),
		DemandWindowDays:  p.int("DEMAND_WINDOW_DAYS", 14),
		DemandPriorDays:   p.int("DEMAND_PRIOR_DAYS", 3),
		SupplyCoverDays:   p.int("SUPPLY_COVER_DAYS", 3),
		SupplyMinPortions: p.int("SUPPLY_MIN_PORTIONS", 10),
		HotOffersAlgo:     p.str("HOTOFFERS_ALGO", "b"),
		HotTopN:           p.int("HOT_TOP_N", 3),
		HotWMinHours:      p.float("HOT_W_MIN_HOURS", 0.5),
		HotCoverHours:     p.float("HOT_COVER_HOURS", 12),
		HotMaxIter:        p.int("HOT_MAX_ITER", 200),
		HotHorizon:        p.float("HOT_HORIZON_HOURS", 24),
		DemoMode:          p.bool("DEMO_MODE", false),
		DemoToken:         getenv("DEMO_TOKEN"),
		SeedOnEmpty:       p.bool("SEED_ON_EMPTY", true),
		AuthRequired:      p.bool("AUTH_REQUIRED", true),
		AdminLogin:        getenv("ADMIN_LOGIN"),
		AdminPassword:     getenv("ADMIN_PASSWORD"),
		SessionTTLHours:   p.int("SESSION_TTL_HOURS", 12),
		SiteDomain:        getenv("SITE_DOMAIN"),
		LogLevel:          p.str("LOG_LEVEL", "info"),
		DBPoolSize:        p.int("DB_POOL_SIZE", 20),
		ClockMode:         p.str("CLOCK_MODE", "real"),
		LateEventsHours:   p.int("LATE_EVENTS_HOURS", 2),
		SimSeed:           int64(p.int("SIM_SEED", 2026)),
		SimFollowHot:      p.bool("SIM_FOLLOW_HOT", true),
		SimHotUplift:      p.float("SIM_HOT_UPLIFT", 1.5),
		SeedProfile:       p.str("SEED_PROFILE", "demo"),
		InvoiceModel:      p.str("INVOICE_MODEL", "claude-haiku-5-5"),
		InvoicePerHour:    p.int("INVOICE_PER_HOUR", 30),
	}
	c.OpenAt = p.clock("OPEN_AT", "10:00")
	c.CloseAt = p.clock("CLOSE_AT", "23:00")

	if v := getenv("SCHEDULE"); strings.TrimSpace(v) != "" {
		days, err := domain.ParseSchedule(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("SCHEDULE: %w", err))
		}
		c.Days = days
	}

	errs = append(errs, c.validate()...)
	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return c, nil
}

func (c *Config) validate() []error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if c.DatabaseURL == "" {
		bad("DATABASE_URL is required")
	}
	loc, err := time.LoadLocation(c.RestaurantTZ)
	if err != nil {
		bad("RESTAURANT_TZ %q: %v", c.RestaurantTZ, err)
	}
	c.Location = loc
	if c.OpenAt >= c.CloseAt {
		bad("OPEN_AT must be earlier than CLOSE_AT (for work across midnight use SCHEDULE)")
	}
	if c.ExpiringShare <= 0 || c.ExpiringMinHours < 0 || c.ExpiringMaxShare <= 0 || c.ExpiringMaxShare > 1 {
		bad("EXPIRING_*: share and max share must be in (0, 1], min hours >= 0")
	}
	if c.DemandWindowDays <= 0 {
		bad("DEMAND_WINDOW_DAYS must be > 0")
	}
	if c.DemandPriorDays <= 0 {
		bad("DEMAND_PRIOR_DAYS (n0) must be > 0")
	}
	if c.SupplyCoverDays < 0 || c.SupplyMinPortions < 0 {
		bad("SUPPLY_COVER_DAYS and SUPPLY_MIN_PORTIONS must be >= 0")
	}
	if c.HotOffersAlgo != "a" && c.HotOffersAlgo != "b" {
		bad("HOTOFFERS_ALGO must be a or b, got %q", c.HotOffersAlgo)
	}
	if c.HotTopN <= 0 || c.HotMaxIter <= 0 || c.HotWMinHours < 0 || c.HotCoverHours < 0 || c.HotHorizon < 0 {
		bad("HOT_*: TOP_N and MAX_ITER must be > 0, hours must be >= 0")
	}
	if !c.SeedOnEmpty && c.DemoMode {
		bad("SEED_ON_EMPTY=false is incompatible with DEMO_MODE=true: the demo stand needs the seed; set DEMO_MODE=false for a restaurant instance")
	}
	if (c.AdminLogin == "") != (c.AdminPassword == "") {
		bad("ADMIN_LOGIN and ADMIN_PASSWORD must be set together")
	}
	if c.SessionTTLHours <= 0 {
		bad("SESSION_TTL_HOURS must be > 0")
	}
	if c.DBPoolSize <= 0 {
		bad("DB_POOL_SIZE must be > 0")
	}
	if c.LateEventsHours < 0 || c.LateEventsHours > 168 {
		bad("LATE_EVENTS_HOURS must be in 0..168")
	}
	if c.SimHotUplift < 1 || c.SimHotUplift > 10 {
		bad("SIM_HOT_UPLIFT must be in 1..10, got %v", c.SimHotUplift)
	}
	if c.InvoicePerHour < 1 || c.InvoicePerHour > 10000 {
		bad("INVOICE_PER_HOUR must be in 1..10000, got %d", c.InvoicePerHour)
	}
	if c.SeedProfile != "demo" && c.SeedProfile != "ab" {
		bad("SEED_PROFILE must be demo or ab, got %q", c.SeedProfile)
	}
	if c.ClockMode != "real" && c.ClockMode != "simulated" {
		bad("CLOCK_MODE must be real or simulated, got %q", c.ClockMode)
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		bad("LOG_LEVEL must be debug, info, warn or error, got %q", c.LogLevel)
	}
	return errs
}

// CheckAdminBootstrap — правило 2.9: при AUTH_REQUIRED=true, пустой таблице
// пользователей и без ADMIN_LOGIN/ADMIN_PASSWORD сервис не запускается.
func (c Config) CheckAdminBootstrap(usersEmpty bool) error {
	if c.AuthRequired && usersEmpty && c.AdminLogin == "" {
		return errors.New("AUTH_REQUIRED=true, no users and no ADMIN_LOGIN/ADMIN_PASSWORD: cannot create first admin")
	}
	return nil
}

type parser struct {
	getenv func(string) string
	errs   *[]error
}

func (p parser) str(key, def string) string {
	if v := p.getenv(key); v != "" {
		return v
	}
	return def
}

func (p parser) int(key string, def int) int {
	v := p.getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		*p.errs = append(*p.errs, fmt.Errorf("%s: %q is not an integer", key, v))
		return def
	}
	return n
}

func (p parser) float(key string, def float64) float64 {
	v := p.getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		*p.errs = append(*p.errs, fmt.Errorf("%s: %q is not a number", key, v))
		return def
	}
	return f
}

func (p parser) bool(key string, def bool) bool {
	v := p.getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		*p.errs = append(*p.errs, fmt.Errorf("%s: %q is not a boolean", key, v))
		return def
	}
	return b
}

// clock разбирает "HH:MM" в смещение от начала суток.
func (p parser) clock(key, def string) time.Duration {
	v := p.str(key, def)
	t, err := time.Parse("15:04", v)
	if err != nil {
		*p.errs = append(*p.errs, fmt.Errorf("%s: %q is not HH:MM", key, v))
		return 0
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute
}
