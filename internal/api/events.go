package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"umirhack/internal/events"
)

// pingEvery — период ping в реальном времени (7.5), не зависит от скорости часов стенда.
const pingEvery = 15 * time.Second

// writeTimeout ограничивает одну запись в поток: зависший клиент не держит соединение вечно.
const writeTimeout = 10 * time.Second

func (rt *Router) eventsRoutes() {
	if rt.deps.Events == nil {
		return
	}
	rt.Handle(http.MethodGet, "/events", rt.streamEvents)
}

// streamEvents — GET /events (text/event-stream). Права — viewer (2.9). Открытие и закрытие
// соединения пишутся в журнал как аудит.
func (rt *Router) streamEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		rt.internalError(w, r, err)
		return
	}

	sub := rt.deps.Events.Subscribe()
	defer sub.Unsubscribe()
	rt.audit(r, "events_open")
	reason := "client_gone"
	defer func() { rt.audit(r, "events_close", slog.String("reason", reason)) }()

	every := rt.deps.PingEvery
	if every <= 0 {
		every = pingEvery
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	write := func(id int64, typ string, data []byte) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(writeTimeout))
		var err error
		if id > 0 {
			_, err = fmt.Fprintf(w, "event: %s\ndata: %s\nid: %d\n\n", typ, data, id)
		} else {
			_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, data)
		}
		if err == nil {
			err = rc.Flush()
		}
		return err == nil
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-sub.C:
			if !ok { // переполнение очереди или остановка сервера: клиент переподключится
				reason = "closed_by_server"
				return
			}
			if !write(ev.ID, ev.Type, ev.Data) {
				reason = "write_failed"
				return
			}
		case <-ticker.C:
			data := fmt.Sprintf(`{"at":%q}`, rt.deps.Now().UTC().Format(time.RFC3339))
			if !write(0, events.Ping, []byte(data)) {
				reason = "write_failed"
				return
			}
		}
	}
}
