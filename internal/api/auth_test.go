package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"umirhack/internal/auth"
	"umirhack/internal/auth/authtest"
)

type authEnv struct {
	rt   *Router
	h    http.Handler
	svc  *auth.Service
	repo *authtest.Mem
	log  *bytes.Buffer
	now  *time.Time
}

func newAuthEnv(t *testing.T, required bool) *authEnv {
	t.Helper()
	e := &authEnv{repo: authtest.New(), log: &bytes.Buffer{}}
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	e.now = &now
	e.svc = auth.New(e.repo, auth.Options{Required: required, SessionTTL: 2 * time.Hour,
		SecureCookie: true, Cost: bcrypt.MinCost, Now: func() time.Time { return *e.now }})
	log := slog.New(slog.NewJSONHandler(e.log, nil))
	e.rt = NewRouter(Deps{Log: log, DB: fakeDB{}, Now: func() time.Time { return standTime }, Auth: e.svc})
	// Маршруты будущих тикетов: чтение и изменение с ролями по умолчанию, плюс /events.
	e.rt.Handle(http.MethodGet, "/api/products", func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, 200, map[string]string{"ok": "1"}) })
	e.rt.Handle(http.MethodGet, "/events", func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, 200, map[string]string{"ok": "1"}) })
	e.rt.Handle(http.MethodPost, "/api/orders", func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, 201, map[string]string{"ok": "1"}) })
	e.rt.Handle(http.MethodPost, "/api/deliveries", func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, 201, map[string]string{"ok": "1"}) })
	e.h = e.rt.Handler()
	return e
}

func (e *authEnv) addUser(t *testing.T, login, pw string, role auth.Role) {
	t.Helper()
	if _, err := e.svc.CreateUser(context.Background(), login, pw, role); err != nil {
		t.Fatal(err)
	}
}

type req struct {
	method, path, body string
	cookie             string
	bearer             string
	noCT               bool
}

func (e *authEnv) do(r req) *httptest.ResponseRecorder {
	hr := httptest.NewRequest(r.method, r.path, strings.NewReader(r.body))
	if !r.noCT {
		hr.Header.Set("Content-Type", "application/json")
	}
	if r.cookie != "" {
		hr.AddCookie(&http.Cookie{Name: "session", Value: r.cookie})
	}
	if r.bearer != "" {
		hr.Header.Set("Authorization", "Bearer "+r.bearer)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, hr)
	return rec
}

// login возвращает значение cookie session.
func (e *authEnv) login(t *testing.T, login, pw string) string {
	t.Helper()
	rec := e.do(req{method: "POST", path: "/api/auth/login", body: `{"login":"` + login + `","password":"` + pw + `"}`})
	if rec.Code != 200 {
		t.Fatalf("login %s: %d %s", login, rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" {
			return c.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}

func TestAuthRequiredTrueGives401(t *testing.T) {
	e := newAuthEnv(t, true)
	for _, p := range []string{"/api/products", "/events", "/api/auth/me", "/api/users", "/api/api-keys"} {
		rec := e.do(req{method: "GET", path: p})
		if rec.Code != 401 || decodeErr(t, rec).Error.Code != CodeUnauthorized {
			t.Errorf("GET %s: %d %s", p, rec.Code, rec.Body)
		}
	}
	if rec := e.do(req{method: "POST", path: "/api/orders", body: `{}`}); rec.Code != 401 {
		t.Errorf("POST /api/orders: %d", rec.Code)
	}
	// Исключения: /healthz и login.
	if rec := e.do(req{method: "GET", path: "/healthz"}); rec.Code != 200 {
		t.Errorf("healthz: %d", rec.Code)
	}
	if rec := e.do(req{method: "POST", path: "/api/auth/login", body: `{"login":"x","password":"y"}`}); rec.Code != 401 {
		t.Errorf("login with bad data: %d", rec.Code)
	}
}

func TestAuthRequiredFalseAnonymousIsOperator(t *testing.T) {
	e := newAuthEnv(t, false)
	if rec := e.do(req{method: "GET", path: "/api/products"}); rec.Code != 200 {
		t.Errorf("read: %d", rec.Code)
	}
	if rec := e.do(req{method: "POST", path: "/api/orders", body: `{}`}); rec.Code != 201 {
		t.Errorf("operator write: %d", rec.Code)
	}
	if rec := e.do(req{method: "GET", path: "/api/users"}); rec.Code != 403 {
		t.Errorf("operator on /api/users: %d", rec.Code)
	}
	rec := e.do(req{method: "GET", path: "/api/auth/me"})
	var me map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if rec.Code != 200 || me["login"] != nil || me["role"] != "operator" || me["auth_required"] != false {
		t.Errorf("me: %d %s", rec.Code, rec.Body)
	}
	// Токен демо-ручек не является ключом и не должен ломать открытый стенд.
	if rec := e.do(req{method: "GET", path: "/api/products", bearer: "demo-token"}); rec.Code != 200 {
		t.Errorf("unknown bearer on open stand: %d", rec.Code)
	}
}

func TestLoginCookieAttributesAndMe(t *testing.T) {
	e := newAuthEnv(t, true)
	e.addUser(t, "ann", "password1", auth.Operator)
	rec := e.do(req{method: "POST", path: "/api/auth/login", body: `{"login":"ann","password":"password1"}`})
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"login":"ann","role":"operator"}` {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	var c *http.Cookie
	for _, x := range rec.Result().Cookies() {
		if x.Name == "session" {
			c = x
		}
	}
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie flags: %+v", c)
	}
	if want := e.now.Add(2 * time.Hour); !c.Expires.Equal(want) {
		t.Errorf("cookie expiry %v, want %v", c.Expires, want)
	}
	if len(c.Value) != 43 {
		t.Errorf("token length %d", len(c.Value))
	}

	rec = e.do(req{method: "GET", path: "/api/auth/me", cookie: c.Value})
	var me map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me["login"] != "ann" || me["role"] != "operator" || me["auth_required"] != true {
		t.Errorf("me: %s", rec.Body)
	}
}

func TestWrongLoginAndWrongPasswordLookTheSame(t *testing.T) {
	e := newAuthEnv(t, true)
	e.addUser(t, "ann", "password1", auth.Viewer)
	a := e.do(req{method: "POST", path: "/api/auth/login", body: `{"login":"ann","password":"nope"}`})
	b := e.do(req{method: "POST", path: "/api/auth/login", body: `{"login":"ghost","password":"nope"}`})
	if a.Code != 401 || b.Code != 401 || a.Body.String() != b.Body.String() {
		t.Fatalf("%d %s / %d %s", a.Code, a.Body, b.Code, b.Body)
	}
}

func TestLoginRateLimit429(t *testing.T) {
	e := newAuthEnv(t, true)
	e.addUser(t, "ann", "password1", auth.Viewer)
	for i := 1; i <= 5; i++ {
		if rec := e.do(req{method: "POST", path: "/api/auth/login", body: `{"login":"ann","password":"bad"}`}); rec.Code != 401 {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	rec := e.do(req{method: "POST", path: "/api/auth/login", body: `{"login":"ann","password":"password1"}`})
	if rec.Code != 429 || decodeErr(t, rec).Error.Code != CodeTooManyTries {
		t.Fatalf("6th attempt: %d %s", rec.Code, rec.Body)
	}
}

func TestRoles(t *testing.T) {
	e := newAuthEnv(t, true)
	e.addUser(t, "v", "password1", auth.Viewer)
	e.addUser(t, "o", "password1", auth.Operator)
	e.addUser(t, "a", "password1", auth.Admin)
	v, o, a := e.login(t, "v", "password1"), e.login(t, "o", "password1"), e.login(t, "a", "password1")

	cases := []struct {
		name         string
		cookie       string
		method, path string
		want         int
	}{
		{"viewer reads", v, "GET", "/api/products", 200},
		{"viewer events", v, "GET", "/events", 200},
		{"viewer orders", v, "POST", "/api/orders", 403},
		{"viewer deliveries", v, "POST", "/api/deliveries", 403},
		{"viewer users", v, "GET", "/api/users", 403},
		{"viewer keys", v, "GET", "/api/api-keys", 403},
		{"viewer logout", v, "POST", "/api/auth/logout", 204},
		{"operator orders", o, "POST", "/api/orders", 201},
		{"operator deliveries", o, "POST", "/api/deliveries", 201},
		{"operator users", o, "GET", "/api/users", 403},
		{"operator keys", o, "POST", "/api/api-keys", 403},
		{"admin users", a, "GET", "/api/users", 200},
		{"admin keys", a, "GET", "/api/api-keys", 200},
		{"admin orders", a, "POST", "/api/orders", 201},
	}
	for _, c := range cases {
		rec := e.do(req{method: c.method, path: c.path, cookie: c.cookie, body: `{}`})
		if rec.Code != c.want {
			t.Errorf("%s: got %d want %d (%s)", c.name, rec.Code, c.want, rec.Body)
		}
		if c.want == 403 && decodeErr(t, rec).Error.Code != CodeForbidden {
			t.Errorf("%s: code %s", c.name, rec.Body)
		}
	}
}

func TestMutatingRequestsNeedJSONContentType(t *testing.T) {
	e := newAuthEnv(t, false)
	rec := e.do(req{method: "POST", path: "/api/orders", body: `{}`, noCT: true})
	if rec.Code != 400 || decodeErr(t, rec).Error.Code != CodeValidation {
		t.Fatalf("no content type: %d %s", rec.Code, rec.Body)
	}
	hr := httptest.NewRequest("POST", "/api/orders", strings.NewReader(`{}`))
	hr.Header.Set("Content-Type", "text/plain")
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, hr)
	if rr.Code != 400 {
		t.Errorf("text/plain: %d", rr.Code)
	}
	hr = httptest.NewRequest("POST", "/api/orders", strings.NewReader(`{}`))
	hr.Header.Set("Content-Type", "application/json; charset=utf-8")
	rr = httptest.NewRecorder()
	e.h.ServeHTTP(rr, hr)
	if rr.Code != 201 {
		t.Errorf("json with charset: %d", rr.Code)
	}
	// Без входа 401 приходит раньше проверки типа содержимого.
	e2 := newAuthEnv(t, true)
	if rec := e2.do(req{method: "POST", path: "/api/orders", noCT: true}); rec.Code != 401 {
		t.Errorf("unauth + no CT: %d", rec.Code)
	}
}

func TestLogoutAndExpiredSession(t *testing.T) {
	e := newAuthEnv(t, true)
	e.addUser(t, "ann", "password1", auth.Viewer)
	tok := e.login(t, "ann", "password1")

	rec := e.do(req{method: "POST", path: "/api/auth/logout", cookie: tok})
	if rec.Code != 204 {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := e.do(req{method: "GET", path: "/api/products", cookie: tok}); rec.Code != 401 {
		t.Errorf("after logout: %d", rec.Code)
	}

	tok = e.login(t, "ann", "password1")
	*e.now = e.now.Add(2 * time.Hour)
	if rec := e.do(req{method: "GET", path: "/api/products", cookie: tok}); rec.Code != 401 {
		t.Errorf("expired session: %d", rec.Code)
	}
}

func TestAPIKeyLifecycle(t *testing.T) {
	e := newAuthEnv(t, true)
	e.addUser(t, "root", "password1", auth.Admin)
	adm := e.login(t, "root", "password1")

	rec := e.do(req{method: "POST", path: "/api/api-keys", cookie: adm, body: `{"name":"simulator","role":"operator"}`})
	if rec.Code != 201 {
		t.Fatalf("create key: %d %s", rec.Code, rec.Body)
	}
	var created struct {
		ID   int64  `json:"id"`
		Key  string `json:"key"`
		Role string `json:"role"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Key == "" || created.Role != "operator" {
		t.Fatalf("created: %s", rec.Body)
	}

	if rec := e.do(req{method: "POST", path: "/api/orders", bearer: created.Key, body: `{}`}); rec.Code != 201 {
		t.Errorf("operator key: %d", rec.Code)
	}
	if rec := e.do(req{method: "GET", path: "/api/users", bearer: created.Key}); rec.Code != 403 {
		t.Errorf("key on admin route: %d", rec.Code)
	}
	// Ключ показывается один раз: в списке его нет.
	rec = e.do(req{method: "GET", path: "/api/api-keys", cookie: adm})
	if strings.Contains(rec.Body.String(), created.Key) || strings.Contains(rec.Body.String(), `"key"`) {
		t.Errorf("list leaks key: %s", rec.Body)
	}

	if rec := e.do(req{method: "DELETE", path: "/api/api-keys/" + itoa(created.ID), cookie: adm}); rec.Code != 204 {
		t.Fatalf("revoke: %d", rec.Code)
	}
	if rec := e.do(req{method: "GET", path: "/api/products", bearer: created.Key}); rec.Code != 401 {
		t.Errorf("revoked key: %d", rec.Code)
	}

	// Роль ключа — только viewer или operator.
	if rec := e.do(req{method: "POST", path: "/api/api-keys", cookie: adm, body: `{"name":"x","role":"admin"}`}); rec.Code != 400 {
		t.Errorf("admin key: %d", rec.Code)
	}
	// Viewer-ключ не пишет.
	rec = e.do(req{method: "POST", path: "/api/api-keys", cookie: adm, body: `{"name":"ro","role":"viewer"}`})
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if rec := e.do(req{method: "POST", path: "/api/orders", bearer: created.Key, body: `{}`}); rec.Code != 403 {
		t.Errorf("viewer key write: %d", rec.Code)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestUsersAdminAPI(t *testing.T) {
	e := newAuthEnv(t, true)
	e.addUser(t, "root", "password1", auth.Admin)
	adm := e.login(t, "root", "password1")

	rec := e.do(req{method: "POST", path: "/api/users", cookie: adm, body: `{"login":"bob","password":"password2","role":"viewer"}`})
	if rec.Code != 201 || strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var u struct{ ID int64 }
	_ = json.Unmarshal(rec.Body.Bytes(), &u)

	if rec := e.do(req{method: "POST", path: "/api/users", cookie: adm, body: `{"login":"bob","password":"password2","role":"viewer"}`}); rec.Code != 409 {
		t.Errorf("duplicate: %d", rec.Code)
	}
	for _, body := range []string{
		`{"login":"","password":"password2","role":"viewer"}`,
		`{"login":"c","password":"short","role":"viewer"}`,
		`{"login":"c","password":"password2","role":"god"}`,
		`{"login":"c","password":"password2","role":"viewer","extra":1}`,
		`not json`,
	} {
		if rec := e.do(req{method: "POST", path: "/api/users", cookie: adm, body: body}); rec.Code != 400 {
			t.Errorf("%s: %d", body, rec.Code)
		}
	}

	rec = e.do(req{method: "GET", path: "/api/users", cookie: adm})
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "$2a$") {
		t.Errorf("list leaks hash: %s", rec.Body)
	}

	if rec := e.do(req{method: "DELETE", path: "/api/users/" + itoa(u.ID), cookie: adm}); rec.Code != 204 {
		t.Errorf("delete: %d", rec.Code)
	}
	if rec := e.do(req{method: "DELETE", path: "/api/users/" + itoa(u.ID), cookie: adm}); rec.Code != 404 {
		t.Errorf("delete again: %d", rec.Code)
	}
	if rec := e.do(req{method: "DELETE", path: "/api/users/abc", cookie: adm}); rec.Code != 400 {
		t.Errorf("bad id: %d", rec.Code)
	}
	// Последнего admin удалить нельзя.
	list, _ := e.repo.ListUsers(context.Background())
	rec = e.do(req{method: "DELETE", path: "/api/users/" + itoa(list[0].ID), cookie: adm})
	if rec.Code != 409 {
		t.Errorf("last admin: %d %s", rec.Code, rec.Body)
	}
}

func TestRequestLogHasRoleAndUserID(t *testing.T) {
	e := newAuthEnv(t, true)
	e.addUser(t, "ann", "password1", auth.Operator)
	tok := e.login(t, "ann", "password1")
	e.log.Reset()
	e.do(req{method: "GET", path: "/api/products", cookie: tok})
	m := lastLog(t, e.log)
	if m["msg"] != "request" || m["role"] != "operator" || m["user_id"] == nil || m["path"] != "/api/products" {
		t.Errorf("request log: %v", m)
	}

	// Ключ: роль и id ключа.
	e.addUser(t, "root", "password1", auth.Admin)
	adm := e.login(t, "root", "password1")
	rec := e.do(req{method: "POST", path: "/api/api-keys", cookie: adm, body: `{"name":"k","role":"viewer"}`})
	var k struct {
		ID  int64
		Key string
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &k)
	e.log.Reset()
	e.do(req{method: "GET", path: "/api/products", bearer: k.Key})
	m = lastLog(t, e.log)
	if m["role"] != "viewer" || m["api_key_id"] == nil || m["user_id"] != nil {
		t.Errorf("api key request log: %v", m)
	}
}

func TestAuditRecords(t *testing.T) {
	e := newAuthEnv(t, true)
	e.addUser(t, "root", "password1", auth.Admin)
	e.do(req{method: "POST", path: "/api/auth/login", body: `{"login":"root","password":"bad"}`})
	adm := e.login(t, "root", "password1")
	e.do(req{method: "POST", path: "/api/users", cookie: adm, body: `{"login":"bob","password":"password2","role":"viewer"}`})
	e.do(req{method: "POST", path: "/api/auth/logout", cookie: adm})

	events := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(e.log.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["audit"] == true {
			events[m["event"].(string)] = true
		}
	}
	for _, want := range []string{"login_failed", "login_ok", "user_created", "logout"} {
		if !events[want] {
			t.Errorf("missing audit event %q (have %v)", want, events)
		}
	}
}

// Пароль, токен сессии и ключ интеграции не попадают в журнал ни при каких путях.
func TestSecretsNeverReachTheLog(t *testing.T) {
	e := newAuthEnv(t, true)
	const adminPW, userPW, badPW = "adm1n-Secret-PW", "us3r-Secret-PW", "wr0ng-Secret-PW"
	e.addUser(t, "root", adminPW, auth.Admin)

	e.do(req{method: "POST", path: "/api/auth/login", body: `{"login":"root","password":"` + badPW + `"}`})
	adm := e.login(t, "root", adminPW)
	e.do(req{method: "POST", path: "/api/users", cookie: adm, body: `{"login":"bob","password":"` + userPW + `","role":"viewer"}`})
	rec := e.do(req{method: "POST", path: "/api/api-keys", cookie: adm, body: `{"name":"k","role":"operator"}`})
	var k struct{ Key string }
	_ = json.Unmarshal(rec.Body.Bytes(), &k)
	e.do(req{method: "GET", path: "/api/products", bearer: k.Key})
	e.do(req{method: "GET", path: "/api/products", bearer: "fsk_invalid-presented-key"})
	e.do(req{method: "GET", path: "/api/products", cookie: "invalid-presented-session"})
	for i := 0; i < 6; i++ { // и путь 429
		e.do(req{method: "POST", path: "/api/auth/login", body: `{"login":"bob","password":"` + badPW + `"}`})
	}
	e.do(req{method: "POST", path: "/api/auth/logout", cookie: adm})

	out := e.log.String()
	if out == "" {
		t.Fatal("log is empty")
	}
	for name, secret := range map[string]string{
		"admin password": adminPW, "user password": userPW, "wrong password": badPW,
		"session token": adm, "api key": k.Key,
		"presented bad key": "fsk_invalid-presented-key", "presented bad session": "invalid-presented-session",
		"session token hash": string(auth.HashToken(adm)),
	} {
		if strings.Contains(out, secret) {
			t.Errorf("%s leaked into the log", name)
		}
	}
	if strings.Contains(strings.ToLower(out), "bearer ") || strings.Contains(out, "$2a$") {
		t.Error("authorization header or password hash leaked into the log")
	}
}
