package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"umirhack/internal/auth"
	"umirhack/internal/events"
	"umirhack/internal/metrics"
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

	// Auth — вход и роли (2.9). nil — доступ не настроен: все запросы идут с ролью operator,
	// как при AUTH_REQUIRED=false, а маршруты /api/auth, /api/users и /api/api-keys не регистрируются.
	Auth *auth.Service

	// Audit — журнал действий в БД (2.9, 8.3 п. 6). nil — записи идут только в журнал запросов,
	// а GET /api/audit не регистрируется.
	Audit AuditLog

	// Stock — поставки и остатки (2.1). nil — маршруты склада не регистрируются.
	Stock Stock
	// Receiving — приёмка: подтверждение срока партии и список неподтверждённых (2.1а).
	// nil — маршруты не регистрируются.
	Receiving Receiving
	// Catalog — импорт справочников (8.3 п. 5). nil — маршрут не регистрируется.
	Catalog Catalog
	// Orders — заказы и меню (2.3). nil — маршруты заказов не регистрируются.
	Orders Orders
	// Corrections — списание брака, инвентаризация, отмена заказа. nil — маршруты не регистрируются.
	Corrections Corrections
	// Menu — стоп-лист и порции (2.5). nil — маршруты не регистрируются.
	Menu MenuSource
	// HotOffers — снимок для горячих предложений (2.7). nil — маршрут не регистрируется.
	HotOffers HotOffersSource
	// HotConfig — основной алгоритм и параметры R/Bg.
	HotConfig HotOffersConfig
	// Supply — предложение поставок (2.6). nil — маршрут не регистрируется.
	Supply SupplySource
	// SupplyOrders — заказ поставщику через outbox (FS-121). nil — маршруты не регистрируются.
	SupplyOrders SupplyOrders
	// Outbox — очередь исходящих сообщений для адаптера (8.0а). nil — маршруты не регистрируются.
	Outbox Outbox
	// Analytics — деньги: списания и расхождения кассы (7.4). nil — маршруты не регистрируются.
	Analytics Analytics
	// Location — часовой пояс ресторана для expires_on; nil — UTC.
	Location *time.Location
	// Monitor — лента предупреждений и журнал движений (2.6). nil — маршруты не регистрируются.
	Monitor Monitor
	// Events — шина событий для GET /events (7.5). nil — маршрут не регистрируется.
	Events *events.Bus
	// PingEvery — период ping в SSE; 0 — 15 с. Нужен тестам.
	PingEvery time.Duration
	// Clock — скорость и признаки симуляции для GET /api/clock; nil — реальные часы.
	Clock ClockInfo
	// Demo — управление стендом (2.8). nil — DEMO_MODE=false: /api/demo/* отвечают 404.
	Demo Demo
	// Sim — запуск и пауза генератора заказов; nil — ручка /api/demo/simulator не регистрируется.
	Sim SimControl
	// DemoToken — значение DEMO_TOKEN; пустой не открывает ничего.
	DemoToken string
	// NotSimulated — ошибка Demo «часы не ускоренные»; Router отвечает на неё 409.
	NotSimulated error
	// Gate — затвор процесса (2.8): изменяющие запросы (кроме /api/demo/*) берут его на чтение.
	// nil — без затвора.
	Gate Gate
	// Metrics — реестр для GET /metrics (8.0а) и учёта запросов в общем звене. nil — метрик нет.
	Metrics *metrics.Registry
	// MetricsToken — значение METRICS_TOKEN; пустой — /metrics отвечает 404.
	MetricsToken string
	// Invoices — распознавание фото накладной (FS-158). nil — маршрут не регистрируется.
	Invoices *Invoices
}

// Router — хаб-файл маршрутов. Следующие тикеты добавляют строки в Routes(),
// а не свои мультиплексоры. Метод проверяется здесь: чужой метод даёт 405 в общем формате.
type Router struct {
	deps   Deps
	mux    *http.ServeMux
	routes map[string]map[string]http.Handler // шаблон → метод → хендлер
	access map[string]map[string]access       // шаблон → метод → требуемый доступ
}

// NewRouter собирает роутер с маршрутами каркаса.
func NewRouter(deps Deps) *Router {
	if deps.Log == nil {
		deps.Log = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	// Запись журнала запроса получает роль и id пользователя или ключа (см. authLogHandler).
	deps.Log = slog.New(authLogHandler{deps.Log.Handler()})
	rt := &Router{deps: deps, mux: http.NewServeMux(),
		routes: map[string]map[string]http.Handler{}, access: map[string]map[string]access{}}
	rt.mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, CodeNotFound, "not found", nil)
	}))
	rt.Routes()
	return rt
}

// Routes — единственное место регистрации маршрутов.
func (rt *Router) Routes() {
	rt.HandlePublic(http.MethodGet, "/healthz", rt.healthz)
	rt.metricsRoutes()
	rt.authRoutes()
	rt.stockRoutes()
	rt.receivingRoutes()
	rt.invoiceRoutes()
	rt.catalogRoutes()
	rt.orderRoutes()
	rt.correctionRoutes()
	rt.openedRoutes()
	rt.stopListRoutes()
	rt.monitorRoutes()
	rt.hotOffersRoutes()
	rt.forecastRoutes()
	rt.supplyRoutes()
	rt.supplyOrderRoutes()
	rt.analyticsRoutes()
	rt.eventsRoutes()
	rt.outboxRoutes()
	rt.clockRoutes()
	rt.demoRoutes()
	rt.demoSimRoutes()
	rt.openAPIRoutes()
}

// Handle регистрирует хендлер для метода и шаблона пути (синтаксис ServeMux без метода: "/api/x/{id}").
// Доступ по умолчанию (2.9): чтение — viewer, всё изменяющее — operator. Иное требование — HandleRole.
func (rt *Router) Handle(method, pattern string, h http.HandlerFunc) {
	min := auth.Operator
	if method == http.MethodGet || method == http.MethodHead {
		min = auth.Viewer
	}
	rt.register(method, pattern, h, access{min: min})
}

// HandleRole — как Handle, но с явно заданной минимальной ролью (проверка — звено роутера, не хендлера).
func (rt *Router) HandleRole(method, pattern string, min auth.Role, h http.HandlerFunc) {
	rt.register(method, pattern, h, access{min: min})
}

// HandlePublic регистрирует маршрут без входа (2.9): /healthz, POST /api/auth/login, демо-ручки под
// DEMO_TOKEN (demoAuth) и статичное описание API (/api/openapi.yaml, /api/docs).
func (rt *Router) HandlePublic(method, pattern string, h http.HandlerFunc) {
	rt.register(method, pattern, h, access{public: true})
}

func (rt *Router) register(method, pattern string, h http.HandlerFunc, acc access) {
	if rt.access[pattern] == nil {
		rt.access[pattern] = map[string]access{}
	}
	rt.access[pattern][method] = acc
	if method == http.MethodGet {
		rt.access[pattern][http.MethodHead] = acc
	}
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
		r, ok = rt.guard(w, r, rt.access[pattern][r.Method])
		if !ok {
			return
		}
		// Затвор (2.8): сброс стенда ждёт завершения изменяющих запросов, новые ждут сброса.
		// /api/demo/* затвор не берут — сброс сам берёт его на запись.
		if g := rt.deps.Gate; g != nil && isMutating(r.Method) && !strings.HasPrefix(pattern, "/api/demo/") {
			g.RLock()
			defer g.RUnlock()
		}
		h.ServeHTTP(w, r)
	})
}

// Handler возвращает готовый http.Handler со сквозными слоями.
func (rt *Router) Handler() http.Handler {
	return withAuthInfo(wrap(rt.mux, rt.deps.Log, rt.deps.Now, rt.deps.Metrics))
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
