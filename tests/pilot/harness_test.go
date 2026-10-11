package pilot

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"umirhack/internal/iiko"
)

// Обвязка прогона: собирает cmd/api и cmd/iiko-adapter, создаёт отдельную базу и поднимает
// экземпляр на ней. Знает только контракты: HTTP API (docs/api.md), схему и командную строку адаптера.

const demoToken = "pilot-token"

func adminURL(t testing.TB) string {
	t.Helper()
	for _, k := range []string{"TEST_DATABASE_URL", "INVARIANTS_DATABASE_URL", "DATABASE_URL"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	t.Skip("прогон требует настоящий PostgreSQL: задайте TEST_DATABASE_URL")
	return ""
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func goBuild(t testing.TB, out string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", append([]string{"build", "-o", out}, args...)...)
	cmd.Dir = repoRoot()
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %v: %v\n%s", args, err, b)
	}
}

// simClockSrc — единственное отличие тестовой сборки адаптера от боевой: «сейчас» адаптера берётся
// из файла, куда прогон кладёт время стенда. Адаптер читает time.Now(), а прогон идёт на ускоренных
// часах стенда; без подмены 30 суток пришлось бы ждать 30 суток. Логика синхронизации, сверки и
// outbox не затронута; исходники репозитория не меняются (подмена — через go build -overlay).
const simClockSrc = `package main

import (
	"os"
	"strings"
	"time"
)

func simNow() time.Time {
	b, err := os.ReadFile(os.Getenv("PILOT_NOW_FILE"))
	if err != nil {
		panic(err)
	}
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b)))
	if err != nil {
		panic(err)
	}
	return t
}
`

// buildAdapter собирает cmd/iiko-adapter с подменой часов.
func buildAdapter(t testing.TB, dir string) string {
	t.Helper()
	root := repoRoot()
	mainGo := filepath.Join(root, "cmd", "iiko-adapter", "main.go")
	src, err := os.ReadFile(mainGo)
	if err != nil {
		t.Fatal(err)
	}
	const from = "now:       time.Now,"
	if !strings.Contains(string(src), from) {
		t.Fatalf("в %s не найдена строка %q: подмена часов адаптера в прогоне устарела", mainGo, from)
	}
	patched := filepath.Join(dir, "main_patched.go")
	clockGo := filepath.Join(dir, "simclock.go")
	if err := os.WriteFile(patched, []byte(strings.Replace(string(src), from, "now:       simNow,", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clockGo, []byte(simClockSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(dir, "overlay.json")
	ov, _ := json.Marshal(map[string]any{"Replace": map[string]string{
		mainGo: patched,
		filepath.Join(root, "cmd", "iiko-adapter", "simclock.go"): clockGo,
	}})
	if err := os.WriteFile(overlay, ov, 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "iiko-adapter")
	goBuild(t, bin, "-overlay", overlay, "./cmd/iiko-adapter")
	return bin
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// since возвращает журнал с позиции off и новую позицию.
func (l *lockedBuf) since(off int) (string, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.b.String()
	return s[off:], len(s)
}

// stand — запущенный экземпляр cmd/api на своей базе.
type stand struct {
	t       testing.TB
	base    string
	pool    *pgxpool.Pool
	log     *lockedBuf
	cli     *http.Client
	logOff  int
	key     string // ключ API роли operator (как у адаптера)
	reqs    int    // запросов с момента создания
	status5 []string
}

type startOpts struct {
	// Phase1 вызывается на чистом экземпляре (реальные часы, без сида), когда миграции применены:
	// здесь загружается справочник.
	Phase1 func(s *stand)
	Env    []string
}

var status5xx = regexp.MustCompile(`"status":5\d\d`)

func newStand(t testing.TB, o startOpts) *stand {
	t.Helper()
	admin := adminURL(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "api")
	goBuild(t, bin, "./cmd/api")
	ctx := context.Background()

	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	name := fmt.Sprintf("pilot_%x", rnd)
	ac, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("подключение к PostgreSQL: %v", err)
	}
	if _, err := ac.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	_ = ac.Close(ctx)
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	dbURL := u.String()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	logb := &lockedBuf{}
	s := &stand{t: t, base: fmt.Sprintf("http://127.0.0.1:%d", port), log: logb,
		cli: &http.Client{Timeout: 60 * time.Second}}
	spawn := func(extra ...string) *exec.Cmd {
		cmd := exec.Command(bin)
		cmd.Stdout, cmd.Stderr = logb, logb
		cmd.Env = append(append(os.Environ(),
			"DATABASE_URL="+dbURL, "PORT="+fmt.Sprint(port), "AUTH_REQUIRED=true", "ADMIN_LOGIN=pilot-admin", "ADMIN_PASSWORD=pilot-admin-password-1",
			"RESTAURANT_TZ=Europe/Moscow", "DB_POOL_SIZE=20", "LOG_LEVEL=info"), extra...)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			if st, _ := s.raw("GET", "/healthz", nil, false); st == 200 {
				return cmd
			}
			if time.Now().After(deadline) {
				_ = cmd.Process.Kill()
				t.Fatalf("сервис не стал готов за 30 с\n%s", logb.String())
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	stop := func(cmd *exec.Cmd) {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	s.pool, err = pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	// Запуск 1: чистый экземпляр, реальные часы, без сида. Сюда загружается справочник.
	first := spawn("DEMO_MODE=false", "CLOCK_MODE=real", "SEED_ON_EMPTY=false")
	s.issueKey()
	if o.Phase1 != nil {
		o.Phase1(s)
	}
	stop(first)
	// Запуск 2: рабочий, ускоренные часы под ручным управлением. Справочник непуст — сид не грузится.
	cmd := spawn(append([]string{"DEMO_MODE=true", "DEMO_TOKEN=" + demoToken, "CLOCK_MODE=simulated", "SEED_ON_EMPTY=true"}, o.Env...)...)
	t.Cleanup(func() {
		stop(cmd)
		s.pool.Close()
		if c, err := pgx.Connect(ctx, admin); err == nil {
			_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			_ = c.Close(ctx)
		}
		if t.Failed() {
			l := logb.String()
			if len(l) > 20000 {
				l = l[len(l)-20000:]
			}
			t.Logf("журнал сервиса (хвост):\n%s", l)
		}
	})
	return s
}

func (s *stand) raw(method, path string, body any, demo bool) (int, []byte) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, s.base+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch {
	case demo:
		req.Header.Set("Authorization", "Bearer "+demoToken)
	case s.key != "":
		req.Header.Set("Authorization", "Bearer "+s.key)
	}
	resp, err := s.cli.Do(req)
	if err != nil {
		return 0, []byte(err.Error())
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	s.reqs++
	if resp.StatusCode >= 500 {
		s.status5 = append(s.status5, fmt.Sprintf("%s %s → %d", method, path, resp.StatusCode))
	}
	return resp.StatusCode, b
}

func (s *stand) do(method, path string, body any) (int, []byte) {
	return s.raw(method, path, body, false)
}

// get разбирает JSON ответа 200 в v.
func (s *stand) get(path string, v any) {
	s.t.Helper()
	st, b := s.do("GET", path, nil)
	if st != 200 {
		s.t.Fatalf("GET %s: %d %s", path, st, b)
	}
	if err := json.Unmarshal(b, v); err != nil {
		s.t.Fatalf("GET %s: %v\n%s", path, err, b)
	}
}

// serverErrors — записи «request … status=5xx» в журнале сервиса с прошлого вызова: сюда попадают
// и запросы адаптера, которых клиент прогона не видит.
func (s *stand) serverErrors() []string {
	var chunk string
	chunk, s.logOff = s.log.since(s.logOff)
	var out []string
	for _, ln := range strings.Split(chunk, "\n") {
		if strings.Contains(ln, `"msg":"request"`) && status5xx.MatchString(ln) || strings.Contains(ln, `"msg":"panic"`) {
			out = append(out, ln)
		}
	}
	return out
}

func (s *stand) queryRows(q string, args ...any) pgx.Rows {
	s.t.Helper()
	rows, err := s.pool.Query(context.Background(), q, args...)
	if err != nil {
		s.t.Fatalf("sql: %v\n%s", err, q)
	}
	return rows
}

func (s *stand) queryInt(q string, args ...any) int64 {
	s.t.Helper()
	var n int64
	if err := s.pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		s.t.Fatalf("sql: %v\n%s", err, q)
	}
	return n
}

func (s *stand) now() time.Time {
	s.t.Helper()
	var c struct {
		Now time.Time `json:"now"`
	}
	s.get("/api/clock", &c)
	return c.Now
}

func (s *stand) demo(path string, body any) {
	s.t.Helper()
	if st, b := s.raw("POST", path, body, true); st/100 != 2 {
		s.t.Fatalf("POST %s: %d %s", path, st, b)
	}
}

// jumpTo переводит часы стенда на момент at (вперёд, не более 720 ч за раз).
func (s *stand) jumpTo(at time.Time) {
	s.t.Helper()
	d := at.Sub(s.now())
	if d <= 0 {
		s.t.Fatalf("часы идут только вперёд: %s уже не впереди %s", at, s.now())
	}
	s.demo("/api/demo/clock/jump", map[string]any{"hours": d.Hours()})
}

// issueKey входит под администратором (он создаётся при старте из ADMIN_LOGIN/ADMIN_PASSWORD) и
// выпускает ключ API роли operator: такой же ключ получает адаптер (импорт каталога доступен
// только администратору или ключу).
func (s *stand) issueKey() {
	s.t.Helper()
	b, _ := json.Marshal(map[string]string{"login": "pilot-admin", "password": "pilot-admin-password-1"})
	resp, err := s.cli.Post(s.base+"/api/auth/login", "application/json", bytes.NewReader(b))
	if err != nil {
		s.t.Fatal(err)
	}
	resp.Body.Close()
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "session" {
			cookie = c
		}
	}
	if resp.StatusCode != 200 || cookie == nil {
		s.t.Fatalf("вход администратора: %d", resp.StatusCode)
	}
	kb, _ := json.Marshal(map[string]string{"name": "pilot-adapter", "role": "operator"})
	req, _ := http.NewRequest("POST", s.base+"/api/api-keys", bytes.NewReader(kb))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	r2, err := s.cli.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer r2.Body.Close()
	body, _ := io.ReadAll(r2.Body)
	var out struct {
		Key string `json:"key"`
	}
	if r2.StatusCode != 201 || json.Unmarshal(body, &out) != nil || out.Key == "" {
		s.t.Fatalf("POST /api/api-keys: %d %s", r2.StatusCode, body)
	}
	s.key = out.Key
}

// pastOnly ставит перед фейковым iiko прокси, который не отдаёт накладные «из будущего».
// Фейк выдаёт накладные за дату целиком (в том числе 08:00 при запросе в 00:30), настоящий iiko
// знает только о проведённых документах; без фильтра адаптер получил бы поставку раньше её прихода.
// now() — время стенда, которое прогон кладёт в файл перед каждым запуском адаптера.
func pastOnly(target string, now func() time.Time, loc *time.Location) http.Handler {
	u, err := url.Parse(target)
	if err != nil {
		panic(err)
	}
	rp := httputil.NewSingleHostReverseProxy(u)
	rp.ModifyResponse = func(resp *http.Response) error {
		if resp.Request.URL.Path != iiko.PathIncomingInvoice || resp.StatusCode != http.StatusOK {
			return nil
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		var invs iiko.IncomingInvoices
		if err := xml.Unmarshal(raw, &invs); err != nil {
			return err
		}
		kept := invs.Documents[:0:0]
		for _, d := range invs.Documents {
			at, err := time.ParseInLocation(iiko.DateTimeLayout, d.DateIncoming, loc)
			if err != nil {
				return err
			}
			if !at.After(now()) {
				kept = append(kept, d)
			}
		}
		invs.Documents = kept
		out, err := xml.MarshalIndent(invs, "", "  ")
		if err != nil {
			return err
		}
		out = append([]byte(xml.Header), out...)
		resp.Body = io.NopCloser(bytes.NewReader(out))
		resp.ContentLength = int64(len(out))
		resp.Header.Set("Content-Length", fmt.Sprint(len(out)))
		return nil
	}
	return rp
}
