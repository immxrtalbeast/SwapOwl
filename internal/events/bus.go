// Package events — шина событий склада для SSE-потока /events (спецификация, 7.5).
// События — сигналы «перечитай»: полезная нагрузка минимальна, потеря события лечится
// переподключением клиента, поэтому медленного подписчика шина не ждёт, а закрывает.
package events

import (
	"encoding/json"
	"sync"
	"time"
)

// Типы событий (7.5).
const (
	DeliveryAccepted = "delivery.accepted"
	OrderAccepted    = "order.accepted"
	OrderRejected    = "order.rejected"
	BatchExpiring    = "batch.expiring"
	BatchExpired     = "batch.expired"
	StockChanged     = "stock.changed"
	StoplistChanged  = "stoplist.changed"
	ClockChanged     = "clock.changed"
	StandReset       = "stand.reset"
	Ping             = "ping"
)

// SubscriberBuffer — размер буфера канала подписчика и, отдельно, очереди склейки за ним;
// переполнение обоих закрывает соединение.
const SubscriberBuffer = 64

// Event — одно событие потока. Data — готовый JSON с полем "at".
type Event struct {
	ID   int64
	Type string
	Data []byte
}

// Bus рассылает события подписчикам; Publish никогда не блокируется.
type Bus struct {
	now func() time.Time

	mu     sync.Mutex
	nextID int64
	subs   map[*Subscription]struct{}
	closed bool
}

// New создаёт шину; now — время стенда для поля "at".
func New(now func() time.Time) *Bus {
	return &Bus{now: now, subs: map[*Subscription]struct{}{}}
}

// Subscription — подписка одного соединения. C закрывается, когда шина отключила подписчика
// (переполнение очереди или остановка шины).
//
// Событие сразу уходит в буфер канала C (SubscriberBuffer). Если читатель отстал и буфер
// полон, события копятся во второй очереди pending, где одинаковые сигналы склеиваются;
// из неё в C их переливает насос. Отключение — только когда переполнена и pending.
type Subscription struct {
	C   <-chan Event
	ch  chan Event
	bus *Bus

	pending []Event       // под bus.mu
	pumping bool          // под bus.mu: насос жив и владеет порядком выдачи
	closed  bool          // под bus.mu: новые события не принимаются, C закроется после выдачи
	chShut  bool          // под bus.mu: ch уже закрыт
	done    chan struct{} // закрыт при Unsubscribe: потребитель ушёл
}

// Subscribe добавляет подписчика. После Close шины возвращает уже закрытую подписку.
func (b *Bus) Subscribe() *Subscription {
	ch := make(chan Event, SubscriberBuffer)
	s := &Subscription{C: ch, ch: ch, bus: b, done: make(chan struct{})}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		s.closed = true
		s.shut()
		return s
	}
	b.subs[s] = struct{}{}
	return s
}

// shut закрывает C, если больше нечего выдавать. Вызывать под mu.
func (s *Subscription) shut() {
	if s.closed && !s.pumping && !s.chShut {
		s.chShut = true
		close(s.ch)
	}
}

// pump переливает pending в C по порядку; живёт, пока pending не пуст.
func (s *Subscription) pump() {
	for {
		s.bus.mu.Lock()
		if len(s.pending) == 0 {
			s.pumping = false
			s.shut()
			s.bus.mu.Unlock()
			return
		}
		ev := s.pending[0]
		s.pending = s.pending[1:]
		s.bus.mu.Unlock()
		select {
		case s.ch <- ev:
		case <-s.done:
			s.bus.mu.Lock()
			s.pending, s.pumping = nil, false
			s.shut()
			s.bus.mu.Unlock()
			return
		}
	}
}

// Unsubscribe снимает подписку; безопасен при повторном вызове.
func (s *Subscription) Unsubscribe() {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	s.bus.drop(s)
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

// drop отключает подписчика: уже накопленные события ещё выдаются, затем C закрывается.
// Вызывать под mu.
func (b *Bus) drop(s *Subscription) {
	if _, ok := b.subs[s]; ok {
		delete(b.subs, s)
		s.closed = true
		s.shut()
	}
}

// push доставляет событие; false — буфер C и очередь pending переполнены даже после склейки.
// Пока pending пуст и насос не работает, событие идёт прямо в C. Иначе оно встаёт в pending
// и склеивается с последним стоящим там событием того же типа (не только с хвостом): это
// сигнал «перечитай», данные к моменту публикации уже зафиксированы, поэтому порядок
// относительно order.* между такими сигналами не важен. Склеенное событие уходит в хвост
// с id последнего — id в потоке остаются возрастающими. Вызывать под mu.
func (s *Subscription) push(ev Event) bool {
	if !s.pumping {
		select {
		case s.ch <- ev:
			return true
		default:
		}
	}
	for i := len(s.pending) - 1; i >= 0; i-- {
		if s.pending[i].Type != ev.Type {
			continue
		}
		if merged, ok := coalesce(s.pending[i], ev); ok {
			s.pending = append(s.pending[:i], s.pending[i+1:]...)
			s.pending = append(s.pending, merged)
			return true
		}
		break
	}
	if len(s.pending) >= SubscriberBuffer {
		return false
	}
	s.pending = append(s.pending, ev)
	if !s.pumping {
		s.pumping = true
		go s.pump()
	}
	return true
}

// Publish присваивает событию монотонный id, добавляет "at" и раздаёт подписчикам.
// Подряд идущие события-сигналы склеиваются в очереди (coalesce); подписчик, у которого
// очередь переполнена и после склейки, отключается: клиент перечитает всё при переподключении.
func (b *Bus) Publish(typ string, data map[string]any) Event {
	payload := make(map[string]any, len(data)+1)
	for k, v := range data {
		payload[k] = v
	}
	payload["at"] = b.now().UTC().Format(time.RFC3339)
	raw, err := json.Marshal(payload)
	if err != nil { // значения собирают сами издатели; сюда не попадаем
		raw = []byte(`{}`)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	ev := Event{ID: b.nextID, Type: typ, Data: raw}
	for s := range b.subs {
		if !s.push(ev) {
			b.drop(s)
		}
	}
	return ev
}

// Close отключает всех подписчиков (остановка сервера): открытые SSE-соединения завершаются.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for s := range b.subs {
		b.drop(s)
	}
}

// Subscribers — число подписчиков (для тестов и журнала).
func (b *Bus) Subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
