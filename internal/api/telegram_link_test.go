package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

// TelegramLinked — для GET /api/auth/telegram-link (необязательное расширение TelegramLinks).
func (m *memLinks) TelegramLinked(_ context.Context, login string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.byID {
		if v == login {
			return true, nil
		}
	}
	return false, nil
}

// linkEnv — вход через Telegram с подвижными часами (код живёт 10 минут).
type linkEnv struct {
	h     http.Handler
	links *memLinks
	log   *bytes.Buffer
	now   time.Time
}

func newLinkEnv(t *testing.T, required bool) *linkEnv {
	t.Helper()
	e := &linkEnv{log: &bytes.Buffer{}, now: standTime}
	repo := authtest.New()
	svc := auth.New(repo, auth.Options{Required: required, SessionTTL: 2 * time.Hour, SecureCookie: true, Cost: bcrypt.MinCost,
		Now: func() time.Time { return e.now }})
	e.links = &memLinks{repo: repo, byID: map[int64]string{}, users: map[string]bool{}}
	for login, role := range map[string]auth.Role{"waiter": auth.Operator, "boss": auth.Admin, "guest": auth.Viewer} {
		if _, err := svc.CreateUser(context.Background(), login, "password1", role); err != nil {
			t.Fatal(err)
		}
		e.links.users[login] = true
	}
	e.h = NewRouter(Deps{Log: slog.New(slog.NewJSONHandler(e.log, nil)), DB: fakeDB{}, Now: func() time.Time { return standTime },
		Auth: svc, Telegram: e.links, TelegramBotToken: tgToken}).Handler()
	return e
}

func (e *linkEnv) do(method, path, body, remote string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if remote != "" {
		r.RemoteAddr = remote
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	return rec
}

func (e *linkEnv) webLogin(t *testing.T, login string) *http.Cookie {
	t.Helper()
	rec := e.do("POST", "/api/auth/login", `{"login":"`+login+`","password":"password1"}`, "", nil)
	c := sessionOf(rec)
	if rec.Code != 200 || c == nil {
		t.Fatalf("вход %s: %d %s", login, rec.Code, rec.Body)
	}
	return c
}

func (e *linkEnv) issue(t *testing.T, c *http.Cookie) string {
	t.Helper()
	rec := e.do("POST", "/api/auth/telegram-link-code", "", "", c)
	var out struct {
		Code      string `json:"code"`
		ExpiresIn int    `json:"expires_in_seconds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 200 || len(out.Code) != 9 || out.ExpiresIn != 600 {
		t.Fatalf("выдача кода: %d %s", rec.Code, rec.Body)
	}
	return out.Code
}

func (e *linkEnv) link(tgID int64, code, remote string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]string{"init_data": signInitData(tgToken, tgID, e.now), "code": code})
	return e.do("POST", "/api/auth/telegram-link", string(b), remote, nil)
}

func TestLinkByCodeHappyPathAndSingleUse(t *testing.T) {
	e := newLinkEnv(t, true)
	code := e.issue(t, e.webLogin(t, "waiter"))

	rec := e.link(555, code, "")
	if rec.Code != 200 {
		t.Fatalf("привязка: %d %s", rec.Code, rec.Body)
	}
	var me map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me["login"] != "waiter" || me["role"] != "operator" || me["auth_required"] != true {
		t.Fatalf("ответ: %v", me)
	}
	if e.links.byID[555] != "waiter" {
		t.Fatalf("привязка не записана: %v", e.links.byID)
	}
	// Сессия, выданная при привязке, рабочая и с теми же правами.
	if me := e.do("GET", "/api/auth/me", "", "", sessionOf(rec)); me.Code != 200 || !strings.Contains(me.Body.String(), `"waiter"`) {
		t.Fatalf("me: %d %s", me.Code, me.Body)
	}
	// Повторное использование — тот же ответ, что на неверный код.
	again := e.link(556, code, "")
	if c, _ := errCodeOf(t, again); again.Code != 400 || c != "invalid_link_code" {
		t.Fatalf("повтор: %d %s", again.Code, again.Body)
	}
	if _, ok := e.links.byID[556]; ok {
		t.Fatal("второй Telegram привязан по сгоревшему коду")
	}
}

func errCodeOf(t *testing.T, rec *httptest.ResponseRecorder) (string, map[string]any) {
	t.Helper()
	return errCode(t, rec)
}

// Регистр, пробелы и дефис при вводе не важны — код переписывают с экрана.
func TestLinkCodeInputIsForgiving(t *testing.T) {
	e := newLinkEnv(t, true)
	code := e.issue(t, e.webLogin(t, "boss"))
	typed := " " + strings.ToLower(strings.Replace(code, "-", " ", 1)) + " "
	if rec := e.link(900, typed, ""); rec.Code != 200 || e.links.byID[900] != "boss" {
		t.Fatalf("%q: %d %s", typed, rec.Code, rec.Body)
	}
}

func TestLinkCodeExpiresAfterTenMinutes(t *testing.T) {
	e := newLinkEnv(t, true)
	code := e.issue(t, e.webLogin(t, "waiter"))
	e.now = e.now.Add(linkCodeTTL - time.Second)
	probe := e.link(1, "ZZZZ-ZZZZ", "9.9.9.9:1") // чужая попытка не мешает
	_ = probe
	e.now = e.now.Add(2 * time.Second)
	rec := e.link(555, code, "")
	if c, _ := errCodeOf(t, rec); rec.Code != 400 || c != "invalid_link_code" {
		t.Fatalf("просроченный код: %d %s", rec.Code, rec.Body)
	}
	if len(e.links.byID) != 0 {
		t.Fatalf("привязка по просроченному коду: %v", e.links.byID)
	}
}

// Новый код отзывает прежний; код чужого пользователя привязывает именно его, а не того, кто ввёл.
func TestNewCodeRevokesOldAndBindsOwner(t *testing.T) {
	e := newLinkEnv(t, true)
	c := e.webLogin(t, "waiter")
	first := e.issue(t, c)
	second := e.issue(t, c)
	if first == second {
		t.Fatal("коды совпали")
	}
	if rec := e.link(1, first, ""); rec.Code != 400 {
		t.Fatalf("отозванный код: %d", rec.Code)
	}
	bossCode := e.issue(t, e.webLogin(t, "boss"))
	if rec := e.link(2, bossCode, ""); rec.Code != 200 || e.links.byID[2] != "boss" {
		t.Fatalf("код boss: %d %v", rec.Code, e.links.byID)
	}
	if rec := e.link(3, second, ""); rec.Code != 200 || e.links.byID[3] != "waiter" {
		t.Fatalf("код waiter: %d %v", rec.Code, e.links.byID)
	}
}

// Перебор: после пяти неверных кодов с адреса — 429, даже на верный код; другой адрес не затронут.
func TestLinkBruteForceIsLimited(t *testing.T) {
	e := newLinkEnv(t, true)
	code := e.issue(t, e.webLogin(t, "waiter"))
	for i := 0; i < linkFailMax; i++ {
		if rec := e.link(int64(10+i), "ABCD-EFGH", "7.7.7.7:1"); rec.Code != 400 {
			t.Fatalf("попытка %d: %d", i, rec.Code)
		}
	}
	rec := e.link(77, code, "7.7.7.7:1")
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("после лимита: %d %v", rec.Code, rec.Header())
	}
	if len(e.links.byID) != 0 {
		t.Fatalf("привязка при лимите: %v", e.links.byID)
	}
	// Верный код с другого адреса работает: лимит по адресу, а не глобальный.
	if rec := e.link(77, code, "8.8.8.8:1"); rec.Code != 200 {
		t.Fatalf("другой адрес: %d %s", rec.Code, rec.Body)
	}
	// Через окно лимит снимается.
	e.now = e.now.Add(linkFailWindow + time.Second)
	if rec := e.link(78, "ABCD-EFGH", "7.7.7.7:1"); rec.Code != 400 {
		t.Fatalf("после окна: %d", rec.Code)
	}
}

// Подделанная подпись — 401, код не сгорает и попытка не считается перебором.
func TestForgedSignatureDoesNotBurnCode(t *testing.T) {
	e := newLinkEnv(t, true)
	code := e.issue(t, e.webLogin(t, "waiter"))
	forged := signInitData("999999:other-token", 555, e.now)
	b, _ := json.Marshal(map[string]string{"init_data": forged, "code": code})
	for i := 0; i < linkFailMax+2; i++ {
		if rec := e.do("POST", "/api/auth/telegram-link", string(b), "6.6.6.6:1", nil); rec.Code != 401 {
			t.Fatalf("подделка %d: %d", i, rec.Code)
		}
	}
	if rec := e.link(555, code, "6.6.6.6:1"); rec.Code != 200 {
		t.Fatalf("код сгорел или адрес заблокирован: %d %s", rec.Code, rec.Body)
	}
}

func TestLinkStatusAndSelfUnlink(t *testing.T) {
	e := newLinkEnv(t, true)
	c := e.webLogin(t, "waiter")
	linked := func() bool {
		rec := e.do("GET", "/api/auth/telegram-link", "", "", c)
		var out struct{ Linked bool }
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if rec.Code != 200 {
			t.Fatalf("статус: %d %s", rec.Code, rec.Body)
		}
		return out.Linked
	}
	if linked() {
		t.Fatal("привязан до привязки")
	}
	if rec := e.link(555, e.issue(t, c), ""); rec.Code != 200 {
		t.Fatalf("%d", rec.Code)
	}
	if !linked() {
		t.Fatal("не привязан после привязки")
	}
	if rec := e.do("DELETE", "/api/auth/telegram-link", "", "", c); rec.Code != 204 {
		t.Fatalf("отвязка: %d", rec.Code)
	}
	if rec := e.do("DELETE", "/api/auth/telegram-link", "", "", c); rec.Code != 204 {
		t.Fatalf("повтор отвязки: %d", rec.Code)
	}
	if linked() {
		t.Fatal("привязан после отвязки")
	}
}

// Код выдаётся только вошедшему в веб; без входа — 401.
func TestLinkCodeNeedsWebSession(t *testing.T) {
	e := newLinkEnv(t, true)
	if rec := e.do("POST", "/api/auth/telegram-link-code", "", "", nil); rec.Code != 401 {
		t.Fatalf("без входа: %d", rec.Code)
	}
	if rec := e.do("GET", "/api/auth/telegram-link", "", "", nil); rec.Code != 401 {
		t.Fatalf("статус без входа: %d", rec.Code)
	}
}

// Занятый Telegram-аккаунт — 409, а код при этом остаётся годным для другого аккаунта.
func TestLinkConflictKeepsCode(t *testing.T) {
	e := newLinkEnv(t, true)
	e.links.byID[555] = "boss"
	code := e.issue(t, e.webLogin(t, "waiter"))
	if rec := e.link(555, code, ""); rec.Code != 409 {
		t.Fatalf("занятый аккаунт: %d %s", rec.Code, rec.Body)
	}
	if rec := e.link(556, code, ""); rec.Code != 200 || e.links.byID[556] != "waiter" {
		t.Fatalf("после конфликта: %d %v", rec.Code, e.links.byID)
	}
}

// Без включённого входа привязывать некуда.
func TestLinkNeedsAuthRequired(t *testing.T) {
	e := newLinkEnv(t, false)
	b, _ := json.Marshal(map[string]string{"init_data": signInitData(tgToken, 5, e.now), "code": "ABCD-EFGH"})
	if rec := e.do("POST", "/api/auth/telegram-link", string(b), "", nil); rec.Code != 409 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

// В журнале нет ни кода, ни init_data.
func TestLinkNeverLogsCodeOrInitData(t *testing.T) {
	e := newLinkEnv(t, true)
	code := e.issue(t, e.webLogin(t, "waiter"))
	initData := signInitData(tgToken, 555, e.now)
	b, _ := json.Marshal(map[string]string{"init_data": initData, "code": code})
	if rec := e.do("POST", "/api/auth/telegram-link", string(b), "", nil); rec.Code != 200 {
		t.Fatalf("%d", rec.Code)
	}
	logs := e.log.String()
	raw := strings.ReplaceAll(code, "-", "")
	for _, secret := range []string{code, raw, initData, tgToken} {
		if strings.Contains(logs, secret) {
			t.Fatalf("секрет %q в журнале: %s", secret, logs)
		}
	}
	_ = io.Discard
}
