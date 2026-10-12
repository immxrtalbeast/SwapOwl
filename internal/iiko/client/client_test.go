package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fixtureServer отдаёт записанные ответы по путям с базовым путём base.
func fixtureServer(t *testing.T, base string, tls bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var logouts atomic.Int32
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, base)
		if !strings.HasPrefix(r.URL.Path, base) {
			http.NotFound(w, r)
			return
		}
		switch p {
		case "/api/auth":
			if r.URL.Query().Get("pass") != "89e495e7941cf9e40e6980d14a16bf023ccd4c91" { // sha1("demo")
				http.Error(w, "bad", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte("tok-1\n"))
			return
		case "/api/logout":
			logouts.Add(1)
			_, _ = w.Write([]byte("ok"))
			return
		}
		if r.URL.Query().Get("key") != "tok-1" {
			http.Error(w, "no key", http.StatusUnauthorized)
			return
		}
		switch p {
		case "/api/v2/entities/products/list":
			_, _ = w.Write(fixture(t, "products.json"))
		case "/api/v2/assemblyCharts/getAll":
			_, _ = w.Write(fixture(t, "charts.json"))
		case "/api/documents/export/incomingInvoice":
			_, _ = w.Write(fixture(t, "invoices.xml"))
		case "/api/v2/reports/olap":
			_, _ = w.Write(fixture(t, "olap.json"))
		case "/api/v2/reports/balance/stores":
			_, _ = w.Write(fixture(t, "balance.json"))
		default:
			http.NotFound(w, r)
		}
	})
	if tls {
		return httptest.NewTLSServer(h), &logouts
	}
	return httptest.NewServer(h), &logouts
}

func fast(o Options) Options {
	o.RetryPause = time.Millisecond
	return o
}

func TestFixtures(t *testing.T) {
	srv, logouts := fixtureServer(t, "/resto", false)
	defer srv.Close()
	c := NewWithOptions(srv.URL, "demo", "demo", nil, fast(Options{}))
	ctx := context.Background()
	ps, err := c.Products(ctx)
	if err != nil || len(ps) != 6 || ps[0].ShelfLifeHours != 4320 {
		t.Fatalf("products: %v %v", ps, err)
	}
	ch, err := c.Charts(ctx)
	if err != nil || len(ch) != 1 || len(ch[0].Items) != 2 || ch[0].DateTo != nil {
		t.Fatalf("charts: %v %v", ch, err)
	}
	inv, err := c.Invoices(ctx, time.Now(), time.Now())
	if err != nil || len(inv) != 1 || len(inv[0].Items) != 2 || inv[0].Items[0].Price != 48.5 {
		t.Fatalf("invoices: %v %v", inv, err)
	}
	sales, err := c.Sales(ctx, time.Now(), time.Now())
	if err != nil || len(sales) != 2 || sales[0].Portions != 42 {
		t.Fatalf("sales: %v %v", sales, err)
	}
	bal, err := c.Balance(ctx, time.Now())
	if err != nil || len(bal) != 2 || bal[0].Amount != 12.5 {
		t.Fatalf("balance: %v %v", bal, err)
	}
	if err := c.Logout(ctx); err != nil || logouts.Load() != 1 {
		t.Fatalf("logout: %v %d", err, logouts.Load())
	}
	if err := c.Logout(ctx); err != nil || logouts.Load() != 1 { // токена нет — запроса нет
		t.Fatalf("повторный logout: %v %d", err, logouts.Load())
	}
}

func TestBasePath(t *testing.T) {
	srv, _ := fixtureServer(t, "/custom/resto", false)
	defer srv.Close()
	for name, c := range map[string]*Client{
		"в адресе":  NewWithOptions(srv.URL+"/custom/resto/", "demo", "demo", nil, fast(Options{})),
		"в options": NewWithOptions(srv.URL, "demo", "demo", nil, fast(Options{BasePath: "custom/resto"})),
	} {
		if ps, err := c.Products(context.Background()); err != nil || len(ps) != 6 {
			t.Errorf("%s: %v %v", name, ps, err)
		}
	}
	// по умолчанию — /resto
	srv2, _ := fixtureServer(t, "/resto", false)
	defer srv2.Close()
	if _, err := New(srv2.URL, "demo", "demo", nil).Products(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRelogin401Once(t *testing.T) {
	var auths, calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/auth") {
			n := auths.Add(1)
			_, _ = w.Write([]byte("tok-" + string(rune('0'+n))))
			return
		}
		calls.Add(1)
		if r.URL.Query().Get("key") != "tok-2" { // первый токен «протух»
			http.Error(w, "expired", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write(fixture(t, "products.json"))
	}))
	defer srv.Close()
	c := NewWithOptions(srv.URL, "demo", "demo", nil, fast(Options{}))
	if ps, err := c.Products(context.Background()); err != nil || len(ps) != 6 {
		t.Fatalf("%v %v", ps, err)
	}
	if auths.Load() != 2 || calls.Load() != 2 {
		t.Fatalf("auth=%d calls=%d", auths.Load(), calls.Load())
	}
	// токен переиспользуется
	if _, err := c.Products(context.Background()); err != nil || auths.Load() != 2 {
		t.Fatalf("auth=%d err=%v", auths.Load(), err)
	}
}

func TestPersistent401NotLooping(t *testing.T) {
	var auths atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/auth") {
			auths.Add(1)
			_, _ = w.Write([]byte("t"))
			return
		}
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := NewWithOptions(srv.URL, "demo", "demo", nil, fast(Options{}))
	if _, err := c.Products(context.Background()); err == nil || auths.Load() != 2 {
		t.Fatalf("err=%v auths=%d", err, auths.Load())
	}
}

func TestRetry5xx(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/auth") {
			_, _ = w.Write([]byte("t"))
			return
		}
		if n.Add(1) <= 2 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		_, _ = w.Write(fixture(t, "balance.json"))
	}))
	defer srv.Close()
	c := NewWithOptions(srv.URL, "demo", "demo", nil, fast(Options{Retries: 2}))
	if b, err := c.Balance(context.Background(), time.Now()); err != nil || len(b) != 2 || n.Load() != 3 {
		t.Fatalf("%v %v n=%d", b, err, n.Load())
	}
	// без повторов — ошибка сразу
	n.Store(0)
	c = NewWithOptions(srv.URL, "demo", "demo", nil, fast(Options{Retries: -1}))
	if _, err := c.Balance(context.Background(), time.Now()); err == nil || n.Load() != 1 {
		t.Fatalf("err=%v n=%d", err, n.Load())
	}
}

func Test4xxNotRetried(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/auth") {
			_, _ = w.Write([]byte("t"))
			return
		}
		n.Add(1)
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	defer srv.Close()
	c := NewWithOptions(srv.URL, "demo", "demo", nil, fast(Options{}))
	if _, err := c.Products(context.Background()); err == nil || n.Load() != 1 {
		t.Fatalf("err=%v n=%d", err, n.Load())
	}
}

func TestRateLimit(t *testing.T) {
	srv, _ := fixtureServer(t, "/resto", false)
	defer srv.Close()
	c := NewWithOptions(srv.URL, "demo", "demo", nil, Options{MinInterval: time.Hour})
	var slept []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	if _, err := c.Products(context.Background()); err != nil { // auth + products
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] < 59*time.Minute {
		t.Fatalf("ожидалась пауза между auth и запросом: %v", slept)
	}
}

func TestTLS(t *testing.T) {
	srv, _ := fixtureServer(t, "/resto", true)
	defer srv.Close()
	strict := NewWithOptions(srv.URL, "demo", "demo", nil, fast(Options{Retries: -1}))
	if _, err := strict.Products(context.Background()); err == nil {
		t.Fatal("самоподписанный сертификат должен отвергаться")
	}
	loose := NewWithOptions(srv.URL, "demo", "demo", nil, fast(Options{Insecure: true}))
	if ps, err := loose.Products(context.Background()); err != nil || len(ps) != 6 {
		t.Fatalf("%v %v", ps, err)
	}
}

func TestTimeoutAndCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	c := NewWithOptions(srv.URL, "demo", "demo", nil, fast(Options{Timeout: 50 * time.Millisecond, Retries: -1}))
	if _, err := c.Products(context.Background()); err == nil {
		t.Fatal("ожидался таймаут")
	}
}
