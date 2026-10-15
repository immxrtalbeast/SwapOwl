package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"umirhack/internal/auth"
)

const (
	// linkCodeTTL — сколько живёт код привязки.
	linkCodeTTL = 10 * time.Minute
	// Алфавит без похожих знаков (I, L, O, 0, 1): код переписывают с экрана на телефон. 31^8 ≈ 8,5·10^11 вариантов.
	linkCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	linkCodeLen      = 8
	// linkFailMax неудачных попыток с адреса за linkFailWindow — затем 429: код нельзя перебирать.
	linkFailMax    = 5
	linkFailWindow = 10 * time.Minute

	codeInvalidLink = "invalid_link_code"
)

// TelegramLinkState — необязательное расширение TelegramLinks: привязан ли пользователь (для экрана «Аккаунт»).
type TelegramLinkState interface {
	TelegramLinked(ctx context.Context, login string) (bool, error)
}

type linkEntry struct {
	login   string
	expires time.Time
}

// linkCodes — одноразовые коды привязки Telegram в памяти процесса. Код хранится хешем; новый код пользователя
// отзывает прежний; неудачные попытки считаются по адресу. Состояние не переживает перезапуск — код живёт 10 минут.
type linkCodes struct {
	mu      sync.Mutex
	byHash  map[string]linkEntry
	byLogin map[string]string
	fails   map[string][]time.Time
}

func newLinkCodes() *linkCodes {
	return &linkCodes{byHash: map[string]linkEntry{}, byLogin: map[string]string{}, fails: map[string][]time.Time{}}
}

func hashLinkCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// normalizeLinkCode приводит ввод человека («abcd-efgh », «ABCD EFGH») к виду хранимого кода; ok=false — не похоже на код.
func normalizeLinkCode(in string) (string, bool) {
	s := strings.ToUpper(strings.NewReplacer("-", "", " ", "", "\t", "").Replace(strings.TrimSpace(in)))
	if len(s) != linkCodeLen {
		return "", false
	}
	for _, r := range s {
		if !strings.ContainsRune(linkCodeAlphabet, r) {
			return "", false
		}
	}
	return s, true
}

func newLinkCode() (string, error) {
	b := make([]byte, linkCodeLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, linkCodeLen)
	for i, v := range b {
		out[i] = linkCodeAlphabet[int(v)%len(linkCodeAlphabet)]
	}
	return string(out), nil
}

// issue выдаёт пользователю новый код и отзывает прежний.
func (c *linkCodes) issue(login string, now time.Time) (string, error) {
	code, err := newLinkCode()
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweep(now)
	if old, ok := c.byLogin[login]; ok {
		delete(c.byHash, old)
	}
	h := hashLinkCode(code)
	c.byHash[h] = linkEntry{login: login, expires: now.Add(linkCodeTTL)}
	c.byLogin[login] = h
	return code, nil
}

// lookup возвращает логин по коду, не сжигая его: код сгорает только после успешной привязки.
func (c *linkCodes) lookup(code string, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweep(now)
	e, ok := c.byHash[hashLinkCode(code)]
	if !ok {
		return "", false
	}
	return e.login, true
}

func (c *linkCodes) consume(code string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := hashLinkCode(code)
	if e, ok := c.byHash[h]; ok {
		delete(c.byHash, h)
		if c.byLogin[e.login] == h {
			delete(c.byLogin, e.login)
		}
	}
}

// sweep удаляет просроченные коды и старые попытки; вызывается под c.mu.
func (c *linkCodes) sweep(now time.Time) {
	for h, e := range c.byHash {
		if !now.Before(e.expires) {
			delete(c.byHash, h)
			if c.byLogin[e.login] == h {
				delete(c.byLogin, e.login)
			}
		}
	}
	for ip, ts := range c.fails {
		keep := ts[:0]
		for _, t := range ts {
			if now.Sub(t) < linkFailWindow {
				keep = append(keep, t)
			}
		}
		if len(keep) == 0 {
			delete(c.fails, ip)
		} else {
			c.fails[ip] = keep
		}
	}
}

func (c *linkCodes) blocked(ip string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweep(now)
	return len(c.fails[ip]) >= linkFailMax
}

func (c *linkCodes) fail(ip string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fails[ip] = append(c.fails[ip], now)
}

func (rt *Router) linkNow() time.Time {
	if rt.deps.Auth != nil && rt.deps.Auth.Now != nil {
		return rt.deps.Auth.Now()
	}
	return time.Now()
}

// formatLinkCode — «ABCD-EFGH» для показа.
func formatLinkCode(c string) string { return c[:4] + "-" + c[4:] }

func (rt *Router) telegramLinkCodeRoutes() {
	rt.links = newLinkCodes()
	rt.HandleRole(http.MethodPost, "/api/auth/telegram-link-code", auth.Viewer, rt.issueLinkCode)
	rt.HandlePublic(http.MethodPost, "/api/auth/telegram-link", rt.linkWithCode)
	rt.HandleRole(http.MethodGet, "/api/auth/telegram-link", auth.Viewer, rt.telegramLinkStatus)
	rt.HandleRole(http.MethodDelete, "/api/auth/telegram-link", auth.Viewer, rt.telegramUnlinkSelf)
}

// webLogin — логин веб-сессии; ключи доступа и демо-режим без входа привязать к Telegram не могут.
func (rt *Router) webLogin(w http.ResponseWriter, r *http.Request) (string, bool) {
	p, ok := PrincipalFrom(r.Context())
	if !ok || p.Kind != auth.KindSession || p.Login == "" {
		WriteError(w, http.StatusForbidden, CodeForbidden, "a signed-in web session is required", nil)
		return "", false
	}
	return p.Login, true
}

// issueLinkCode — POST /api/auth/telegram-link-code (любая роль веб-сессии): одноразовый код на 10 минут.
func (rt *Router) issueLinkCode(w http.ResponseWriter, r *http.Request) {
	login, ok := rt.webLogin(w, r)
	if !ok {
		return
	}
	code, err := rt.links.issue(login, rt.linkNow())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "telegram_link_code_issued", slog.String("login", login)) // сам код в журнал не пишем
	WriteJSON(w, http.StatusOK, map[string]any{"code": formatLinkCode(code), "expires_in_seconds": int(linkCodeTTL / time.Second)})
}

// linkWithCode — POST /api/auth/telegram-link {"init_data","code"}: подпись Telegram доказывает, кто пишет, код —
// какой логин веба он привязывает. Успех: привязка, сессия, роль как в вебе; код сгорает только здесь.
func (rt *Router) linkWithCode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		InitData string `json:"init_data"`
		Code     string `json:"code"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if in.InitData == "" || len(in.InitData) > maxInitDataLen {
		validationError(w, "init_data", "init_data is required (Telegram.WebApp.initData)")
		return
	}
	svc := rt.deps.Auth
	if !svc.Required {
		WriteError(w, http.StatusConflict, codeConflict, "linking needs sign-in to be enabled", nil)
		return
	}
	now := rt.linkNow()
	ip := rescueClientIP(r)
	if rt.links.blocked(ip, now) {
		w.Header().Set("Retry-After", "600")
		WriteError(w, http.StatusTooManyRequests, CodeTooManyTries, "too many attempts, try again later", nil)
		return
	}
	tgID, err := verifyInitData(in.InitData, rt.deps.TelegramBotToken, now)
	if err != nil {
		rt.audit(r, "telegram_link_failed", slog.String("reason", err.Error()))
		msg := "invalid init_data"
		if errors.Is(err, errInitExpired) {
			msg = "init_data expired, reopen the mini-app"
		}
		WriteError(w, http.StatusUnauthorized, CodeUnauthorized, msg, nil) // код не сгорает и попытка не считается: подпись — не перебор
		return
	}
	code, ok := normalizeLinkCode(in.Code)
	login := ""
	if ok {
		login, ok = rt.links.lookup(code, now)
	}
	if !ok {
		rt.links.fail(ip, now)
		rt.audit(r, "telegram_link_failed", slog.String("reason", "bad_code"))
		WriteError(w, http.StatusBadRequest, codeInvalidLink, "the link code is invalid, expired or already used", nil)
		return
	}
	switch err := rt.deps.Telegram.LinkTelegram(r.Context(), login, tgID); {
	case err == nil:
	case errors.Is(err, auth.ErrExists):
		WriteError(w, http.StatusConflict, codeConflict, "telegram account is already linked to another user", nil)
		return
	case errors.Is(err, auth.ErrNotFound):
		rt.links.consume(code)
		WriteError(w, http.StatusBadRequest, codeInvalidLink, "the link code is invalid, expired or already used", nil)
		return
	default:
		rt.internalError(w, r, err)
		return
	}
	rt.links.consume(code)
	u, err := svc.Repo.UserByLogin(r.Context(), login)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	if err := rt.startSession(w, r, u); err != nil {
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "telegram_linked_by_code", slog.String("login", login), slog.Int64("telegram_user_id", tgID))
	WriteJSON(w, http.StatusOK, map[string]any{"login": u.Login, "role": u.Role, "auth_required": true})
}

// telegramLinkStatus — GET /api/auth/telegram-link: привязан ли текущий пользователь.
func (rt *Router) telegramLinkStatus(w http.ResponseWriter, r *http.Request) {
	login, ok := rt.webLogin(w, r)
	if !ok {
		return
	}
	linked := false
	if st, ok := rt.deps.Telegram.(TelegramLinkState); ok {
		var err error
		if linked, err = st.TelegramLinked(r.Context(), login); err != nil {
			rt.internalError(w, r, err)
			return
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"linked": linked})
}

// telegramUnlinkSelf — DELETE /api/auth/telegram-link: пользователь снимает свою привязку (повтор — тоже 204).
func (rt *Router) telegramUnlinkSelf(w http.ResponseWriter, r *http.Request) {
	login, ok := rt.webLogin(w, r)
	if !ok {
		return
	}
	switch err := rt.deps.Telegram.UnlinkTelegram(r.Context(), login); {
	case err == nil, errors.Is(err, auth.ErrNotFound):
	default:
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "telegram_unlinked_self", slog.String("login", login))
	w.WriteHeader(http.StatusNoContent)
}
