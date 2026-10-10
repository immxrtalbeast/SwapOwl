package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

type ctxKey int

const (
	requestIDKey ctxKey = 1
	routeKey     ctxKey = 2
)

// routeInfo заполняется роутером: шаблон пути попадает в журнал вместо реального пути
// (в реальном пути могут быть id и прочие данные).
type routeInfo struct{ pattern string }

// RequestID возвращает id запроса из контекста.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func newRequestID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// validRequestID — клиентский id принимаем только короткий и безопасный для журнала.
func validRequestID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// statusWriter запоминает статус ответа.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status, w.wrote = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status, w.wrote = http.StatusOK, true
	}
	return w.ResponseWriter.Write(b)
}

// Flush нужен для SSE (/events): без него обёртка скрыла бы http.Flusher.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap позволяет http.ResponseController добраться до исходного ResponseWriter.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// wrap собирает цепочку: request id и заголовки → журнал → восстановление после паники.
func wrap(next http.Handler, log *slog.Logger, now func() time.Time) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get("X-Request-Id")
		if !validRequestID(id) {
			id = newRequestID()
		}
		rw.Header().Set("X-Request-Id", id)
		rw.Header().Set("X-Stand-Time", now().UTC().Format(time.RFC3339))
		route := &routeInfo{pattern: "unmatched"}
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		r = r.WithContext(context.WithValue(ctx, routeKey, route))

		w := &statusWriter{ResponseWriter: rw, status: http.StatusOK}
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler { //nolint:errorlint // sentinel по контракту net/http
					panic(rec)
				}
				// Детали паники — только в журнал, клиенту — общий ответ.
				log.LogAttrs(r.Context(), slog.LevelError, "panic",
					slog.String("request_id", id),
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())))
				if !w.wrote {
					WriteError(w, http.StatusInternalServerError, CodeInternal, "internal error", nil)
				}
			}
			log.LogAttrs(r.Context(), slog.LevelInfo, "request",
				slog.String("request_id", id),
				slog.String("method", r.Method),
				slog.String("path", route.pattern),
				slog.Int("status", w.status),
				slog.Duration("duration", time.Since(start)))
		}()
		next.ServeHTTP(w, r)
	})
}
