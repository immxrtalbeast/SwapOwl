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
)

// Config — конфигурация сервиса.
type Config struct {
	DatabaseURL string

	RestaurantTZ string
	Location     *time.Location
	OpenAt       time.Duration // от начала суток
	CloseAt      time.Duration

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
	}
	c.OpenAt = p.clock("OPEN_AT", "10:00")
	c.CloseAt = p.clock("CLOSE_AT", "23:00")

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
		bad("OPEN_AT must be earlier than CLOSE_AT (work across midnight is not supported)")
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
	if c.HotTopN <= 0 || c.HotMaxIter <= 0 || c.HotWMinHours < 0 || c.HotCoverHours < 0 {
		bad("HOT_*: TOP_N and MAX_ITER must be > 0, hours must be >= 0")
	}
	if !c.SeedOnEmpty && c.DemoMode {
		bad("SEED_ON_EMPTY=false requires DEMO_MODE=false")
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
