package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"umirhack/internal/iiko"
	"umirhack/internal/iiko/fake"
	"umirhack/internal/iiko/mapping"
)

var fixedNow = func() time.Time { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC) }

func fakeServer(t *testing.T) *httptest.Server {
	t.Helper()
	loc, _ := time.LoadLocation("Europe/Moscow")
	s, err := fake.New(fake.Options{Seed: 1, Origin: time.Date(2026, 1, 1, 0, 0, 0, 0, loc), Login: "demo", Password: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(s.Handler())
}

func TestAgainstFake(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()
	var out, errw bytes.Buffer
	code := run(context.Background(), []string{"-url", srv.URL, "-login", "demo", "-password", "demo"}, &out, &errw, fixedNow)
	if code != 0 {
		t.Fatalf("код %d\n%s\n%s", code, out.String(), errw.String())
	}
	for _, want := range []string{"продуктов 12, блюд 8", "техкарты: всего 8", "единицы измерения: все распознаны", "logout: выполнен", "ИТОГ: подключение работает"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("нет %q в отчёте:\n%s", want, out.String())
		}
	}
}

func TestBadPassword(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()
	var out, errw bytes.Buffer
	if code := run(context.Background(), []string{"-url", srv.URL, "-login", "demo", "-password", "wrong"}, &out, &errw, fixedNow); code != 1 {
		t.Fatalf("код %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "ИТОГ: сбой") {
		t.Errorf("нет итога:\n%s", out.String())
	}
}

func TestUsage(t *testing.T) {
	var out, errw bytes.Buffer
	t.Setenv("IIKO_URL", "")
	if code := run(context.Background(), []string{"-login", "x"}, &out, &errw, fixedNow); code != 2 {
		t.Fatalf("код %d", code)
	}
}

func TestLogoutOnce(t *testing.T) {
	var logouts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/auth"):
			_, _ = w.Write([]byte("tok"))
		case strings.HasSuffix(r.URL.Path, "/api/logout"):
			logouts.Add(1)
		case strings.HasSuffix(r.URL.Path, "/incomingInvoice"):
			_, _ = w.Write([]byte("<incomingInvoiceDtoes/>"))
		case strings.HasSuffix(r.URL.Path, "/olap"):
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()
	var out, errw bytes.Buffer
	run(context.Background(), []string{"-url", srv.URL, "-login", "a", "-password", "b"}, &out, &errw, fixedNow)
	if logouts.Load() != 1 {
		t.Fatalf("logout вызван %d раз", logouts.Load())
	}
}

type stub struct{}

func (stub) Products(context.Context) ([]iiko.Product, error) {
	return []iiko.Product{
		{ID: "a", Name: "Мука", Type: "GOODS", MainUnit: "кг", ShelfLifeHours: 10},
		{ID: "b", Name: "Соль", Type: "GOODS", MainUnit: "упак"},
		{ID: "c", Name: "", Type: "GOODS", MainUnit: "л"},
		{ID: "d", Name: "Блин", Type: "DISH", Price: 100},
		{ID: "e", Name: "Удалён", Type: "GOODS", MainUnit: "кг", Deleted: true},
	}, nil
}
func (stub) Charts(context.Context) ([]iiko.AssemblyChart, error) {
	return []iiko.AssemblyChart{{ID: "x", AssembledProductID: "d", DateFrom: "2026-01-01"}}, nil
}
func (stub) Invoices(context.Context, time.Time, time.Time) ([]iiko.IncomingInvoice, error) {
	return nil, nil
}
func (stub) Sales(context.Context, time.Time, time.Time) ([]mapping.Sale, error) {
	return []mapping.Sale{{DishID: "d", Date: "2026-10-10", Portions: 0}}, nil
}
func (stub) Balance(context.Context, time.Time) ([]iiko.BalanceItem, error) {
	return nil, context.DeadlineExceeded
}

func TestCollectCounts(t *testing.T) {
	r := collect(context.Background(), stub{}, fixedNow())
	if r.Goods != 3 || r.Dishes != 1 || r.Deleted != 1 {
		t.Errorf("счётчики: %+v", r)
	}
	if r.UnknownUnits["упак"] != 1 || len(r.UnknownUnits) != 1 {
		t.Errorf("единицы: %v", r.UnknownUnits)
	}
	if r.NoShelfLife != 2 {
		t.Errorf("без срока: %d", r.NoShelfLife)
	}
	if r.Missing["products.name"] != 1 || r.Missing["assemblyCharts.items"] != 1 || r.Missing["olap.DishAmountInt"] != 1 {
		t.Errorf("поля: %v", r.Missing)
	}
	if _, ok := r.Failed["остатки"]; !ok || len(r.Errors) != 1 {
		t.Errorf("ошибки: %v %v", r.Failed, r.Errors)
	}
	var out bytes.Buffer
	r.print(&out)
	if !strings.Contains(out.String(), `"упак"`) || !strings.Contains(out.String(), "остатки: ОШИБКА") {
		t.Errorf("отчёт:\n%s", out.String())
	}
}
