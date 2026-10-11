// Package events — шина событий склада для SSE-потока /events (.ai/solution.md, 7.5).
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

// SubscriberBuffer — размер очереди подписчика; переполнение закрывает соединение.
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
type Subscription struct {
	C   <-chan Event
	ch  chan Event
	bus *Bus
}

// Subscribe добавляет подписчика. После Close шины возвращает уже закрытую подписку.
func (b *Bus) Subscribe() *Subscription {
	ch := make(chan Event, SubscriberBuffer)
	s := &Subscription{C: ch, ch: ch, bus: b}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(ch)
		return s
	}
	b.subs[s] = struct{}{}
	return s
}

// Unsubscribe снимает подписку; безопасен при повторном вызове.
func (s *Subscription) Unsubscribe() {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	s.bus.drop(s)
}

// drop закрывает и удаляет подписчика; вызывать под mu.
func (b *Bus) drop(s *Subscription) {
	if _, ok := b.subs[s]; ok {
		delete(b.subs, s)
		close(s.ch)
	}
}

// Publish присваивает событию монотонный id, добавляет "at" и раздаёт подписчикам.
// Подписчик с полной очередью отключается: клиент перечитает всё при переподключении.
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
		select {
		case s.ch <- ev:
		default:
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
