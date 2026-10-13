package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"
	"unicode"

	"umirhack/internal/auth"
)

const (
	maxBody      = 1 << 20
	maxLoginLen  = 128
	minPassBytes = 8
	maxPassBytes = 72 // предел bcrypt
)

// authRoutes регистрирует маршруты доступа (7.4); без сервиса auth их нет.
func (rt *Router) authRoutes() {
	if rt.deps.Auth == nil {
		return
	}
	rt.HandlePublic(http.MethodPost, "/api/auth/login", rt.login)
	rt.HandleRole(http.MethodPost, "/api/auth/logout", auth.Viewer, rt.logout)
	rt.HandleRole(http.MethodGet, "/api/auth/me", auth.Viewer, rt.me)
	rt.HandleRole(http.MethodPost, "/api/auth/password", auth.Viewer, rt.changePassword)
	rt.auditRoutes()

	rt.HandleRole(http.MethodGet, "/api/users", auth.Admin, rt.listUsers)
	rt.HandleRole(http.MethodPost, "/api/users", auth.Admin, rt.createUser)
	rt.HandleRole(http.MethodDelete, "/api/users/{id}", auth.Admin, rt.deleteUser)
	rt.HandleRole(http.MethodGet, "/api/api-keys", auth.Admin, rt.listAPIKeys)
	rt.HandleRole(http.MethodPost, "/api/api-keys", auth.Admin, rt.createAPIKey)
	rt.HandleRole(http.MethodDelete, "/api/api-keys/{id}", auth.Admin, rt.deleteAPIKey)
}

// decodeBody читает JSON-тело без неизвестных полей; при ошибке пишет 400 и возвращает false.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) && te.Field != "" {
			validationError(w, te.Field, typeErrorMessage(te))
			return false
		}
		WriteError(w, http.StatusBadRequest, CodeValidation, "некорректное тело запроса: ожидается JSON-объект", nil)
		return false
	}
	if dec.More() {
		WriteError(w, http.StatusBadRequest, CodeValidation, "некорректное тело запроса: после JSON-объекта есть лишние данные", nil)
		return false
	}
	return true
}

// typeErrorMessage называет поле и ожидаемый вид значения, когда в JSON пришёл не тот тип
// (дробные порции, число вместо строки в quantity, строка вместо целого в qty заказа поставщику).
// Вид берётся из типа поля в Go, имя — только для порций.
func typeErrorMessage(te *json.UnmarshalTypeError) string {
	name := te.Field
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	if name == "portions" {
		return "portions — целое число от 1"
	}
	kind := reflect.Invalid
	if te.Type != nil {
		t := te.Type
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		kind = t.Kind()
	}
	switch kind {
	case reflect.String:
		return name + ` — строка с числом, например "1.5"`
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return name + " — целое число, например 5"
	}
	return name + " — значение неверного типа (получено " + te.Value + ")"
}

func validationError(w http.ResponseWriter, field, msg string) {
	WriteError(w, http.StatusBadRequest, CodeValidation, msg, map[string]any{"field": field})
}

// validLabel — непустая строка без управляющих символов (логин, имя ключа).
func validLabel(s string) bool {
	if s == "" || len(s) > maxLoginLen || strings.TrimSpace(s) != s {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

func (rt *Router) internalError(w http.ResponseWriter, r *http.Request, err error) {
	rt.deps.Log.LogAttrs(r.Context(), slog.LevelError, "handler failed",
		slog.String("request_id", RequestID(r.Context())), slog.String("error", err.Error()))
	WriteError(w, http.StatusInternalServerError, CodeInternal, "internal error", nil)
}

func (rt *Router) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if in.Login == "" || len(in.Login) > maxLoginLen || in.Password == "" {
		validationError(w, "login", "login and password are required")
		return
	}
	svc := rt.deps.Auth
	token, u, err := svc.Login(r.Context(), in.Login, in.Password)
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrTooManyAttempts):
		rt.audit(r, "login_blocked", slog.String("login", in.Login))
		WriteError(w, http.StatusTooManyRequests, CodeTooManyTries, "too many failed attempts, try later", nil)
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		rt.audit(r, "login_failed", slog.String("login", in.Login))
		WriteError(w, http.StatusUnauthorized, CodeUnauthorized, "invalid login or password", nil)
		return
	default:
		rt.internalError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  svc.Now().Add(svc.SessionTTL),
		MaxAge:   int(svc.SessionTTL / time.Second),
		HttpOnly: true,
		Secure:   svc.SecureCookie,
		SameSite: http.SameSiteStrictMode,
	})
	p := auth.Principal{Kind: auth.KindSession, Role: u.Role, UserID: u.ID, Login: u.Login}
	if ai, ok := r.Context().Value(authInfoKey).(*authInfo); ok {
		ai.set(p)
	}
	rt.audit(r, "login_ok", slog.String("login", u.Login))
	WriteJSON(w, http.StatusOK, map[string]any{"login": u.Login, "role": u.Role})
}

func (rt *Router) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if err := rt.deps.Auth.Logout(r.Context(), c.Value); err != nil {
			rt.internalError(w, r, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: rt.deps.Auth.SecureCookie, SameSite: http.SameSiteStrictMode,
	})
	rt.audit(r, "logout")
	w.WriteHeader(http.StatusNoContent)
}

func (rt *Router) me(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var login any
	if p.Kind == auth.KindSession {
		login = p.Login
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"login": login, "role": p.Role, "auth_required": rt.deps.Auth.Required,
	})
}

func (rt *Router) changePassword(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	if p.Kind != auth.KindSession {
		// Анонимный operator стенда и ключ интеграции своего пароля не имеют.
		WriteError(w, http.StatusForbidden, CodeForbidden, "password can be changed only by a signed-in user", nil)
		return
	}
	var in struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	switch {
	case in.Old == "":
		validationError(w, "old", "old password is required")
		return
	case len(in.New) < minPassBytes || len(in.New) > maxPassBytes:
		validationError(w, "new", "new password must be 8..72 bytes")
		return
	case in.New == in.Old:
		validationError(w, "new", "new password must differ from the old one")
		return
	}
	var token string
	if c, err := r.Cookie(sessionCookie); err == nil {
		token = c.Value
	}
	err := rt.deps.Auth.ChangePassword(r.Context(), p.UserID, token, in.Old, in.New)
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrTooManyAttempts):
		rt.audit(r, "password_change_blocked")
		WriteError(w, http.StatusTooManyRequests, CodeTooManyTries, "too many failed attempts, try later", nil)
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		rt.audit(r, "password_change_failed")
		WriteError(w, http.StatusUnauthorized, CodeUnauthorized, "invalid old password", nil)
		return
	case errors.Is(err, auth.ErrNotFound):
		WriteError(w, http.StatusUnauthorized, CodeUnauthorized, "authentication required", nil)
		return
	default:
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "password_changed")
	w.WriteHeader(http.StatusNoContent)
}
