// Package metrics — минимальный реестр метрик в текстовом формате Prometheus без внешних зависимостей (8.0а).
package metrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefBuckets — границы гистограммы длительности запроса, секунды.
var DefBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type histogram struct {
	counts []uint64 // по границам, кумулятивно считается при выводе
	count  uint64
	sum    float64
}

type reqKey struct{ method, route, status string }

// Registry собирает метрики запросов и значения-«датчики», которые читаются при выдаче.
type Registry struct {
	mu       sync.Mutex
	buckets  []float64
	requests map[reqKey]uint64
	durs     map[string]*histogram // по шаблону пути
	gauges   []gauge
}

type gauge struct {
	name, help string
	fn         func() float64
}

// New создаёт пустой реестр.
func New() *Registry {
	return &Registry{buckets: DefBuckets, requests: map[reqKey]uint64{}, durs: map[string]*histogram{}}
}

// Gauge регистрирует метрику-датчик: fn вызывается при каждой выдаче.
func (r *Registry) Gauge(name, help string, fn func() float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges = append(r.gauges, gauge{name, help, fn})
}

// ObserveRequest учитывает запрос; route — шаблон пути, не сырой URL.
func (r *Registry) ObserveRequest(method, route string, status int, d time.Duration) {
	sec := d.Seconds()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests[reqKey{method, route, strconv.Itoa(status)}]++
	h := r.durs[route]
	if h == nil {
		h = &histogram{counts: make([]uint64, len(r.buckets))}
		r.durs[route] = h
	}
	h.count++
	h.sum += sec
	for i, b := range r.buckets {
		if sec <= b {
			h.counts[i]++
			break
		}
	}
}

var esc = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func num(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// Write пишет все метрики в формате Prometheus text 0.0.4.
func (r *Registry) Write(w io.Writer) error {
	r.mu.Lock()
	reqs := make([]reqKey, 0, len(r.requests))
	for k := range r.requests {
		reqs = append(reqs, k)
	}
	sort.Slice(reqs, func(i, j int) bool {
		a, b := reqs[i], reqs[j]
		if a.route != b.route {
			return a.route < b.route
		}
		if a.method != b.method {
			return a.method < b.method
		}
		return a.status < b.status
	})
	var sb strings.Builder
	sb.WriteString("# HELP http_requests_total Запросы по методу, шаблону пути и статусу.\n# TYPE http_requests_total counter\n")
	for _, k := range reqs {
		fmt.Fprintf(&sb, "http_requests_total{method=\"%s\",route=\"%s\",status=\"%s\"} %d\n",
			esc.Replace(k.method), esc.Replace(k.route), k.status, r.requests[k])
	}
	routes := make([]string, 0, len(r.durs))
	for k := range r.durs {
		routes = append(routes, k)
	}
	sort.Strings(routes)
	sb.WriteString("# HELP http_request_duration_seconds Длительность запроса по шаблону пути.\n# TYPE http_request_duration_seconds histogram\n")
	for _, rt := range routes {
		h, lbl := r.durs[rt], esc.Replace(rt)
		var cum uint64
		for i, b := range r.buckets {
			cum += h.counts[i]
			fmt.Fprintf(&sb, "http_request_duration_seconds_bucket{route=\"%s\",le=\"%s\"} %d\n", lbl, num(b), cum)
		}
		fmt.Fprintf(&sb, "http_request_duration_seconds_bucket{route=\"%s\",le=\"+Inf\"} %d\n", lbl, h.count)
		fmt.Fprintf(&sb, "http_request_duration_seconds_sum{route=\"%s\"} %s\n", lbl, num(h.sum))
		fmt.Fprintf(&sb, "http_request_duration_seconds_count{route=\"%s\"} %d\n", lbl, h.count)
	}
	gs := append([]gauge(nil), r.gauges...)
	r.mu.Unlock()

	// Датчики читаем без блокировки реестра: fn может ходить в БД.
	for _, g := range gs {
		fmt.Fprintf(&sb, "# HELP %s %s\n# TYPE %s gauge\n%s %s\n", g.name, g.help, g.name, g.name, num(g.fn()))
	}
	_, err := io.WriteString(w, sb.String())
	return err
}
