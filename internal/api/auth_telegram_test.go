package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"umirhack/internal/auth"
	"umirhack/internal/auth/authtest"
)

const tgToken = "123456:TEST-bot-token-secret"

type memLinks struct {
	mu    sync.Mutex
	repo  *authtest.Mem
	byID  map[int64]string
	users map[string]bool
}

func (m *memLinks) TelegramLogin(_ context.Context, id int64) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l, ok := m.byID[id]; ok {
		return l, nil
	}
	return "", auth.ErrNotFound
}

func (m *memLinks) LinkTelegram(_ context.Context, login string, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.users[login] {
		return auth.ErrNotFound
	}
	if o, ok := m.byID[id]; ok && o != login {
		return auth.ErrExists
	}
	for k, v := range m.byID {
		if v == login {
			delete(m.byID, k)
		}
	}
	m.byID[id] = login
	return nil
}

func (m *memLinks) UnlinkTelegram(_ context.Context, login string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range m.byID {
		if v == login {
			delete(m.byID, k)
			return nil
		}
	}
	return auth.ErrNotFound
}

type tgEnv struct {
	h     http.Handler
	svc   *auth.Service
	repo  *authtest.Mem
	links *memLinks
	log   *bytes.Buffer
}

func newTgEnv(t *testing.T, required bool) *tgEnv {
	t.Helper()
	e := &tgEnv{repo: authtest.New(), log: &bytes.Buffer{}}
	e.svc = auth.New(e.repo, auth.Options{Required: required, SessionTTL: 2 * time.Hour,
		SecureCookie: true, Cost: bcrypt.MinCost, Now: func() time.Time { return standTime }})
	e.links = &memLinks{repo: e.repo, byID: map[int64]string{}, users: map[string]bool{}}
	for login, role := range map[string]auth.Role{"waiter": auth.Operator, "boss": auth.Admin} {
		if _, err := e.svc.CreateUser(context.Background(), login, "password1", role); err != nil {
			t.Fatal(err)
		}
		e.links.users[login] = true
	}
	log := slog.New(slog.NewJSONHandler(e.log, nil))
	e.h = NewRouter(Deps{Log: log, DB: fakeDB{}, Now: func() time.Time { return standTime },
		Auth: e.svc, Telegram: e.links, TelegramBotToken: tgToken}).Handler()
	return e
}

// signInitData собирает initData, подписанный токеном по правилам Telegram.
func signInitData(token string, userID int64, authDate time.Time) string {
	v := url.Values{}
	v.Set("auth_date", strconv.FormatInt(authDate.Unix(), 10))
	v.Set("query_id", "AAH-test")
	v.Set("user", `{"id":`+strconv.FormatInt(userID, 10)+`,"first_name":"Ivan"}`)
	dcs := "auth_date=" + v.Get("auth_date") + "\nquery_id=" + v.Get("query_id") + "\nuser=" + v.Get("user")
	secret := hmacSHA256([]byte("WebAppData"), []byte(token))
	v.Set("hash", hex.EncodeToString(hmacSHA256(secret, []byte(dcs))))
	return v.Encode()
}

func (e *tgEnv) do(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	return rec
}

func (e *tgEnv) tgLogin(initData string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]string{"init_data": initData})
	return e.do("POST", "/api/auth/telegram", string(b), nil)
}

func sessionOf(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	return nil
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) (string, map[string]any) {
	t.Helper()
	var b errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return b.Error.Code, b.Error.Details
}

func TestTelegramLoginValidSignature(t *testing.T) {
	e := newTgEnv(t, true)
	e.links.byID[777] = "waiter"
	rec := e.tgLogin(signInitData(tgToken, 777, standTime.Add(-time.Hour)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var me map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me["login"] != "waiter" || me["role"] != "operator" || me["auth_required"] != true {
		t.Fatalf("ответ как у /me: %v", me)
	}
	c := sessionOf(rec)
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie: %+v", c)
	}
	// Сессия рабочая: /api/auth/me отвечает тем же пользователем.
	rec = e.do("GET", "/api/auth/me", "", c)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"waiter"`) {
		t.Fatalf("me: %d %s", rec.Code, rec.Body)
	}
}

func TestTelegramLoginForgedSignature(t *testing.T) {
	e := newTgEnv(t, true)
	e.links.byID[777] = "boss"
	cases := map[string]string{
		"чужой токен":        signInitData("999:other-token", 777, standTime),
		"подмена user":       strings.Replace(signInitData(tgToken, 5, standTime), url.QueryEscape(`{"id":5`), url.QueryEscape(`{"id":777`), 1),
		"нет hash":           "auth_date=" + strconv.FormatInt(standTime.Unix(), 10) + "&user=%7B%22id%22%3A777%7D",
		"hash не hex":        "auth_date=1&user=%7B%22id%22%3A777%7D&hash=zz",
		"hash короткий":      "auth_date=1&user=%7B%22id%22%3A777%7D&hash=abcd",
		"мусор":              "%zz",
		"подмена auth_date":  strings.Replace(signInitData(tgToken, 777, standTime.Add(-48*time.Hour)), strconv.FormatInt(standTime.Add(-48*time.Hour).Unix(), 10), strconv.FormatInt(standTime.Unix(), 10), 1),
		"auth_date будущее":  signInitData(tgToken, 777, standTime.Add(time.Hour)),
		"user без числового": signInitDataRaw(tgToken, `{"id":"x"}`, standTime),
	}
	for name, d := range cases {
		rec := e.tgLogin(d)
		if code, _ := errCode(t, rec); rec.Code != 401 || code != CodeUnauthorized {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
		if sessionOf(rec) != nil {
			t.Errorf("%s: выдана сессия", name)
		}
	}
	if e.repo.SessionCount() != 0 {
		t.Fatal("сессий быть не должно")
	}
}

func signInitDataRaw(token, user string, at time.Time) string {
	ad := strconv.FormatInt(at.Unix(), 10)
	secret := hmacSHA256([]byte("WebAppData"), []byte(token))
	h := hex.EncodeToString(hmacSHA256(secret, []byte("auth_date="+ad+"\nuser="+user)))
	v := url.Values{"auth_date": {ad}, "user": {user}, "hash": {h}}
	return v.Encode()
}

func TestTelegramLoginExpiredAuthDate(t *testing.T) {
	e := newTgEnv(t, true)
	e.links.byID[777] = "waiter"
	rec := e.tgLogin(signInitData(tgToken, 777, standTime.Add(-24*time.Hour-time.Minute)))
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), "expired") {
		t.Fatalf("просрочено: %d %s", rec.Code, rec.Body)
	}
	if rec := e.tgLogin(signInitData(tgToken, 777, standTime.Add(-24*time.Hour+time.Minute))); rec.Code != 200 {
		t.Fatalf("почти 24 ч ещё можно: %d %s", rec.Code, rec.Body)
	}
}

// На стенде с ускоренными часами Deps.Now уходит вперёд, а Telegram ставит реальное время.
func TestTelegramLoginIgnoresStandClock(t *testing.T) {
	e := newTgEnv(t, true)
	e.links.byID[777] = "waiter"
	e.h = NewRouter(Deps{Log: slog.New(slog.NewJSONHandler(e.log, nil)), DB: fakeDB{},
		Now:  func() time.Time { return standTime.Add(48 * time.Hour) },
		Auth: e.svc, Telegram: e.links, TelegramBotToken: tgToken}).Handler()
	if rec := e.tgLogin(signInitData(tgToken, 777, standTime)); rec.Code != 200 {
		t.Fatalf("вход при сдвинутых часах стенда: %d %s", rec.Code, rec.Body)
	}
}

func TestTelegramLoginNotLinked(t *testing.T) {
	e := newTgEnv(t, true)
	rec := e.tgLogin(signInitData(tgToken, 4242, standTime))
	code, det := errCode(t, rec)
	if rec.Code != 403 || code != "telegram_not_linked" || det["telegram_user_id"] != float64(4242) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if sessionOf(rec) != nil {
		t.Fatal("без привязки сессии нет")
	}
}

func TestTelegramLoginReplayWithinWindow(t *testing.T) {
	// Подпись не одноразовая (как у самого Telegram): повтор тех же данных в пределах 24 ч
	// входит снова и выдаёт отдельную сессию; защита от кражи — срок auth_date.
	e := newTgEnv(t, true)
	e.links.byID[777] = "waiter"
	d := signInitData(tgToken, 777, standTime.Add(-time.Minute))
	a, b := e.tgLogin(d), e.tgLogin(d)
	if a.Code != 200 || b.Code != 200 {
		t.Fatalf("%d %d", a.Code, b.Code)
	}
	if sessionOf(a).Value == sessionOf(b).Value || e.repo.SessionCount() != 2 {
		t.Fatal("ожидались две разные сессии")
	}
}

func TestTelegramLoginDemoGuest(t *testing.T) {
	e := newTgEnv(t, false)
	rec := e.tgLogin(signInitData(tgToken, 31337, standTime)) // привязки нет — всё равно гость
	var me map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if rec.Code != 200 || me["login"] != nil || me["role"] != "operator" || me["auth_required"] != false {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if sessionOf(rec) != nil {
		t.Fatal("гостю сессия не нужна")
	}
	// Подпись проверяется и на демо-стенде.
	if rec := e.tgLogin(signInitData("1:evil", 1, standTime)); rec.Code != 401 {
		t.Fatalf("подделка на демо: %d", rec.Code)
	}
	if rec := e.tgLogin(signInitData(tgToken, 1, standTime.Add(-25*time.Hour))); rec.Code != 401 {
		t.Fatalf("просрочка на демо: %d", rec.Code)
	}
}

func TestTelegramLoginBadRequest(t *testing.T) {
	e := newTgEnv(t, true)
	for _, body := range []string{`{}`, `{"init_data":""}`, `{"init_data":1}`, `{"x":1}`, `nope`} {
		if rec := e.do("POST", "/api/auth/telegram", body, nil); rec.Code != 400 {
			t.Errorf("%s: %d", body, rec.Code)
		}
	}
}

func TestTelegramSecretsNotLogged(t *testing.T) {
	e := newTgEnv(t, true)
	e.links.byID[777] = "waiter"
	d := signInitData(tgToken, 777, standTime)
	e.tgLogin(d)
	e.tgLogin(signInitData("1:evil", 777, standTime))
	hash, _ := url.ParseQuery(d)
	out := e.log.String()
	if strings.Contains(out, tgToken) || strings.Contains(out, "TEST-bot-token") || strings.Contains(out, hash.Get("hash")) || strings.Contains(out, "init_data") {
		t.Fatalf("секрет в журнале:\n%s", out)
	}
	if !strings.Contains(out, "telegram_login_ok") || !strings.Contains(out, "telegram_login_failed") {
		t.Fatalf("нет записей аудита:\n%s", out)
	}
}

func TestTelegramLinkAdminRoutes(t *testing.T) {
	e := newTgEnv(t, true)
	adminTok, _, _ := loginCookie(t, e, "boss")
	waiterTok, _, _ := loginCookie(t, e, "waiter")

	if rec := e.do("PUT", "/api/users/waiter/telegram", `{"telegram_user_id":777}`, waiterTok); rec.Code != 403 {
		t.Fatalf("не admin: %d", rec.Code)
	}
	if rec := e.do("PUT", "/api/users/waiter/telegram", `{"telegram_user_id":777}`, nil); rec.Code != 401 {
		t.Fatalf("аноним: %d", rec.Code)
	}
	if rec := e.do("PUT", "/api/users/waiter/telegram", `{"telegram_user_id":0}`, adminTok); rec.Code != 400 {
		t.Fatalf("id 0: %d", rec.Code)
	}
	if rec := e.do("PUT", "/api/users/ghost/telegram", `{"telegram_user_id":1}`, adminTok); rec.Code != 404 {
		t.Fatalf("нет пользователя: %d", rec.Code)
	}
	if rec := e.do("PUT", "/api/users/waiter/telegram", `{"telegram_user_id":777}`, adminTok); rec.Code != 200 {
		t.Fatalf("link: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do("PUT", "/api/users/boss/telegram", `{"telegram_user_id":777}`, adminTok); rec.Code != 409 {
		t.Fatalf("чужой аккаунт: %d", rec.Code)
	}
	if rec := e.tgLogin(signInitData(tgToken, 777, standTime)); rec.Code != 200 {
		t.Fatalf("вход после привязки: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do("DELETE", "/api/users/waiter/telegram", "", adminTok); rec.Code != 204 {
		t.Fatalf("unlink: %d", rec.Code)
	}
	if rec := e.do("DELETE", "/api/users/waiter/telegram", "", adminTok); rec.Code != 404 {
		t.Fatalf("повторный unlink: %d", rec.Code)
	}
	if rec := e.tgLogin(signInitData(tgToken, 777, standTime)); rec.Code != 403 {
		t.Fatalf("вход после отвязки: %d", rec.Code)
	}
}

func loginCookie(t *testing.T, e *tgEnv, login string) (*http.Cookie, string, string) {
	t.Helper()
	rec := e.do("POST", "/api/auth/login", `{"login":"`+login+`","password":"password1"}`, nil)
	if rec.Code != 200 {
		t.Fatalf("login %s: %d %s", login, rec.Code, rec.Body)
	}
	return sessionOf(rec), "", ""
}

func TestTelegramRoutesNeedToken(t *testing.T) {
	rt := NewRouter(Deps{Log: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), DB: fakeDB{},
		Now: func() time.Time { return standTime }, Auth: auth.New(authtest.New(), auth.Options{}), Telegram: &memLinks{}})
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/auth/telegram", strings.NewReader(`{"init_data":"x"}`))
	r.Header.Set("Content-Type", "application/json")
	rt.Handler().ServeHTTP(rec, r)
	if rec.Code != 404 && rec.Code != 405 {
		t.Fatalf("без токена маршрута быть не должно: %d", rec.Code)
	}
}

// Контрольный вектор: подпись из документации считается тем же алгоритмом, что и в Telegram.
func TestVerifyInitDataMatchesSpec(t *testing.T) {
	m := hmac.New(sha256.New, []byte("WebAppData"))
	m.Write([]byte(tgToken))
	secret := m.Sum(nil)
	m2 := hmac.New(sha256.New, secret)
	m2.Write([]byte("auth_date=1000\nuser={\"id\":9}"))
	q := url.Values{"auth_date": {"1000"}, "user": {`{"id":9}`}, "hash": {hex.EncodeToString(m2.Sum(nil))}}
	id, err := verifyInitData(q.Encode(), tgToken, time.Unix(1000, 0))
	if err != nil || id != 9 {
		t.Fatalf("%d %v", id, err)
	}
}
