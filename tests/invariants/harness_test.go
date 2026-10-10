package invariants

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Общая обвязка сторожей с пометкой «БД». Сторож не знает устройства реализации:
// он собирает и запускает cmd/api, создаёт для себя отдельную базу и проверяет
// только то, что зафиксировано контрактами: HTTP API (docs/api.md) и схему 7.1.

const demoToken = "invariants-token"

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

func adminURL(t testing.TB) string {
	t.Helper()
	for _, k := range []string{"INVARIANTS_DATABASE_URL", "DATABASE_URL"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	t.Skip("сторож требует настоящий PostgreSQL: задайте INVARIANTS_DATABASE_URL (или DATABASE_URL)")
	return ""
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func buildAPI(t testing.TB) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "invariants-bin")
		if err != nil {
			buildErr = err
			return
		}
		binPath = filepath.Join(dir, "api")
		cmd := exec.Command("go", "build", "-o", binPath, "./cmd/api")
		cmd.Dir = repoRoot()
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build ./cmd/api: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return binPath
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

// stand — запущенный экземпляр сервиса на отдельной базе.
type stand struct {
	t    testing.TB
	base string
	pool *pgxpool.Pool
	log  *lockedBuf
	cli  *http.Client
}

type standOpts struct {
	TZ string // RESTAURANT_TZ
	// Dishes — справочник теста: блюдо → продукт → норма. Он готовится до рабочего запуска
	// сервиса: .ai/solution.md (2.3) разрешает реализации читать справочник один раз при старте.
	Dishes map[string]map[string]int64
}

func newStand(t testing.TB, o standOpts) *stand {
	t.Helper()
	admin := adminURL(t)
	bin := buildAPI(t)
	ctx := context.Background()

	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	name := fmt.Sprintf("inv_%x", rnd)
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
	tz := o.TZ
	if tz == "" {
		tz = "Europe/Moscow"
	}
	s := &stand{t: t, base: fmt.Sprintf("http://127.0.0.1:%d", port), log: logb,
		cli: &http.Client{Timeout: 30 * time.Second}}
	spawn := func(extra ...string) *exec.Cmd {
		cmd := exec.Command(bin)
		cmd.Stdout, cmd.Stderr = logb, logb
		cmd.Env = append(append(os.Environ(),
			"DATABASE_URL="+dbURL, "PORT="+fmt.Sprint(port), "AUTH_REQUIRED=false",
			"RESTAURANT_TZ="+tz, "DB_POOL_SIZE=40", "LOG_LEVEL=debug"), extra...)
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

	// Запуск 1: чистый экземпляр без сида — только чтобы применились миграции.
	first := spawn("DEMO_MODE=false", "CLOCK_MODE=real", "SEED_ON_EMPTY=false")
	stop(first)
	s.pool, err = pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	// Справочник теста: у каждого продукта из рецептов справочная цена > 0 (проверка старта, 2.2).
	for d, recipe := range o.Dishes {
		s.exec("INSERT INTO dishes (id, name) VALUES ($1, $1)", d)
		for p, q := range recipe {
			s.exec("INSERT INTO products (id, name, unit, price_minor_per_1000) VALUES ($1, $1, 'g', 100) ON CONFLICT DO NOTHING", p)
			s.exec("INSERT INTO recipe_items (dish_id, product_id, quantity) VALUES ($1, $2, $3)", d, p, q)
		}
	}
	if len(o.Dishes) == 0 {
		t.Fatal("сторожу нужно хотя бы одно блюдо в справочнике: на пустом справочнике сервис загрузил бы сид")
	}
	// Запуск 2: рабочий. Справочник непуст, сид повторно не загружается.
	cmd := spawn("DEMO_MODE=true", "DEMO_TOKEN="+demoToken, "CLOCK_MODE=simulated", "SEED_ON_EMPTY=true")
	t.Cleanup(func() {
		stop(cmd)
		s.pool.Close()
		if c, err := pgx.Connect(ctx, admin); err == nil {
			_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			_ = c.Close(ctx)
		}
		if t.Failed() {
			t.Logf("журнал сервиса:\n%s", logb.String())
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
	if demo {
		req.Header.Set("Authorization", "Bearer "+demoToken)
	}
	resp, err := s.cli.Do(req)
	if err != nil {
		return 0, []byte(err.Error())
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (s *stand) do(method, path string, body any) (int, []byte) {
	return s.raw(method, path, body, false)
}

func (s *stand) exec(q string, args ...any) {
	s.t.Helper()
	if _, err := s.pool.Exec(context.Background(), q, args...); err != nil {
		s.t.Fatalf("sql: %v\n%s", err, q)
	}
}

func (s *stand) queryInt(q string, args ...any) int64 {
	s.t.Helper()
	var n int64
	if err := s.pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		s.t.Fatalf("sql: %v\n%s", err, q)
	}
	return n
}

// now — время стенда из GET /api/clock.
func (s *stand) now() time.Time {
	s.t.Helper()
	st, b := s.do("GET", "/api/clock", nil)
	var c struct {
		Now time.Time `json:"now"`
	}
	if st != 200 || json.Unmarshal(b, &c) != nil {
		s.t.Fatalf("GET /api/clock: %d %s", st, b)
	}
	return c.Now
}

func (s *stand) demo(path string, body any) {
	s.t.Helper()
	if st, b := s.raw("POST", path, body, true); st/100 != 2 {
		s.t.Fatalf("POST %s: %d %s", path, st, b)
	}
}

func (s *stand) pause()               { s.demo("/api/demo/clock/speed", map[string]any{"speed": 0}) }
func (s *stand) speed(v float64)      { s.demo("/api/demo/clock/speed", map[string]any{"speed": v}) }
func (s *stand) jumpHours(h float64)  { s.demo("/api/demo/clock/jump", map[string]any{"hours": h}) }
func (s *stand) jump(d time.Duration) { s.jumpHours(d.Hours()) }

var deliverySeq int

// deliver принимает поставку одной позиции в граммах; возвращает статус и тело.
func (s *stand) deliver(product string, grams int64, shelfHours int) (int, []byte) {
	deliverySeq++
	return s.do("POST", "/api/deliveries", map[string]any{
		"id": fmt.Sprintf("D-%d-%d", time.Now().UnixNano(), deliverySeq), "number": "1",
		"created_at": time.Now().Format(time.RFC3339),
		"items": []map[string]any{{"product_id": product, "name": product, "unit": "g",
			"quantity": fmt.Sprint(grams), "shelf_life_hours": shelfHours}},
	})
}

func (s *stand) mustDeliver(product string, grams int64, shelfHours int) {
	s.t.Helper()
	if st, b := s.deliver(product, grams, shelfHours); st != 201 {
		s.t.Fatalf("поставка %s: %d %s", product, st, b)
	}
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type orderLine struct {
	Dish     string
	Portions int
}

func orderBody(id string, lines ...orderLine) map[string]any {
	items := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		items = append(items, map[string]any{"dish_id": l.Dish, "portions": l.Portions})
	}
	return map[string]any{"id": id, "items": items}
}

func (s *stand) order(id string, lines ...orderLine) (int, []byte) {
	return s.do("POST", "/api/orders", orderBody(id, lines...))
}

func errCode(b []byte) string {
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(b, &e)
	return e.Error.Code
}

// batchesSnapshot — побайтный снимок партий.
func (s *stand) batchesSnapshot() string {
	s.t.Helper()
	var out string
	err := s.pool.QueryRow(context.Background(),
		`SELECT COALESCE(json_agg(b ORDER BY id)::text, '[]') FROM batches b`).Scan(&out)
	if err != nil {
		s.t.Fatal(err)
	}
	return out
}

// checkConservation — инвариант 1 по журналу движений: quantity − sale − writeoff = remaining ≥ 0.
func (s *stand) checkConservation(ctx string) {
	s.t.Helper()
	rows, err := s.pool.Query(context.Background(), `
		SELECT b.id, b.quantity, b.remaining,
		       COALESCE(SUM(m.quantity) FILTER (WHERE m.kind IN ('sale','writeoff')), 0)
		FROM batches b LEFT JOIN movements m ON m.batch_id = b.id
		GROUP BY b.id ORDER BY b.id`)
	if err != nil {
		s.t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, q, rem, out int64
		if err := rows.Scan(&id, &q, &rem, &out); err != nil {
			s.t.Fatal(err)
		}
		if rem < 0 || q-out != rem {
			s.t.Fatalf("%s: партия %d: поступило %d − выбыло %d ≠ remaining %d", ctx, id, q, out, rem)
		}
	}
}

// availableByProduct — эталон: Σ remaining непросроченных партий (now < expires_at).
func (s *stand) availableByProduct(now time.Time) map[string]int64 {
	s.t.Helper()
	rows, err := s.pool.Query(context.Background(),
		`SELECT product_id, SUM(remaining) FROM batches WHERE expires_at > $1 AND remaining > 0 GROUP BY product_id`, now)
	if err != nil {
		s.t.Fatal(err)
	}
	defer rows.Close()
	m := map[string]int64{}
	for rows.Next() {
		var p string
		var n int64
		if err := rows.Scan(&p, &n); err != nil {
			s.t.Fatal(err)
		}
		m[p] = n
	}
	return m
}

// referenceStopList — независимая эталонная функция стоп-листа по партиям и рецептам из БД.
func (s *stand) referenceStopList(now time.Time) map[string]bool {
	s.t.Helper()
	avail := s.availableByProduct(now)
	rows, err := s.pool.Query(context.Background(), `SELECT dish_id, product_id, quantity FROM recipe_items`)
	if err != nil {
		s.t.Fatal(err)
	}
	defer rows.Close()
	stopped := map[string]bool{}
	for rows.Next() {
		var d, p string
		var q int64
		if err := rows.Scan(&d, &p, &q); err != nil {
			s.t.Fatal(err)
		}
		if avail[p] < q {
			stopped[d] = true
		}
	}
	return stopped
}

func (s *stand) apiStopList() map[string]bool {
	s.t.Helper()
	st, b := s.do("GET", "/api/stop-list", nil)
	var list []struct {
		ID        string `json:"id"`
		Available bool   `json:"available"`
	}
	if st != 200 || json.Unmarshal(b, &list) != nil {
		s.t.Fatalf("GET /api/stop-list: %d %s", st, b)
	}
	m := map[string]bool{}
	for _, d := range list {
		if d.Available {
			s.t.Fatalf("в стоп-листе блюдо %s с available=true", d.ID)
		}
		m[d.ID] = true
	}
	return m
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func keys(m map[string]bool) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return "[" + strings.Join(ks, " ") + "]"
}
