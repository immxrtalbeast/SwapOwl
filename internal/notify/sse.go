package notify

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// Event — событие SSE.
type Event struct {
	Type string
	Data string
}

// ReadEvents разбирает поток text/event-stream до конца потока.
func ReadEvents(r io.Reader, fn func(Event)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var ev Event
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if ev.Type != "" {
				fn(ev)
			}
			ev = Event{}
		case strings.HasPrefix(line, "event:"):
			ev.Type = strings.TrimSpace(line[len("event:"):])
		case strings.HasPrefix(line, "data:"):
			ev.Data = strings.TrimSpace(line[len("data:"):])
		}
	}
	return sc.Err()
}

// Subscribe держит подписку на GET /events с переподключением, пока жив ctx.
// onConnect вызывается после каждого подключения: события за разрыв не повторяются.
func (c *APIClient) Subscribe(ctx context.Context, retry time.Duration, onConnect func(), fn func(Event), logf func(string, ...any)) {
	for ctx.Err() == nil {
		err := c.stream(ctx, onConnect, fn)
		if ctx.Err() != nil {
			return
		}
		logf("поток событий оборвался: %v", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(retry):
		}
	}
}

func (c *APIClient) stream(ctx context.Context, onConnect func(), fn func(Event)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.Base, "/")+"/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Accept", "text/event-stream")
	hc := *c.HC
	hc.Timeout = 0 // поток долгий
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &statusError{resp.StatusCode}
	}
	if onConnect != nil {
		onConnect()
	}
	return ReadEvents(resp.Body, fn)
}

type statusError struct{ code int }

func (e *statusError) Error() string { return "events: статус " + http.StatusText(e.code) }
