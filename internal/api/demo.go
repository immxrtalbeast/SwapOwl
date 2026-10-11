package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"umirhack/internal/clock"
)

// Коды, которых нет в 7.4.
const codeNotSimulated = "not_simulated"

// MaxDemoSpeed — верхняя граница скорости часов стенда (7.4).
const MaxDemoSpeed = 3600

// Demo — управление стендом (7.4, «Стенд»). nil в Deps.Demo — DEMO_MODE=false: все /api/demo/* дают 404.
type Demo interface {
	SetSpeed(ctx context.Context, speed float64) error
	Jump(ctx context.Context, hours float64) (time.Time, error)
	Reset(ctx context.Context) error
	Now() time.Time
	Speed() float64
}

// Gate — затвор процесса (2.8): изменяющие запросы берут его на чтение.
type Gate interface {
	RLock()
	RUnlock()
}

func (rt *Router) demoRoutes() {
	if rt.deps.Demo == nil {
		return // DEMO_MODE=false: общий обработчик отвечает 404
	}
	// Роль не требуется: доступ закрывает DEMO_TOKEN (2.8), а не сессия. Затвор эти ручки не берут:
	// сброс сам берёт его на запись.
	rt.HandlePublic(http.MethodPost, "/api/demo/clock/speed", rt.demoAuth(rt.demoSpeed))
	rt.HandlePublic(http.MethodPost, "/api/demo/clock/jump", rt.demoAuth(rt.demoJump))
	rt.HandlePublic(http.MethodPost, "/api/demo/reset", rt.demoAuth(rt.demoReset))
}

// demoAuth требует Authorization: Bearer <DEMO_TOKEN>; пустой токен в конфиге не совпадает ни с чем.
func (rt *Router) demoAuth(next http.HandlerFunc) http.HandlerFunc {
	want := sha256.Sum256([]byte(rt.deps.DemoToken))
	return func(w http.ResponseWriter, r *http.Request) {
		got := sha256.Sum256([]byte(bearerToken(r)))
		if rt.deps.DemoToken == "" || subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			WriteError(w, http.StatusUnauthorized, CodeUnauthorized, "demo token required", nil)
			return
		}
		next(w, r)
	}
}

func (rt *Router) demoState(w http.ResponseWriter) {
	d := rt.deps.Demo
	WriteJSON(w, http.StatusOK, map[string]any{
		"now": d.Now().UTC().Format(time.RFC3339Nano), "speed": d.Speed()})
}

func (rt *Router) demoFail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, clock.ErrInvalidSpeed), errors.Is(err, clock.ErrInvalidJump):
		WriteError(w, http.StatusBadRequest, CodeValidation, err.Error(), nil)
	case rt.deps.NotSimulated != nil && errors.Is(err, rt.deps.NotSimulated):
		WriteError(w, http.StatusConflict, codeNotSimulated, "clock is not simulated (CLOCK_MODE=real)", nil)
	default:
		rt.deps.Log.LogAttrs(r.Context(), slog.LevelError, "demo failed",
			slog.String("request_id", RequestID(r.Context())), slog.String("error", err.Error()))
		WriteError(w, http.StatusInternalServerError, CodeInternal, "internal error", nil)
	}
}

func (rt *Router) demoSpeed(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Speed *float64 `json:"speed"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil || in.Speed == nil {
		WriteError(w, http.StatusBadRequest, CodeValidation, "body must be {\"speed\": number}", nil)
		return
	}
	if !(*in.Speed >= 0 && *in.Speed <= MaxDemoSpeed) {
		WriteError(w, http.StatusBadRequest, CodeValidation, "speed must be in 0..3600", nil)
		return
	}
	if err := rt.deps.Demo.SetSpeed(r.Context(), *in.Speed); err != nil {
		rt.demoFail(w, r, err)
		return
	}
	rt.audit(r, "demo.speed", slog.Float64("speed", *in.Speed))
	rt.demoState(w)
}

func (rt *Router) demoJump(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Hours *float64 `json:"hours"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil || in.Hours == nil {
		WriteError(w, http.StatusBadRequest, CodeValidation, "body must be {\"hours\": number}", nil)
		return
	}
	if _, err := rt.deps.Demo.Jump(r.Context(), *in.Hours); err != nil {
		rt.demoFail(w, r, err)
		return
	}
	rt.audit(r, "demo.jump", slog.Float64("hours", *in.Hours))
	rt.demoState(w)
}

func (rt *Router) demoReset(w http.ResponseWriter, r *http.Request) {
	if err := rt.deps.Demo.Reset(r.Context()); err != nil {
		rt.demoFail(w, r, err)
		return
	}
	rt.audit(r, "demo.reset")
	rt.demoState(w)
}
