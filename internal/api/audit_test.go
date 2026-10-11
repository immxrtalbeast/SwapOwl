package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"umirhack/internal/audit"
	"umirhack/internal/auth"
)

func newAuditEnv(t *testing.T) (*authEnv, *audit.Mem) {
	t.Helper()
	e := newAuthEnv(t, true)
	sink := &audit.Mem{}
	e.rt.deps.Audit = sink
	e.rt.auditRoutes()
	return e, sink
}

func actions(t *testing.T, sink *audit.Mem) []string {
	t.Helper()
	items, _ := sink.ListAudit(context.Background(), 100)
	var out []string
	for i := len(items) - 1; i >= 0; i-- {
		out = append(out, items[i].Action)
	}
	return out
}

func TestAuditEventsReachDBWithActor(t *testing.T) {
	e, sink := newAuditEnv(t)
	e.addUser(t, "root", "password1", auth.Admin)

	e.do(req{method: "POST", path: "/api/auth/login", body: `{"login":"root","password":"nope"}`})
	cookie := e.login(t, "root", "password1")
	e.do(req{method: "POST", path: "/api/users", cookie: cookie, body: `{"login":"bob","password":"password1","role":"viewer"}`})
	e.do(req{method: "POST", path: "/api/auth/logout", cookie: cookie})

	got := strings.Join(actions(t, sink), ",")
	if got != "login_failed,login_ok,user_created,logout" {
		t.Fatalf("actions: %s", got)
	}
	items, _ := sink.ListAudit(context.Background(), 10)
	byAction := map[string]audit.Entry{}
	for _, it := range items {
		byAction[it.Action] = it
	}
	if a := byAction["login_failed"].Actor; a != "login:root" {
		t.Errorf("login_failed actor: %s", a)
	}
	if a := byAction["login_ok"].Actor; a != "user:root" {
		t.Errorf("login_ok actor: %s", a)
	}
	c := byAction["user_created"]
	if c.Actor != "user:root" || c.Details["target_login"] != "bob" || c.Details["request_id"] == "" {
		t.Errorf("user_created: %+v", c)
	}
	for _, it := range items {
		b, _ := json.Marshal(it)
		if strings.Contains(string(b), "password") {
			t.Errorf("entry leaks password: %s", b)
		}
	}
}

func TestEventsOpenCloseStayOutOfAuditTable(t *testing.T) {
	e, sink := newAuditEnv(t)
	r := httptest.NewRequest("GET", "/x", nil)
	e.rt.audit(r, "events_open")
	e.rt.audit(r, "events_close")
	e.rt.audit(r, "demo.reset")
	if got := strings.Join(actions(t, sink), ","); got != "demo.reset" {
		t.Fatalf("got %s", got)
	}
}

func TestGetAuditAdminOnlyWithLimit(t *testing.T) {
	e, _ := newAuditEnv(t)
	e.addUser(t, "root", "password1", auth.Admin)
	e.addUser(t, "ann", "password1", auth.Operator)
	admin := e.login(t, "root", "password1")
	op := e.login(t, "ann", "password1")

	if rec := e.do(req{method: "GET", path: "/api/audit"}); rec.Code != 401 {
		t.Errorf("anonymous: %d", rec.Code)
	}
	if rec := e.do(req{method: "GET", path: "/api/audit", cookie: op}); rec.Code != 403 {
		t.Errorf("operator: %d", rec.Code)
	}
	rec := e.do(req{method: "GET", path: "/api/audit?limit=1", cookie: admin})
	var out struct {
		Items []audit.Entry `json:"items"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || len(out.Items) != 1 {
		t.Fatalf("limit=1: %d %s", rec.Code, rec.Body)
	}
	if out.Items[0].Action != "login_ok" || out.Items[0].Actor != "user:ann" {
		t.Errorf("newest first: %+v", out.Items[0])
	}
	for _, bad := range []string{"0", "-1", "x", "1001"} {
		if rec := e.do(req{method: "GET", path: "/api/audit?limit=" + bad, cookie: admin}); rec.Code != 400 {
			t.Errorf("limit=%s: %d", bad, rec.Code)
		}
	}
	if rec := e.do(req{method: "GET", path: "/api/audit", cookie: admin}); rec.Code != 200 {
		t.Errorf("default limit: %d", rec.Code)
	}
}

func TestPasswordChange(t *testing.T) {
	e, sink := newAuditEnv(t)
	e.addUser(t, "ann", "password1", auth.Operator)
	cur := e.login(t, "ann", "password1")
	other := e.login(t, "ann", "password1")
	body := func(o, n string) string { return `{"old":"` + o + `","new":"` + n + `"}` }

	cases := []struct {
		name string
		r    req
		want int
	}{
		{"no session", req{method: "POST", path: "/api/auth/password", body: body("password1", "password22")}, 401},
		{"short new", req{method: "POST", path: "/api/auth/password", cookie: cur, body: body("password1", "short")}, 400},
		{"same", req{method: "POST", path: "/api/auth/password", cookie: cur, body: body("password1", "password1")}, 400},
		{"empty old", req{method: "POST", path: "/api/auth/password", cookie: cur, body: body("", "password22")}, 400},
		{"unknown field", req{method: "POST", path: "/api/auth/password", cookie: cur, body: `{"old":"a","new":"b","x":1}`}, 400},
		{"wrong old", req{method: "POST", path: "/api/auth/password", cookie: cur, body: body("wrongwrong", "password22")}, 401},
	}
	for _, c := range cases {
		if rec := e.do(c.r); rec.Code != c.want {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
	if rec := e.do(req{method: "POST", path: "/api/auth/password", cookie: cur, body: body("password1", "password22")}); rec.Code != 204 {
		t.Fatalf("change: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(req{method: "GET", path: "/api/auth/me", cookie: cur}); rec.Code != 200 {
		t.Errorf("current session after change: %d", rec.Code)
	}
	if rec := e.do(req{method: "GET", path: "/api/auth/me", cookie: other}); rec.Code != 401 {
		t.Errorf("other session after change: %d", rec.Code)
	}
	if rec := e.do(req{method: "POST", path: "/api/auth/login", body: `{"login":"ann","password":"password1"}`}); rec.Code != 401 {
		t.Errorf("old password: %d", rec.Code)
	}
	e.login(t, "ann", "password22")

	got := strings.Join(actions(t, sink), ",")
	if !strings.Contains(got, "password_change_failed") || !strings.HasSuffix(got, "password_changed,login_failed,login_ok") {
		t.Errorf("audit: %s", got)
	}
}

func TestPasswordChangeRateLimit(t *testing.T) {
	e, _ := newAuditEnv(t)
	e.addUser(t, "ann", "password1", auth.Operator)
	cur := e.login(t, "ann", "password1")
	for i := 0; i < 5; i++ {
		if rec := e.do(req{method: "POST", path: "/api/auth/password", cookie: cur, body: `{"old":"wrongwrong","new":"password22"}`}); rec.Code != 401 {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	rec := e.do(req{method: "POST", path: "/api/auth/password", cookie: cur, body: `{"old":"password1","new":"password22"}`})
	if rec.Code != 429 || decodeErr(t, rec).Error.Code != CodeTooManyTries {
		t.Fatalf("6th: %d %s", rec.Code, rec.Body)
	}
}

func TestPasswordChangeNotForAnonymousOrKey(t *testing.T) {
	e := newAuthEnv(t, false)
	if rec := e.do(req{method: "POST", path: "/api/auth/password", body: `{"old":"password1","new":"password22"}`}); rec.Code != 403 {
		t.Errorf("anonymous: %d %s", rec.Code, rec.Body)
	}
	_, key, err := e.svc.CreateAPIKey(context.Background(), "k", auth.Operator)
	if err != nil {
		t.Fatal(err)
	}
	if rec := e.do(req{method: "POST", path: "/api/auth/password", bearer: key, body: `{"old":"password1","new":"password22"}`}); rec.Code != 403 {
		t.Errorf("api key: %d", rec.Code)
	}
}

func TestLoginTooManyAttemptsIs429AndAudited(t *testing.T) {
	e, sink := newAuditEnv(t)
	e.addUser(t, "ann", "password1", auth.Operator)
	for i := 0; i < 5; i++ {
		e.do(req{method: "POST", path: "/api/auth/login", body: `{"login":"ann","password":"x"}`})
	}
	rec := e.do(req{method: "POST", path: "/api/auth/login", body: `{"login":"ann","password":"password1"}`})
	if rec.Code != 429 {
		t.Fatalf("6th: %d", rec.Code)
	}
	if got := actions(t, sink); got[len(got)-1] != "login_blocked" {
		t.Errorf("audit: %v", got)
	}
}
