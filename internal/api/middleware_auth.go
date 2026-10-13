package api

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"umirhack/internal/audit"
	"umirhack/internal/auth"
)

const sessionCookie = "session"

// access — требование к маршруту: открыт всем или нужна минимальная роль.
type access struct {
	public bool
	min    auth.Role
}

const principalKey ctxKey = 3
const authInfoKey ctxKey = 4

// PrincipalFrom возвращает того, кто делает запрос. Для маршрутов под guard он всегда есть
// (при AUTH_REQUIRED=false — анонимный operator); для открытых маршрутов — нет.
func PrincipalFrom(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(principalKey).(auth.Principal)
	return p, ok
}

// authInfo передаёт роль и id из звена доступа в запись журнала запроса: запись пишет wrap,
// который стоит снаружи и контекста внутреннего запроса не видит.
type authInfo struct {
	mu sync.Mutex
	p  *auth.Principal
}

func (a *authInfo) set(p auth.Principal) {
	a.mu.Lock()
	a.p = &p
	a.mu.Unlock()
}

func (a *authInfo) get() (auth.Principal, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.p == nil {
		return auth.Principal{}, false
	}
	return *a.p, true
}

func withAuthInfo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), authInfoKey, &authInfo{})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// authLogHandler дописывает к каждой записи журнала роль и id пользователя или ключа.
type authLogHandler struct{ slog.Handler }

func (h authLogHandler) Handle(ctx context.Context, rec slog.Record) error {
	if ai, ok := ctx.Value(authInfoKey).(*authInfo); ok {
		if p, ok := ai.get(); ok {
			rec.AddAttrs(slog.String("role", string(p.Role)), slog.String("auth", string(p.Kind)))
			switch p.Kind {
			case auth.KindSession:
				rec.AddAttrs(slog.Int64("user_id", p.UserID))
			case auth.KindAPIKey:
				rec.AddAttrs(slog.Int64("api_key_id", p.KeyID))
			}
		}
	}
	return h.Handler.Handle(ctx, rec)
}

func (h authLogHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.Handler.Enabled(ctx, l)
}
func (h authLogHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return authLogHandler{h.Handler.WithAttrs(a)}
}
func (h authLogHandler) WithGroup(n string) slog.Handler {
	return authLogHandler{h.Handler.WithGroup(n)}
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func isJSON(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// guard — единое звено доступа: вход (401), роль (403), тип содержимого изменяющих запросов (400).
// Возвращает запрос с принципалом в контексте; false — ответ уже записан.
func (rt *Router) guard(w http.ResponseWriter, r *http.Request, acc access) (*http.Request, bool) {
	if !acc.public {
		p, ok := rt.authenticate(w, r)
		if !ok {
			return r, false
		}
		if ai, ok := r.Context().Value(authInfoKey).(*authInfo); ok {
			ai.set(p)
		}
		if !p.Role.Allows(acc.min) {
			WriteError(w, http.StatusForbidden, CodeForbidden, "недостаточно прав", nil)
			return r, false
		}
		r = r.WithContext(context.WithValue(r.Context(), principalKey, p))
	}
	// Ручки /api/demo/* без тела (сброс) принимают запрос без Content-Type: curl -X POST -H 'Authorization: Bearer …'.
	emptyDemo := strings.HasPrefix(r.URL.Path, "/api/demo/") && r.ContentLength == 0
	if isMutating(r.Method) && !isJSON(r) && !emptyDemo {
		WriteError(w, http.StatusBadRequest, CodeValidation, "заголовок Content-Type должен быть application/json", nil)
		return r, false
	}
	return r, true
}

// authenticate определяет принципала. При AUTH_REQUIRED=false запрос без действительных
// учётных данных идёт как анонимный operator (так же работает токен демо-ручек, который
// не является ключом); при true — 401.
func (rt *Router) authenticate(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	svc := rt.deps.Auth
	if svc == nil {
		return auth.Anonymous(), true
	}
	var token string
	if c, err := r.Cookie(sessionCookie); err == nil {
		token = c.Value
	}
	p, err := svc.Authenticate(r.Context(), token, bearerToken(r))
	switch {
	case err == nil:
		return p, true
	case errors.Is(err, auth.ErrNoCredentials) || errors.Is(err, auth.ErrInvalidCredentials):
		if !svc.Required {
			return auth.Anonymous(), true
		}
		WriteError(w, http.StatusUnauthorized, CodeUnauthorized, "authentication required", nil)
	default:
		rt.deps.Log.LogAttrs(r.Context(), slog.LevelError, "authenticate failed",
			slog.String("request_id", RequestID(r.Context())), slog.String("error", err.Error()))
		WriteError(w, http.StatusInternalServerError, CodeInternal, "internal error", nil)
	}
	return auth.Principal{}, false
}

// auditSkip — события, которые остаются только в журнале запросов: открытие и закрытие SSE
// происходят при каждом переподключении клиента и забили бы таблицу.
var auditSkip = map[string]bool{"events_open": true, "events_close": true}

// audit пишет запись аудита (2.9) в журнал запросов и, кроме auditSkip, в таблицу audit_log.
// Секреты в attrs не передаются. Сбой записи в БД не ломает запрос: он попадает в журнал ошибок.
func (rt *Router) audit(r *http.Request, event string, attrs ...slog.Attr) {
	all := append([]slog.Attr{
		slog.Bool("audit", true),
		slog.String("event", event),
		slog.String("request_id", RequestID(r.Context())),
	}, attrs...)
	rt.deps.Log.LogAttrs(r.Context(), slog.LevelInfo, "audit", all...)
	if rt.deps.Audit == nil || auditSkip[event] {
		return
	}
	details := make(map[string]any, len(attrs)+1)
	for _, a := range attrs {
		details[a.Key] = a.Value.Resolve().Any()
	}
	details["request_id"] = RequestID(r.Context())
	if err := audit.Record(r.Context(), rt.deps.Audit, rt.actor(r, attrs), event, details); err != nil {
		rt.deps.Log.LogAttrs(r.Context(), slog.LevelError, "audit insert failed",
			slog.String("request_id", RequestID(r.Context())), slog.String("event", event), slog.String("error", err.Error()))
	}
}

// actor — кто совершил действие: user:<login>, key:<id>, либо login:<попытка> для событий
// входа без сессии, иначе anonymous.
func (rt *Router) actor(r *http.Request, attrs []slog.Attr) string {
	p, ok := PrincipalFrom(r.Context())
	if !ok {
		if ai, k := r.Context().Value(authInfoKey).(*authInfo); k {
			p, ok = ai.get()
		}
	}
	if ok {
		switch p.Kind {
		case auth.KindSession:
			return "user:" + p.Login
		case auth.KindAPIKey:
			return "key:" + strconv.FormatInt(p.KeyID, 10)
		}
	}
	for _, a := range attrs {
		if a.Key == "login" {
			return "login:" + a.Value.String()
		}
	}
	return "anonymous"
}
