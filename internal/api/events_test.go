package api

import (
	"bufio"
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"umirhack/internal/events"
)

func newEventsServer(t *testing.T, ping time.Duration) (*httptest.Server, *events.Bus, *syncBuf) {
	t.Helper()
	buf := &syncBuf{}
	bus := events.New(func() time.Time { return standTime })
	rt := NewRouter(Deps{Log: slog.New(slog.NewJSONHandler(buf, nil)), Now: func() time.Time { return standTime },
		Events: bus, PingEvery: ping})
	srv := httptest.NewServer(rt.Handler())
	t.Cleanup(srv.Close)
	return srv, bus, buf
}

// syncBuf — буфер журнала, в который пишут несколько горутин.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func readUntil(t *testing.T, sc *bufio.Scanner, want string) []string {
	t.Helper()
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if strings.Contains(sc.Text(), want) {
			return lines
		}
	}
	t.Fatalf("stream ended before %q; got %q", want, lines)
	return nil
}

func TestEventsStreamFormatAndPing(t *testing.T) {
	srv, bus, _ := newEventsServer(t, 20*time.Millisecond)
	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	readUntil(t, sc, "event: ping")

	bus.Publish(events.OrderAccepted, map[string]any{"order_id": "o1"})
	got := strings.Join(readUntil(t, sc, "id: "), "\n")
	if !strings.Contains(got, "event: order.accepted") || !strings.Contains(got, `"order_id":"o1"`) ||
		!strings.Contains(got, `"at":"2026-10-10T12:00:00Z"`) {
		t.Fatalf("bad frame: %q", got)
	}
}

func TestEventsOpenCloseAuditedAndUnsubscribed(t *testing.T) {
	srv, bus, buf := newEventsServer(t, time.Hour)
	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return bus.Subscribers() == 1 })
	resp.Body.Close()
	waitFor(t, func() bool { return bus.Subscribers() == 0 })
	waitFor(t, func() bool { return strings.Contains(buf.String(), `"event":"events_close"`) })
	if !strings.Contains(buf.String(), `"event":"events_open"`) {
		t.Fatalf("no open audit: %s", buf.String())
	}
}

func TestEventsSlowSubscriberClosed(t *testing.T) {
	srv, bus, _ := newEventsServer(t, time.Hour)
	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	waitFor(t, func() bool { return bus.Subscribers() == 1 })
	// Клиент не читает, а событий больше буфера: шина не блокируется, соединение закрывается.
	for i := 0; i < events.SubscriberBuffer*200; i++ {
		bus.Publish(events.OrderAccepted, map[string]any{"pad": strings.Repeat("x", 1024)})
	}
	waitFor(t, func() bool { return bus.Subscribers() == 0 })
}

func TestEventsRequiresViewerNotRegisteredWithoutBus(t *testing.T) {
	rt := NewRouter(Deps{Now: func() time.Time { return standTime }})
	if rec := do(rt.Handler(), http.MethodGet, "/events", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("code %d", rec.Code)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
