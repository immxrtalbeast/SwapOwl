package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Kind — тип уведомления, который менеджер может отключить.
type Kind string

const (
	KindExpiring Kind = "expiring" // истекающие партии
	KindStop     Kind = "stop"     // стоп-лист
	KindDigest   Kind = "digest"   // дневная сводка
	KindWeekly   Kind = "weekly"   // недельный отчёт
	KindHall     Kind = "hall"     // подсказки залу
)

// Kinds — порядок типов в /settings.
var Kinds = []Kind{KindExpiring, KindStop, KindDigest, KindWeekly, KindHall}

// KindTitle — подпись типа в кнопке.
var KindTitle = map[Kind]string{
	KindExpiring: "Истекающие партии",
	KindStop:     "Стоп-лист",
	KindDigest:   "Дневная сводка",
	KindWeekly:   "Недельный отчёт",
	KindHall:     "Подсказки залу",
}

const (
	settingsVersion  = 1
	maxSentKeys      = 1000
	defaultQuietFrom = "22:00"
	defaultQuietTo   = "07:00"
)

// queued — событие, отложенное на тихие часы или паузу.
type queued struct {
	Kind Kind      `json:"kind"`
	Key  string    `json:"key,omitempty"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// settingsData — содержимое файла состояния.
type settingsData struct {
	Version   int                  `json:"version"`
	Disabled  map[Kind]bool        `json:"disabled,omitempty"`
	Quiet     bool                 `json:"quiet"`
	QuietFrom string               `json:"quiet_from"`
	QuietTo   string               `json:"quiet_to"`
	Paused    time.Time            `json:"paused_until,omitempty"`
	Sent      map[string]time.Time `json:"sent,omitempty"` // ключ дедупликации (тип, объект, момент)
	Queue     []queued             `json:"queue,omitempty"`
}

func defaultData() settingsData {
	return settingsData{Version: settingsVersion, Disabled: map[Kind]bool{}, QuietFrom: defaultQuietFrom, QuietTo: defaultQuietTo, Sent: map[string]time.Time{}}
}

// Settings — настройки уведомлений, очередь тихих часов и ключи дедупликации в файле NOTIFY_STATE_FILE.
// Пустой путь — состояние только в памяти.
type Settings struct {
	path string
	logf func(string, ...any)
	mu   sync.Mutex
	d    settingsData
}

// LoadSettings читает файл состояния. Нет файла — значения по умолчанию; повреждённый или
// неизвестной версии — значения по умолчанию и запись в журнал (бот не падает).
func LoadSettings(path string, logf func(string, ...any)) *Settings {
	s := &Settings{path: path, logf: logf, d: defaultData()}
	if path == "" {
		return s
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.log("файл состояния: %v — беру значения по умолчанию", err)
		}
		return s
	}
	var d settingsData
	if err := json.Unmarshal(b, &d); err != nil {
		s.log("файл состояния повреждён: %v — беру значения по умолчанию", err)
		return s
	}
	if d.Version != settingsVersion {
		s.log("файл состояния: версия формата %d не поддерживается — беру значения по умолчанию", d.Version)
		return s
	}
	if d.Disabled == nil {
		d.Disabled = map[Kind]bool{}
	}
	if d.Sent == nil {
		d.Sent = map[string]time.Time{}
	}
	if _, _, ok := parseHM2(d.QuietFrom, d.QuietTo); !ok {
		d.QuietFrom, d.QuietTo = defaultQuietFrom, defaultQuietTo
	}
	s.d = d
	return s
}

func (s *Settings) log(f string, a ...any) {
	if s.logf != nil {
		s.logf(f, a...)
	}
}

// saveLocked пишет файл атомарно (временный файл + rename). Вызывать под s.mu.
func (s *Settings) saveLocked() {
	if s.path == "" {
		return
	}
	if len(s.d.Sent) > maxSentKeys {
		type kv struct {
			k string
			t time.Time
		}
		all := make([]kv, 0, len(s.d.Sent))
		for k, t := range s.d.Sent {
			all = append(all, kv{k, t})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].t.After(all[j].t) })
		s.d.Sent = map[string]time.Time{}
		for _, e := range all[:maxSentKeys] {
			s.d.Sent[e.k] = e.t
		}
	}
	b, err := json.MarshalIndent(s.d, "", " ")
	if err == nil {
		tmp := s.path + ".tmp"
		if err = os.MkdirAll(filepath.Dir(s.path), 0o755); err == nil {
			if err = os.WriteFile(tmp, b, 0o600); err == nil {
				err = os.Rename(tmp, s.path)
			}
		}
	}
	if err != nil {
		s.log("не удалось записать файл состояния: %v", err)
	}
}

func parseHM2(a, b string) (from, to int, ok bool) {
	h1, m1, ok1 := parseHM(a)
	h2, m2, ok2 := parseHM(b)
	return h1*60 + m1, h2*60 + m2, ok1 && ok2
}

// Enabled — включён ли тип.
func (s *Settings) Enabled(k Kind) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.d.Disabled[k]
}

// Toggle переключает тип и возвращает новое значение «включён».
func (s *Settings) Toggle(k Kind) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.Disabled[k] {
		delete(s.d.Disabled, k)
	} else {
		s.d.Disabled[k] = true
	}
	s.saveLocked()
	return !s.d.Disabled[k]
}

// QuietInfo — тихие часы: включены ли и границы «ЧЧ:ММ».
func (s *Settings) QuietInfo() (on bool, from, to string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Quiet, s.d.QuietFrom, s.d.QuietTo
}

// SetQuiet включает/выключает тихие часы; пустые границы не меняют прежние.
func (s *Settings) SetQuiet(on bool, from, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if from != "" || to != "" {
		if _, _, ok := parseHM2(from, to); !ok {
			return errors.New("нужен формат ЧЧ:ММ")
		}
		s.d.QuietFrom, s.d.QuietTo = from, to
	}
	s.d.Quiet = on
	s.saveLocked()
	return nil
}

// Pause ставит паузу на d от now; d == 0 снимает паузу.
func (s *Settings) Pause(now time.Time, d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d <= 0 {
		s.d.Paused = time.Time{}
	} else {
		s.d.Paused = now.Add(d)
	}
	s.saveLocked()
}

// PausedUntil — конец паузы (нулевое время — пауза не стоит или закончилась к now).
func (s *Settings) PausedUntil(now time.Time) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.Paused.After(now) {
		return s.d.Paused
	}
	return time.Time{}
}

// Muted — сейчас нельзя слать: пауза или тихие часы (по времени ресторана в loc).
func (s *Settings) Muted(now time.Time, loc *time.Location) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.Paused.After(now) {
		return true
	}
	if !s.d.Quiet {
		return false
	}
	from, to, ok := parseHM2(s.d.QuietFrom, s.d.QuietTo)
	if !ok || from == to {
		return false
	}
	l := now.In(loc)
	m := l.Hour()*60 + l.Minute()
	if from < to {
		return m >= from && m < to
	}
	return m >= from || m < to
}

func (s *Settings) seen(key string) bool {
	if key == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.d.Sent[key]; ok {
		return true
	}
	for _, q := range s.d.Queue {
		if q.Key == key {
			return true
		}
	}
	return false
}

func (s *Settings) markSent(key string, at time.Time) {
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Sent[key] = at
	s.saveLocked()
}

func (s *Settings) enqueue(q queued) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Queue = append(s.d.Queue, q)
	s.saveLocked()
}

// QueueLen — сколько событий копится.
func (s *Settings) QueueLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.d.Queue)
}

// pruneQueue выбрасывает из очереди события выключенных типов.
func (s *Settings) pruneQueue(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := s.d.Queue[:0:0]
	for _, q := range s.d.Queue {
		if s.d.Disabled[q.Kind] {
			if q.Key != "" {
				s.d.Sent[q.Key] = at
			}
			continue
		}
		keep = append(keep, q)
	}
	s.d.Queue = keep
	s.saveLocked()
}

// takeQueue — копия очереди.
func (s *Settings) takeQueue() []queued {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]queued(nil), s.d.Queue...)
}

func (s *Settings) dropQueued(n int, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > len(s.d.Queue) {
		n = len(s.d.Queue)
	}
	for _, q := range s.d.Queue[:n] {
		if q.Key != "" {
			s.d.Sent[q.Key] = at
		}
	}
	s.d.Queue = append([]queued(nil), s.d.Queue[n:]...)
	s.saveLocked()
}

// --- отправка с повторами ---

const (
	// DefaultSendAttempts — сколько раз пробуем отправить сообщение в Telegram.
	DefaultSendAttempts = 4
	baseBackoff         = 2 * time.Second
	maxBackoff          = time.Minute
)

var (
	reStatus     = regexp.MustCompile(`статус (\d{3})`)
	reRetryAfter = regexp.MustCompile(`"retry_after"\s*:\s*(\d+)`)
)

// retryDelay решает, повторять ли ошибку Telegram и через какую паузу: 429 — retry_after, 5xx и
// транспорт — нарастающая пауза, остальные 4xx не повторяются.
func retryDelay(err error, attempt int) (time.Duration, bool) {
	if errors.Is(err, context.Canceled) {
		return 0, false
	}
	msg := err.Error()
	back := baseBackoff << attempt
	if back > maxBackoff {
		back = maxBackoff
	}
	m := reStatus.FindStringSubmatch(msg)
	if m == nil {
		return back, true // сеть
	}
	code, _ := strconv.Atoi(m[1])
	switch {
	case code == 429:
		if r := reRetryAfter.FindStringSubmatch(msg); r != nil {
			secs, _ := strconv.Atoi(r[1])
			return time.Duration(secs) * time.Second, true
		}
		return back, true
	case code >= 500:
		return back, true
	}
	return 0, false
}

func (n *Notifier) sleep(ctx context.Context, d time.Duration) {
	if n.Sleep != nil {
		n.Sleep(d)
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// sendRetry шлёт сообщение с повторами (n.Attempts, по умолчанию DefaultSendAttempts). Последняя ошибка
// возвращается вызывающему: он решает, повторять ли на следующем проходе; остальные сообщения не блокируются.
func (n *Notifier) sendRetry(ctx context.Context, out Sender, text string) error {
	attempts := n.Attempts
	if attempts < 1 {
		attempts = DefaultSendAttempts
	}
	var err error
	for i := 0; i < attempts; i++ {
		if err = out.Send(ctx, text); err == nil {
			return nil
		}
		d, again := retryDelay(err, i)
		if !again || i == attempts-1 || ctx.Err() != nil {
			break
		}
		n.logf("отправка: %v — повтор через %s", err, d)
		n.sleep(ctx, d)
	}
	n.logf("отправка не удалась после повторов: %v", err)
	return err
}

// now — текущее время стенда (часы API), иначе системное.
func (n *Notifier) now(ctx context.Context) time.Time {
	if c, err := n.API.Clock(ctx); err == nil {
		return c.Now
	}
	return time.Now()
}

// deliver — единая точка отправки: настройки типа, тихие часы/пауза, дедупликация, повторы.
// Без n.Settings шлёт как раньше. Возвращает ошибку только если отправка не удалась (тогда ключ не
// записан и событие можно повторить); отключённый тип, дубликат и отложенное событие — не ошибка.
func (n *Notifier) deliver(ctx context.Context, kind Kind, key, text string, out Sender) error {
	s := n.Settings
	if s == nil {
		return out.Send(ctx, text)
	}
	now := n.now(ctx)
	if s.seen(key) {
		return nil
	}
	if !s.Enabled(kind) {
		s.markSent(key, now) // отключённое не всплывёт при повторном включении
		return nil
	}
	if s.Muted(now, n.loc(ctx)) {
		if kind != KindStop && kind != KindHall { // стоп-лист устаревает; подсказки залу уходят другому адресату — их повторит HallTick
			s.enqueue(queued{Kind: kind, Key: key, Text: text, At: now})
		}
		return nil
	}
	if err := n.sendRetry(ctx, out, text); err != nil {
		return err
	}
	s.markSent(key, now)
	return nil
}

// allowed — для рассылок по расписанию: слать ли сейчас. skip=true — не слать, но день считается
// обработанным (тип выключен); wait=true — отложить (тихие часы/пауза), повторим на следующем тике.
func (n *Notifier) allowed(ctx context.Context, kind Kind) (send, done bool) {
	s := n.Settings
	if s == nil {
		return true, false
	}
	if !s.Enabled(kind) {
		return false, true
	}
	if s.Muted(n.now(ctx), n.loc(ctx)) {
		return false, false
	}
	return true, false
}

// FlushQuiet одним сообщением (при необходимости несколькими по лимиту Telegram) отправляет накопленное,
// когда тихие часы/пауза кончились. Вызывается из Tick и по «Включить сейчас».
func (n *Notifier) FlushQuiet(ctx context.Context) {
	s := n.Settings
	if s == nil || s.QueueLen() == 0 {
		return
	}
	n.sendMu.Lock()
	defer n.sendMu.Unlock()
	now := n.now(ctx)
	if s.Muted(now, n.loc(ctx)) {
		return
	}
	s.pruneQueue(now) // тип успели выключить, пока копилось
	items := s.takeQueue()
	if len(items) == 0 {
		return
	}
	// склейка в чанки до 3500 символов, порядок сохраняется
	const limit = 3500
	var chunks [][]queued
	var cur []queued
	size := 0
	for _, q := range items {
		l := len([]rune(q.Text)) + 2
		if size+l > limit && len(cur) > 0 {
			chunks = append(chunks, cur)
			cur, size = nil, 0
		}
		cur = append(cur, q)
		size += l
	}
	chunks = append(chunks, cur)
	for i, c := range chunks {
		texts := make([]string, len(c))
		for j, q := range c {
			texts[j] = q.Text
		}
		head := ""
		if i == 0 {
			head = fmt.Sprintf("🌅 Накопилось, пока вы не получали уведомления (%d):\n\n", len(items))
		}
		if err := n.sendRetry(ctx, n.Out, head+strings.Join(texts, "\n\n")); err != nil {
			return // отправленные чанки уже убраны из очереди, остальное уйдёт на следующем тике
		}
		s.dropQueued(len(c), now)
	}
}
