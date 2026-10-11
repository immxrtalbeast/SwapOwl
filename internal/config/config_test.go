package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.RestaurantTZ != "Europe/Moscow" || c.Location == nil {
		t.Errorf("tz: %q %v", c.RestaurantTZ, c.Location)
	}
	if c.OpenAt != 10*time.Hour || c.CloseAt != 23*time.Hour {
		t.Errorf("hours: %v %v", c.OpenAt, c.CloseAt)
	}
	if c.DemandWindowDays != 14 || c.DemandPriorDays != 3 || c.HotTopN != 3 || c.DBPoolSize != 20 ||
		c.HotOffersAlgo != "b" || !c.SeedOnEmpty || !c.AuthRequired || c.DemoMode || c.ClockMode != "real" || c.LateEventsHours != 2 {
		t.Errorf("unexpected defaults: %+v", c)
	}
}

func TestLoadValidation(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"no database", map[string]string{"DATABASE_URL": ""}, "DATABASE_URL"},
		{"bad tz", map[string]string{"RESTAURANT_TZ": "Mars/Base"}, "RESTAURANT_TZ"},
		{"open after close", map[string]string{"OPEN_AT": "23:00", "CLOSE_AT": "10:00"}, "OPEN_AT"},
		{"open equals close", map[string]string{"OPEN_AT": "10:00", "CLOSE_AT": "10:00"}, "OPEN_AT"},
		{"bad time", map[string]string{"OPEN_AT": "25:00"}, "OPEN_AT"},
		{"n0 zero", map[string]string{"DEMAND_PRIOR_DAYS": "0"}, "n0"},
		{"n0 negative", map[string]string{"DEMAND_PRIOR_DAYS": "-1"}, "n0"},
		{"seed off in demo", map[string]string{"SEED_ON_EMPTY": "false", "DEMO_MODE": "true"}, "SEED_ON_EMPTY"},
		{"admin login only", map[string]string{"ADMIN_LOGIN": "root"}, "ADMIN_LOGIN"},
		{"admin password only", map[string]string{"ADMIN_PASSWORD": "secret"}, "ADMIN_LOGIN"},
		{"bad algo", map[string]string{"HOTOFFERS_ALGO": "c"}, "HOTOFFERS_ALGO"},
		{"negative late events", map[string]string{"LATE_EVENTS_HOURS": "-1"}, "LATE_EVENTS_HOURS"},
		{"huge late events", map[string]string{"LATE_EVENTS_HOURS": "169"}, "LATE_EVENTS_HOURS"},
		{"bad clock mode", map[string]string{"CLOCK_MODE": "fast"}, "CLOCK_MODE"},
		{"not a number", map[string]string{"DB_POOL_SIZE": "many"}, "DB_POOL_SIZE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := map[string]string{"DATABASE_URL": "postgres://x"}
			for k, v := range tc.env {
				m[k] = v
			}
			_, err := Load(env(m))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestSeedOffAllowedWithoutDemo(t *testing.T) {
	_, err := Load(env(map[string]string{"DATABASE_URL": "x", "SEED_ON_EMPTY": "false", "DEMO_MODE": "false"}))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckAdminBootstrap(t *testing.T) {
	cases := []struct {
		name       string
		auth       bool
		login      string
		usersEmpty bool
		wantErr    bool
	}{
		{"auth, empty table, no admin vars", true, "", true, true},
		{"auth, empty table, admin vars", true, "root", true, false},
		{"auth, users exist", true, "", false, false},
		{"no auth, empty table", false, "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{AuthRequired: tc.auth, AdminLogin: tc.login}
			if err := c.CheckAdminBootstrap(tc.usersEmpty); (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestCleanInstanceConfig(t *testing.T) {
	c, err := Load(env(map[string]string{"DATABASE_URL": "x", "DEMO_MODE": "false", "CLOCK_MODE": "real",
		"SEED_ON_EMPTY": "false", "AUTH_REQUIRED": "true"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.DemoMode || c.SeedOnEmpty || !c.AuthRequired || c.ClockMode != "real" {
		t.Fatalf("unexpected: %+v", c)
	}
}

func TestSeedOffInDemoMessageIsClear(t *testing.T) {
	_, err := Load(env(map[string]string{"DATABASE_URL": "x", "SEED_ON_EMPTY": "false", "DEMO_MODE": "true"}))
	if err == nil || !strings.Contains(err.Error(), "DEMO_MODE=true") {
		t.Fatalf("got %v", err)
	}
}
