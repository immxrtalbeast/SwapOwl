package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"umirhack/internal/auth"
	"umirhack/internal/store"
)

// Outbox — очередь исходящих сообщений для адаптера (8.0а). nil в Deps.Outbox — маршруты не регистрируются.
type Outbox interface {
	ListOutbox(ctx context.Context, after int64, limit int) ([]store.OutboxEntry, error)
	AckOutbox(ctx context.Context, id int64, ok bool, errMsg string, now time.Time) error
}

// maxAckError — длина last_error, дальше обрезается: адаптер может прислать стек.
const maxAckError = 2000

func (rt *Router) outboxRoutes() {
	if rt.deps.Outbox == nil {
		return
	}
	// Ключ адаптера — роль operator (2.9); читать очередь viewer не должен.
	rt.HandleRole(http.MethodGet, "/api/outbox", auth.Operator, rt.listOutbox)
	rt.HandleRole(http.MethodPost, "/api/outbox/{id}/ack", auth.Operator, rt.ackOutbox)
}

// StopListPayload — полный текущий стоп-лист для сообщения kind=stoplist: доставки последней строки достаточно.
func StopListPayload(snap store.MenuSnapshot) []DishAvailability {
	out := []DishAvailability{}
	for _, row := range menuAvailability(snap) {
		if !row.Available {
			out = append(out, row)
		}
	}
	return out
}

// cleanAckError готовит last_error для text: без NUL (Postgres его не принимает) и с обрезкой
// по границе руны, иначе половина многобайтового символа даёт 22021 и ack падает с 500.
func cleanAckError(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	if len(s) > maxAckError {
		s = s[:maxAckError]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

// listOutbox — GET /api/outbox?after=&limit=: недоставленные сообщения по возрастанию id.
func (rt *Router) listOutbox(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var after int64
	if v := q.Get("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			validationError(w, "after", "after must be a non-negative integer")
			return
		}
		after = n
	}
	limit := 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > store.MaxOutboxLimit {
			validationError(w, "limit", "limit must be an integer from 1 to "+strconv.Itoa(store.MaxOutboxLimit))
			return
		}
		limit = n
	}
	items, err := rt.deps.Outbox.ListOutbox(r.Context(), after, limit)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, items)
}

type ackRequest struct {
	OK    *bool  `json:"ok"`
	Error string `json:"error"`
}

// ackOutbox — POST /api/outbox/{id}/ack {ok, error?}: доставлено, либо attempts+1 и last_error.
func (rt *Router) ackOutbox(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		WriteError(w, http.StatusNotFound, CodeNotFound, "outbox entry not found", nil)
		return
	}
	var in ackRequest
	if !decodeBody(w, r, &in) {
		return
	}
	if in.OK == nil {
		validationError(w, "ok", "ok is required")
		return
	}
	msg := cleanAckError(in.Error)
	err = rt.deps.Outbox.AckOutbox(r.Context(), id, *in.OK, msg, rt.deps.Now().UTC())
	switch {
	case errors.Is(err, store.ErrOutboxNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "outbox entry not found", nil)
	case err != nil:
		rt.internalError(w, r, err)
	default:
		WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}
