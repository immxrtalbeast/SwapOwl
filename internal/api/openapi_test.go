package api

import (
	"bufio"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"umirhack/internal/auth"
	"umirhack/internal/events"
	"umirhack/internal/metrics"
)

// Пустые реализации: хендлеры не вызываются, нужна лишь непустая зависимость,
// чтобы роутер зарегистрировал все маршруты.
type (
	allStock       struct{ Stock }
	allCatalog     struct{ Catalog }
	allOrders      struct{ Orders }
	allCorrections struct{ Corrections }
	allMenu        struct{ MenuSource }
	allHot         struct{ HotOffersSource }
	allSupply      struct{ SupplySource }
	allSupplyOrd   struct{ SupplyOrders }
	allOutbox      struct{ Outbox }
	allAnalytics   struct{ Analytics }
	allMonitor     struct{ Monitor }
	allDemo        struct{ Demo }
	allSim         struct{ SimControl }
	allAudit       struct{ AuditLog }
)

// routerPairs — множество «МЕТОД шаблон» всех маршрутов полностью собранного роутера.
// HEAD — автоматический двойник GET, в спецификации не описывается.
func routerPairs() map[string]bool {
	rt := NewRouter(Deps{
		Auth: &auth.Service{}, Audit: allAudit{}, Stock: allStock{}, Catalog: allCatalog{},
		Orders: allOrders{}, Corrections: allCorrections{}, Menu: allMenu{}, HotOffers: allHot{},
		Supply: allSupply{}, SupplyOrders: allSupplyOrd{}, Outbox: allOutbox{}, Analytics: allAnalytics{}, Monitor: allMonitor{},
		Events: events.New(nil), Demo: allDemo{}, Sim: allSim{}, Metrics: metrics.New(), Invoices: &Invoices{},
	})
	out := map[string]bool{}
	for pattern, byMethod := range rt.routes {
		for m := range byMethod {
			if m == "HEAD" {
				continue
			}
			out[m+" "+pattern] = true
		}
	}
	return out
}

var (
	specPathRe   = regexp.MustCompile(`^  (/[^\s:]*):\s*$`)
	specMethodRe = regexp.MustCompile(`^    (get|put|post|delete|patch|head|options):\s*$`)
)

// specPairs читает из docs/openapi.yaml только секцию paths (без зависимостей от YAML-библиотек).
func specPairs(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open("../../docs/openapi.yaml")
	if err != nil {
		t.Fatalf("open spec: %v", err)
	}
	defer f.Close()
	out := map[string]bool{}
	inPaths, path := false, ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, " ") && strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "#") {
			inPaths = strings.HasPrefix(line, "paths:")
			continue
		}
		if !inPaths {
			continue
		}
		if m := specPathRe.FindStringSubmatch(line); m != nil {
			path = m[1]
			continue
		}
		if m := specMethodRe.FindStringSubmatch(line); m != nil && path != "" {
			out[strings.ToUpper(m[1])+" "+path] = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOpenAPIMatchesRouter(t *testing.T) {
	code, spec := routerPairs(), specPairs(t)
	if len(spec) == 0 {
		t.Fatal("в docs/openapi.yaml нет ни одной операции в paths")
	}
	var missing, extra []string
	for p := range code {
		if !spec[p] {
			missing = append(missing, p)
		}
	}
	for p := range spec {
		if !code[p] {
			extra = append(extra, p)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("маршруты есть в роутере, но не описаны в docs/openapi.yaml:\n  %s", strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		t.Errorf("операции описаны в docs/openapi.yaml, но их нет в роутере:\n  %s", strings.Join(extra, "\n  "))
	}
}
