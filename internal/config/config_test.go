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

func TestScheduleConfig(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://x"}
	with := func(s string) map[string]string {
		m := map[string]string{"SCHEDULE": s}
		for k, v := range base {
			m[k] = v
		}
		return m
	}
	c, err := Load(env(with("mon-thu 10:00-23:00; fri-sat 11:00-02:00; sun 11:00-22:00")))
	if err != nil || len(c.Days) != 7 {
		t.Fatalf("days=%d err=%v", len(c.Days), err)
	}
	// OPEN_AT/CLOSE_AT остаются значением по умолчанию и проверяются и с SCHEDULE.
	m := with("mon 20:00-02:00")
	m["OPEN_AT"], m["CLOSE_AT"] = "23:00", "10:00"
	if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), "OPEN_AT") {
		t.Fatalf("inverted OPEN_AT/CLOSE_AT with SCHEDULE: err=%v", err)
	}
	for _, bad := range []string{"mon 10:00-14:00; mon 13:00-20:00", "mon 10:00-10:00", "funday 10:00-12:00", "mon 10:00"} {
		if _, err := Load(env(with(bad))); err == nil || !strings.Contains(err.Error(), "SCHEDULE") {
			t.Errorf("%q: err=%v", bad, err)
		}
	}
	if c, err := Load(env(base)); err != nil || c.Days != nil {
		t.Fatalf("no SCHEDULE: days=%v err=%v", c.Days, err)
	}
}
