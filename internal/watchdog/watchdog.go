// Package watchdog — сторож доступности API и синхронизации адаптера (FS-117, 8.3 п.10).
// Раз в период опрашивает /healthz и /metrics API, при выходе правила за порог шлёт одно
// сообщение в Telegram при входе в тревогу и одно при выходе; повтор — не чаще Repeat.
package watchdog

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"umirhack/internal/clock"
)

// Имена метрик API. Первая есть сегодня; две другие появятся после доработки internal/metrics
// (см. docs/operations.md): пока их нет, соответствующие правила молча не работают.
const (
	MetricOutboxCount  = "outbox_undelivered"
	MetricOutboxOldest = "outbox_oldest_unacked_age_seconds"
	MetricSyncAge      = "adapter_last_sync_age_seconds"
)

// Config — пороги и адреса.
type Config struct {
	APIURL       string // база API, например http://api:8080
	MetricsToken string // Bearer для /metrics; пусто — метрики не опрашиваются
	AdapterURL   string // база адаптера (необязательно): GET /healthz
	SyncMax      time.Duration
	OutboxMax    time.Duration
	Repeat       time.Duration
	DataDir      string  // каталог данных для проверки диска; пусто — правило пропущено
	DiskMinFree  float64 // доля, по умолчанию 0.10
	APIFailures  int     // проверок подряд до тревоги, по умолчанию 2
}

// Notifier отправляет текст инженеру.
type Notifier interface {
	Send(ctx context.Context, text string) error
}

// Watchdog хранит состояние правил между проверками.
type Watchdog struct {
	cfg    Config
	http   *http.Client
	clk    clock.Clock
	notify Notifier
	disk   func(string) (float64, error)

	apiFails int
	rules    map[string]*ruleState
}

type ruleState struct {
	active   bool
	lastSent time.Time
}

// New собирает сторожа; клиент, часы и диск подменяются в тестах.
func New(cfg Config, hc *http.Client, clk clock.Clock, n Notifier) *Watchdog {
	if cfg.DiskMinFree <= 0 {
		cfg.DiskMinFree = 0.10
	}
	if cfg.APIFailures <= 0 {
		cfg.APIFailures = 2
	}
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	return &Watchdog{cfg: cfg, http: hc, clk: clk, notify: n, disk: DiskFreeFraction, rules: map[string]*ruleState{}}
}

// SetDisk подменяет источник доли свободного места.
func (w *Watchdog) SetDisk(f func(string) (float64, error)) { w.disk = f }

type verdict struct {
	bad    bool
	detail string
}

// Check выполняет одну проверку и рассылает сообщения о смене состояния.
func (w *Watchdog) Check(ctx context.Context) {
	res := map[string]verdict{}

	apiErr := w.get(ctx, w.cfg.APIURL, "/healthz", "", nil)
	if apiErr != nil {
		w.apiFails++
	} else {
		w.apiFails = 0
	}
	res["api"] = verdict{w.apiFails >= w.cfg.APIFailures,
		fmt.Sprintf("API не отвечает на /healthz (%d проверок подряд): %v", w.apiFails, apiErr)}

	if w.cfg.AdapterURL != "" {
		err := w.get(ctx, w.cfg.AdapterURL, "/healthz", "", nil)
		res["adapter"] = verdict{err != nil, fmt.Sprintf("адаптер не отвечает на /healthz: %v", err)}
	}

	if w.cfg.MetricsToken != "" && apiErr == nil {
		m := map[string]float64{}
		if err := w.get(ctx, w.cfg.APIURL, "/metrics", w.cfg.MetricsToken, func(r io.Reader) { m = parseMetrics(r) }); err == nil {
			if v, ok := m[MetricSyncAge]; ok && w.cfg.SyncMax > 0 {
				res["sync"] = verdict{v > w.cfg.SyncMax.Seconds(),
					fmt.Sprintf("нет синхронизации адаптера уже %s (порог %s)", dur(v), w.cfg.SyncMax)}
			}
			if v, ok := m[MetricOutboxOldest]; ok && w.cfg.OutboxMax > 0 {
				res["outbox"] = verdict{v > w.cfg.OutboxMax.Seconds(),
					fmt.Sprintf("в outbox неподтверждённое сообщение старше %s (порог %s), в очереди %.0f",
						dur(v), w.cfg.OutboxMax, m[MetricOutboxCount])}
			}
		}
	}

	if w.cfg.DataDir != "" {
		if f, err := w.disk(w.cfg.DataDir); err == nil {
			res["disk"] = verdict{f < w.cfg.DiskMinFree,
				fmt.Sprintf("на диске под данными свободно %.1f%% (порог %.0f%%)", f*100, w.cfg.DiskMinFree*100)}
		}
	}

	names := make([]string, 0, len(res))
	for k := range res {
		names = append(names, k)
	}
	sort.Strings(names)
	now := w.clk.Now()
	for _, name := range names {
		w.apply(ctx, now, name, res[name])
	}
}

func (w *Watchdog) apply(ctx context.Context, now time.Time, name string, v verdict) {
	st := w.rules[name]
	if st == nil {
		st = &ruleState{}
		w.rules[name] = st
	}
	switch {
	case v.bad && !st.active:
		if w.send(ctx, "🚨 "+v.detail) {
			st.active, st.lastSent = true, now
		}
	case v.bad && st.active:
		if w.cfg.Repeat > 0 && now.Sub(st.lastSent) >= w.cfg.Repeat {
			if w.send(ctx, "🚨 (всё ещё) "+v.detail) {
				st.lastSent = now
			}
		}
	case !v.bad && st.active:
		if w.send(ctx, "✅ восстановлено: "+ruleTitle[name]) {
			st.active = false
		}
	}
}

var ruleTitle = map[string]string{
	"api": "API отвечает", "adapter": "адаптер отвечает", "sync": "синхронизация адаптера идёт",
	"outbox": "outbox разбирается", "disk": "свободного места достаточно",
}

// send при ошибке доставки возвращает false: состояние не меняется, следующая проверка повторит.
func (w *Watchdog) send(ctx context.Context, text string) bool {
	return w.notify.Send(ctx, text) == nil
}

// Run проверяет каждые every до отмены ctx.
func (w *Watchdog) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		w.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (w *Watchdog) get(ctx context.Context, base, path, token string, body func(io.Reader)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return fmt.Errorf("статус %d", resp.StatusCode)
	}
	if body != nil {
		body(io.LimitReader(resp.Body, 4<<20))
	}
	return nil
}

// parseMetrics читает безметочные ряды текстового формата Prometheus.
func parseMetrics(r io.Reader) map[string]float64 {
	out := map[string]float64{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.ContainsAny(line, "{") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		if v, err := strconv.ParseFloat(f[1], 64); err == nil {
			out[f[0]] = v
		}
	}
	return out
}

func dur(sec float64) string { return (time.Duration(sec) * time.Second).Round(time.Second).String() }

// Telegram шлёт сообщения через Bot API.
type Telegram struct {
	Base   string // по умолчанию https://api.telegram.org
	Token  string
	Chat   string
	Client *http.Client
}

// Send реализует Notifier.
func (t Telegram) Send(ctx context.Context, text string) error {
	base := t.Base
	if base == "" {
		base = "https://api.telegram.org"
	}
	c := t.Client
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}
	form := url.Values{"chat_id": {t.Chat}, "text": {text}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/bot"+t.Token+"/sendMessage", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: недоступен") // без err: URL содержит токен
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram: статус %d", resp.StatusCode)
	}
	return nil
}
