// Command iiko-check — проверка подключения к iikoServer, только чтение (docs/onboarding.md, п. 3а).
// Логинится, читает номенклатуру, техкарты, накладные и продажи за 7 дней, остатки на сейчас,
// печатает сводку и делает logout. Код выхода: 0 — всё прочитано, 1 — сбой, 2 — неверные флаги.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"umirhack/internal/iiko"
	"umirhack/internal/iiko/client"
	"umirhack/internal/iiko/mapping"
)

const days = 7

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, time.Now))
}

func run(ctx context.Context, args []string, out, errw io.Writer, now func() time.Time) int {
	fs := flag.NewFlagSet("iiko-check", flag.ContinueOnError)
	fs.SetOutput(errw)
	env := func(k string) string { return os.Getenv(k) }
	url := fs.String("url", env("IIKO_URL"), "адрес iiko, например https://host:443/resto (или IIKO_URL)")
	login := fs.String("login", env("IIKO_LOGIN"), "логин (или IIKO_LOGIN)")
	pass := fs.String("password", env("IIKO_PASSWORD"), "пароль (или IIKO_PASSWORD)")
	basePath := fs.String("base-path", client.DefaultBasePath, "базовый путь API, если его нет в -url")
	insecure := fs.Bool("insecure", false, "не проверять TLS-сертификат (самоподписанный)")
	timeout := fs.Duration("timeout", 60*time.Second, "таймаут одного запроса")
	rate := fs.Duration("rate", 0, "минимальная пауза между запросами")
	tz := fs.String("tz", "Europe/Moscow", "часовой пояс ресторана")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *url == "" || *login == "" || *pass == "" {
		fmt.Fprintln(errw, "iiko-check: нужны -url, -login, -password (или IIKO_URL, IIKO_LOGIN, IIKO_PASSWORD)")
		return 2
	}
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		fmt.Fprintln(errw, "iiko-check: -tz:", err)
		return 2
	}
	c := client.NewWithOptions(*url, *login, *pass, nil, client.Options{
		BasePath: *basePath, Timeout: *timeout, Insecure: *insecure, MinInterval: *rate,
	})
	r := collect(ctx, c, now().In(loc))
	r.print(out)
	if err := c.Logout(ctx); err != nil {
		fmt.Fprintln(out, "logout: ошибка:", err)
		r.Errors = append(r.Errors, "logout")
	} else {
		fmt.Fprintln(out, "logout: выполнен")
	}
	if len(r.Errors) > 0 {
		fmt.Fprintf(out, "ИТОГ: сбой (%s)\n", strings.Join(r.Errors, ", "))
		return 1
	}
	fmt.Fprintln(out, "ИТОГ: подключение работает")
	return 0
}

// source — то, откуда читаются данные (в тестах подменяется).
type source interface {
	Products(context.Context) ([]iiko.Product, error)
	Charts(context.Context) ([]iiko.AssemblyChart, error)
	Invoices(ctx context.Context, from, to time.Time) ([]iiko.IncomingInvoice, error)
	Sales(ctx context.Context, from, to time.Time) ([]mapping.Sale, error)
	Balance(context.Context, time.Time) ([]iiko.BalanceItem, error)
}

// Report — сводка проверки.
type Report struct {
	Goods, Dishes, Deleted, OtherTypes int
	UnknownUnits                       map[string]int // единица → число продуктов
	NoShelfLife                        int            // продуктов (GOODS) без срока хранения
	Charts, ActiveCharts               int
	Invoices, ProcessedInvoices        int
	InvoiceItems                       int
	SalesRows                          int
	SalesPortions                      int64
	BalanceRows                        int
	// Missing — поле → сколько записей без него (пустое значение = поля нет или не разобралось).
	Missing map[string]int
	// Failed — разделы, которые не прочитались: раздел → ошибка.
	Failed map[string]string
	Errors []string
	From   time.Time
	To     time.Time
}

func collect(ctx context.Context, s source, now time.Time) *Report {
	r := &Report{UnknownUnits: map[string]int{}, Missing: map[string]int{}, Failed: map[string]string{}}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	r.From, r.To = today.AddDate(0, 0, -days), today
	fail := func(section string, err error) {
		r.Failed[section] = err.Error()
		r.Errors = append(r.Errors, section)
	}
	miss := func(field string, empty bool) {
		if empty {
			r.Missing[field]++
		}
	}

	if ps, err := s.Products(ctx); err != nil {
		fail("номенклатура", err)
	} else {
		for _, p := range ps {
			miss("products.id", p.ID == "")
			miss("products.name", p.Name == "")
			miss("products.type", p.Type == "")
			if p.Deleted {
				r.Deleted++
				continue
			}
			switch p.Type {
			case "GOODS":
				r.Goods++
				miss("products.mainUnit", p.MainUnit == "")
				if _, err := mapping.UnitOf(p.MainUnit); err != nil && p.MainUnit != "" {
					r.UnknownUnits[p.MainUnit]++
				}
				if p.ShelfLifeHours <= 0 {
					r.NoShelfLife++
				}
			case "DISH":
				r.Dishes++
				miss("products.defaultSalePrice (блюдо)", p.Price <= 0)
			default:
				r.OtherTypes++
			}
		}
	}
	if cs, err := s.Charts(ctx); err != nil {
		fail("техкарты", err)
	} else {
		for _, c := range cs {
			r.Charts++
			miss("assemblyCharts.assembledProductId", c.AssembledProductID == "")
			miss("assemblyCharts.items", len(c.Items) == 0)
			miss("assemblyCharts.dateFrom", c.DateFrom == "")
			if c.DateTo == nil {
				r.ActiveCharts++
			}
			for _, it := range c.Items {
				miss("assemblyCharts.items.productId", it.ProductID == "")
				miss("assemblyCharts.items.amountIn", it.AmountIn <= 0)
			}
		}
	}
	if invs, err := s.Invoices(ctx, r.From, r.To); err != nil {
		fail("накладные", err)
	} else {
		for _, inv := range invs {
			r.Invoices++
			miss("incomingInvoice.id", inv.ID == "")
			miss("incomingInvoice.documentNumber", inv.DocumentNumber == "")
			miss("incomingInvoice.dateIncoming", inv.DateIncoming == "")
			miss("incomingInvoice.status", inv.Status == "")
			miss("incomingInvoice.items", len(inv.Items) == 0)
			if inv.Status == "PROCESSED" {
				r.ProcessedInvoices++
			}
			for _, it := range inv.Items {
				r.InvoiceItems++
				miss("incomingInvoice.items.product", it.Product == "")
				miss("incomingInvoice.items.amount", it.Amount <= 0)
				miss("incomingInvoice.items.price", it.Price <= 0)
			}
		}
	}
	if sales, err := s.Sales(ctx, r.From, r.To); err != nil {
		fail("продажи", err)
	} else {
		for _, sl := range sales {
			r.SalesRows++
			r.SalesPortions += sl.Portions
			miss("olap.DishId", sl.DishID == "")
			miss("olap.OpenDate.Typed", sl.Date == "")
			miss("olap.DishAmountInt", sl.Portions <= 0)
		}
	}
	if bs, err := s.Balance(ctx, now); err != nil {
		fail("остатки", err)
	} else {
		for _, b := range bs {
			r.BalanceRows++
			miss("balance.product", b.Product == "")
			miss("balance.store", b.Store == "")
		}
	}
	return r
}

func (r *Report) print(w io.Writer) {
	f := func(format string, a ...any) { fmt.Fprintf(w, format+"\n", a...) }
	section := func(name string) (failed bool) {
		if e, ok := r.Failed[name]; ok {
			f("%s: ОШИБКА: %s", name, e)
			return true
		}
		return false
	}
	f("Проверка iiko (только чтение), период продаж и накладных: %s — %s (конец не входит)",
		r.From.Format(iiko.DateLayout), r.To.Format(iiko.DateLayout))
	if !section("номенклатура") {
		f("номенклатура: продуктов %d, блюд %d, удалённых %d, прочих типов %d", r.Goods, r.Dishes, r.Deleted, r.OtherTypes)
		if len(r.UnknownUnits) == 0 {
			f("  единицы измерения: все распознаны")
		} else {
			names := make([]string, 0, len(r.UnknownUnits))
			for u := range r.UnknownUnits {
				names = append(names, u)
			}
			sort.Strings(names)
			for _, u := range names {
				f("  единица не распознана: %q — продуктов %d (такие продукты адаптер пропустит)", u, r.UnknownUnits[u])
			}
		}
		f("  без срока хранения: %d из %d продуктов (срок придётся задать вручную, п. 3 регламента)", r.NoShelfLife, r.Goods)
	}
	if !section("техкарты") {
		f("техкарты: всего %d, действующих %d", r.Charts, r.ActiveCharts)
	}
	if !section("накладные") {
		f("накладные: %d (в статусе PROCESSED %d), позиций %d", r.Invoices, r.ProcessedInvoices, r.InvoiceItems)
	}
	if !section("продажи") {
		f("продажи (OLAP): строк %d, порций %d", r.SalesRows, r.SalesPortions)
	}
	if !section("остатки") {
		f("остатки на сейчас: строк %d", r.BalanceRows)
	}
	if len(r.Missing) == 0 {
		f("поля: все ожидаемые поля на месте")
	} else {
		f("поля, которых нет в ответе или которые не разобрались:")
		keys := make([]string, 0, len(r.Missing))
		for k := range r.Missing {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			f("  %s — записей %d", k, r.Missing[k])
		}
	}
}
