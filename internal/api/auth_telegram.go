package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"umirhack/internal/auth"
)

const (
	// telegramMaxAge — насколько старой может быть auth_date (FS-163).
	telegramMaxAge = 24 * time.Hour
	// telegramSkew — допуск на расхождение часов, если auth_date «из будущего».
	telegramSkew    = 5 * time.Minute
	maxInitDataLen  = 8192
	codeTgNotLinked = "telegram_not_linked"
)

// TelegramLinks — привязки Telegram-аккаунтов к пользователям; реализует internal/store.
type TelegramLinks interface {
	// TelegramLogin возвращает логин привязанного пользователя или auth.ErrNotFound.
	TelegramLogin(ctx context.Context, telegramUserID int64) (string, error)
	// LinkTelegram заменяет привязку пользователя; auth.ErrNotFound — нет пользователя,
	// auth.ErrExists — аккаунт уже привязан к другому.
	LinkTelegram(ctx context.Context, login string, telegramUserID int64) error
	// UnlinkTelegram снимает привязку; auth.ErrNotFound — её не было.
	UnlinkTelegram(ctx context.Context, login string) error
}

// telegramRoutes регистрирует вход через Telegram и привязку аккаунтов. Без сервиса auth,
// хранилища привязок или TELEGRAM_BOT_TOKEN маршрутов нет.
func (rt *Router) telegramRoutes() {
	if rt.deps.Auth == nil || rt.deps.Telegram == nil || rt.deps.TelegramBotToken == "" {
		return
	}
	rt.HandlePublic(http.MethodPost, "/api/auth/telegram", rt.telegramLogin)
	rt.HandleRole(http.MethodPut, "/api/users/{login}/telegram", auth.Admin, rt.linkTelegram)
	rt.HandleRole(http.MethodDelete, "/api/users/{login}/telegram", auth.Admin, rt.unlinkTelegram)
}

var (
	errInitBad     = errors.New("telegram: bad signature")
	errInitExpired = errors.New("telegram: auth_date is too old")
)

// verifyInitData проверяет Telegram.WebApp.initData по документации Telegram:
// secret = HMAC_SHA256(key="WebAppData", msg=botToken), hash = hex(HMAC_SHA256(secret, data_check_string)).
// Возвращает id пользователя Telegram. now — часы API.
func verifyInitData(initData, botToken string, now time.Time) (int64, error) {
	vals, err := url.ParseQuery(initData)
	if err != nil {
		return 0, errInitBad
	}
	got, err := hex.DecodeString(vals.Get("hash"))
	if err != nil || len(got) != sha256.Size {
		return 0, errInitBad
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		if k != "hash" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + vals.Get(k)
	}
	secret := hmacSHA256([]byte("WebAppData"), []byte(botToken))
	want := hmacSHA256(secret, []byte(strings.Join(lines, "\n")))
	if !hmac.Equal(got, want) { // постоянное время
		return 0, errInitBad
	}
	sec, err := strconv.ParseInt(vals.Get("auth_date"), 10, 64)
	if err != nil || sec <= 0 {
		return 0, errInitBad
	}
	at := time.Unix(sec, 0)
	if now.Sub(at) > telegramMaxAge {
		return 0, errInitExpired
	}
	if at.Sub(now) > telegramSkew {
		return 0, errInitBad
	}
	var u struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal([]byte(vals.Get("user")), &u) != nil || u.ID <= 0 {
		return 0, errInitBad
	}
	return u.ID, nil
}

func hmacSHA256(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}

func (rt *Router) telegramLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		InitData string `json:"init_data"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if in.InitData == "" || len(in.InitData) > maxInitDataLen {
		validationError(w, "init_data", "init_data is required (Telegram.WebApp.initData)")
		return
	}
	tgID, err := verifyInitData(in.InitData, rt.deps.TelegramBotToken, rt.deps.Now())
	if err != nil {
		// В журнал идёт только причина, ни init_data, ни токен.
		rt.audit(r, "telegram_login_failed", slog.String("reason", err.Error()))
		msg := "invalid init_data"
		if errors.Is(err, errInitExpired) {
			msg = "init_data expired, reopen the mini-app"
		}
		WriteError(w, http.StatusUnauthorized, CodeUnauthorized, msg, nil)
		return
	}
	svc := rt.deps.Auth
	if !svc.Required {
		// Демо-стенд: как гость-оператор, без сессии (подпись проверена выше).
		WriteJSON(w, http.StatusOK, map[string]any{"login": nil, "role": auth.Operator, "auth_required": false})
		return
	}
	login, err := rt.deps.Telegram.TelegramLogin(r.Context(), tgID)
	if errors.Is(err, auth.ErrNotFound) {
		rt.audit(r, "telegram_login_unlinked", slog.Int64("telegram_user_id", tgID))
		WriteError(w, http.StatusForbidden, codeTgNotLinked, "telegram account is not linked to a user",
			map[string]any{"telegram_user_id": tgID})
		return
	}
	var u auth.User
	if err == nil {
		u, err = svc.Repo.UserByLogin(r.Context(), login)
	}
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	token, err := auth.NewToken()
	if err == nil {
		err = svc.Repo.CreateSession(r.Context(), auth.HashToken(token), u.ID, svc.Now().Add(svc.SessionTTL))
	}
	if err != nil {
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
	if ai, ok := r.Context().Value(authInfoKey).(*authInfo); ok {
		ai.set(auth.Principal{Kind: auth.KindSession, Role: u.Role, UserID: u.ID, Login: u.Login})
	}
	rt.audit(r, "telegram_login_ok", slog.String("login", u.Login), slog.Int64("telegram_user_id", tgID))
	WriteJSON(w, http.StatusOK, map[string]any{"login": u.Login, "role": u.Role, "auth_required": true})
}

func (rt *Router) linkTelegram(w http.ResponseWriter, r *http.Request) {
	login := r.PathValue("login")
	var in struct {
		TelegramUserID int64 `json:"telegram_user_id"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if in.TelegramUserID <= 0 {
		validationError(w, "telegram_user_id", "telegram_user_id must be a positive integer")
		return
	}
	switch err := rt.deps.Telegram.LinkTelegram(r.Context(), login, in.TelegramUserID); {
	case err == nil:
	case errors.Is(err, auth.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "user not found", nil)
		return
	case errors.Is(err, auth.ErrExists):
		WriteError(w, http.StatusConflict, codeConflict, "telegram account is already linked to another user", nil)
		return
	default:
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "telegram_linked", slog.String("target_login", login), slog.Int64("telegram_user_id", in.TelegramUserID))
	WriteJSON(w, http.StatusOK, map[string]any{"login": login, "telegram_user_id": in.TelegramUserID})
}

func (rt *Router) unlinkTelegram(w http.ResponseWriter, r *http.Request) {
	login := r.PathValue("login")
	switch err := rt.deps.Telegram.UnlinkTelegram(r.Context(), login); {
	case err == nil:
	case errors.Is(err, auth.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "telegram link not found", nil)
		return
	default:
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "telegram_unlinked", slog.String("target_login", login))
	w.WriteHeader(http.StatusNoContent)
}
