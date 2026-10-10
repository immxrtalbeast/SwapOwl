package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Pinger — то, что /healthz спрашивает у БД.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps — зависимости каркаса.
type Deps struct {
	Log *slog.Logger
	DB  Pinger
	Now func() time.Time // время стенда для X-Stand-Time
}

// Router — хаб-файл маршрутов. Следующие тикеты добавляют строки в Routes(),
// а не свои мультиплексоры. Метод проверяется здесь: чужой метод даёт 405 в общем формате.
type Router struct {
	deps   Deps
	mux    *http.ServeMux
	routes map[string]map[string]http.Handler // шаблон → метод → хендлер
}

// NewRouter собирает роутер с маршрутами каркаса.
func NewRouter(deps Deps) *Router {
	if deps.Log == nil {
		deps.Log = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	rt := &Router{deps: deps, mux: http.NewServeMux(), routes: map[string]map[string]http.Handler{}}
	rt.mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, CodeNotFound, "not found", nil)
	}))
	rt.Routes()
	return rt
}

// Routes — единственное место регистрации маршрутов.
func (rt *Router) Routes() {
	rt.Handle(http.MethodGet, "/healthz", rt.healthz)
}

// Handle регистрирует хендлер для метода и шаблона пути (синтаксис ServeMux без метода: "/api/x/{id}").
func (rt *Router) Handle(method, pattern string, h http.HandlerFunc) {
	byMethod, ok := rt.routes[pattern]
	if !ok {
		byMethod = map[string]http.Handler{}
		rt.routes[pattern] = byMethod
		rt.mux.Handle(pattern, rt.dispatch(pattern, byMethod))
	}
	byMethod[method] = h
	if method == http.MethodGet {
		byMethod[http.MethodHead] = h
	}
}

func (rt *Router) dispatch(pattern string, byMethod map[string]http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ri, ok := r.Context().Value(routeKey).(*routeInfo); ok {
			ri.pattern = pattern
		}
		h, ok := byMethod[r.Method]
		if !ok {
			allow := make([]string, 0, len(byMethod))
			for m := range byMethod {
				allow = append(allow, m)
			}
			sort.Strings(allow)
			w.Header().Set("Allow", strings.Join(allow, ", "))
			WriteError(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "method not allowed", nil)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// Handler возвращает готовый http.Handler со сквозными слоями.
func (rt *Router) Handler() http.Handler {
	return wrap(rt.mux, rt.deps.Log, rt.deps.Now)
}

func (rt *Router) healthz(w http.ResponseWriter, r *http.Request) {
	if rt.deps.DB != nil {
		if err := rt.deps.DB.Ping(r.Context()); err != nil {
			rt.deps.Log.LogAttrs(r.Context(), slog.LevelWarn, "healthz: db unavailable",
				slog.String("request_id", RequestID(r.Context())), slog.String("error", err.Error()))
			WriteError(w, http.StatusServiceUnavailable, CodeOverloaded, "database unavailable", nil)
			return
		}
	}
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
