# HTTP API и события SSE

Источник: `.ai/solution.md`, разделы 7.4 и 7.5. Контракт заморожен: изменение — только отдельным тикетом с меткой `core`.

## Общее

- JSON, UTF-8. Каждый ответ несёт заголовки `X-Stand-Time` (RFC 3339, время стенда) и `X-Request-Id` (принимается от клиента или создаётся).
- Все моменты — RFC 3339. Количество в ответах: `{"qty": 1500, "unit": "g"}`.
- Количество на входе — только JSON-строка `^[0-9]+(\.[0-9]+)?$` и `unit` ∈ `g, kg, ml, l, pcs`. JSON-число — `400`.
- Изменяющие запросы принимаются только с `Content-Type: application/json`.
- Доступ — по ролям `viewer` < `operator` < `admin`. Сессия: cookie `session`; интеграции: `Authorization: Bearer <ключ>`. При `AUTH_REQUIRED=false` запрос без учётных данных работает с ролью `operator`.

Формат ошибки:

```json
{"error": {"code": "validation_error", "message": "…", "details": {}}}
```

| Код | HTTP |
|---|---|
| `validation_error`, `unknown_product`, `unknown_dish` | 400 |
| `unauthorized` | 401 |
| `forbidden` | 403 |
| `delivery_conflict`, `order_conflict`, `insufficient_stock` | 409 |
| `too_many_attempts` | 429 |
| `internal` | 500 |
| `overloaded` | 503 |

## Поставки

### `POST /api/deliveries` (operator)

```bash
curl -X POST http://localhost:8080/api/deliveries \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "D-2026-0042",
    "number": "42",
    "created_at": "2026-10-09T08:00:00+03:00",
    "items": [
      {"product_id": "salmon", "name": "Лосось", "quantity": "4", "unit": "kg", "shelf_life_hours": 72},
      {"product_id": "cream", "name": "Сливки", "quantity": "600", "expires_on": "2026-10-12"}
    ]
  }'
```

В позиции ровно одно из `shelf_life_hours` (целое, 1…87600) и `expires_on` (`YYYY-MM-DD`). `unit` обязателен только для неизвестного продукта.

- `201` — принята:
  ```json
  {"id": "D-2026-0042", "number": "42", "created_at": "…", "received_at": "…", "replayed": false,
   "batches": [{"id": 17, "line_no": 1, "product_id": "salmon", "quantity": {"qty": 4000, "unit": "g"},
                "received_at": "…", "expires_at": "…", "status": "ok"}],
   "warnings": [{"line_no": 2, "code": "expired_on_arrival", "message": "…"}]}
  ```
- `200` — тот же id и то же содержимое: сохранённый ответ с `"replayed": true`.
- `409 delivery_conflict` — тот же id, другое содержимое.
- `400` — ошибка валидации; ничего не записано.

### `GET /api/deliveries`, `GET /api/deliveries/{id}` (viewer)

История поставок, новые сверху; одна поставка.

## Склад и мониторинг (viewer)

- `GET /api/products` → `[{"id", "name", "unit", "available": Qty, "expiring": Qty, "batches_count", "nearest_expires_at"}]`
- `GET /api/batches?product_id=&status=` → `[{"id", "product_id", "delivery_id", "delivery_created_at", "quantity", "remaining", "received_at", "expires_at", "time_left_seconds", "status"}]`. По умолчанию без списанных в ноль, порядок `(expires_at, id)`. `status` ∈ `ok | expiring | expired`.
- `GET /api/alerts` → `[{"batch_id", "product_id", "product_name", "remaining", "expires_at", "time_left_seconds", "status", "doomed": bool}]` — истекающие партии и просроченные за последние 24 ч стенда.
- `GET /api/movements?product_id=&kind=&limit=` → журнал движений (`receipt`, `sale`, `writeoff`; для `writeoff` `reason` = `expired`).
- `GET /api/supply-suggestions` → `[{"product_id", "product_name", "quantity": Qty, "usable_stock": Qty, "rate_per_day": Qty, "stockout_at" (null), "blocks_dishes": int, "urgency": "now"|"soon"|"planned", "explanation": "…"}]`. Порядок: `blocks_dishes > 0` первыми, затем по `stockout_at`, затем по id.

## Меню и стоп-лист (viewer)

- `GET /api/dishes` → `[{"id", "name", "available": bool, "servings": int, "missing": [{"product_id", "product_name", "required": Qty, "available": Qty, "missing": Qty}], "recipe": [{"product_id", "quantity": Qty}]}]`
- `GET /api/stop-list` → те же объекты, только `available: false`.

## Заказы

### `POST /api/orders` (operator)

```bash
curl -X POST http://localhost:8080/api/orders \
  -H 'Content-Type: application/json' \
  -d '{"id": "5b0c0c3e-8a51-4d0e-9d0b-1f2a3b4c5d6e", "items": [{"dish_id": "salmon-pasta", "portions": 2}]}'
```

`id` — UUID от клиента, обязателен. `portions` — целое 1…10000. Одно блюдо — не более одной строки.

- `201` — `{"id", "status": "accepted", "at", "replayed": false, "writeoffs": [{"product_id", "batch_id", "qty": Qty, "expires_at"}]}`
- `409 insufficient_stock` — `details: {"order_id", "status": "rejected", "at", "missing": [{"product_id", "product_name", "required", "available", "missing"}]}`; заказ сохранён как отклонённый.
- Повтор с тем же id и телом: принятый — `200` и то же тело с `"replayed": true`; отклонённый — `409 insufficient_stock` и то же тело с `"replayed": true`. Тот же id, другое тело — `409 order_conflict`.

### `GET /api/orders?status=` (viewer)

История; отклонённые отделены полем `status` (`accepted` / `rejected`).

## Горячие предложения

### `GET /api/hot-offers?algo=` (viewer)

`algo` — `a` или `b`, по умолчанию из `HOTOFFERS_ALGO`.

```json
{
  "algo": "b",
  "offers": [{
    "dish_id": "salmon-steak", "dish_name": "Стейк из лосося", "priority": 412.5,
    "target_portions": 7, "usual_portions": 3, "sell_until": "2026-10-09T16:00:00+03:00",
    "reasons": [{"product_id": "salmon", "product_name": "Лосось", "batch_id": 17,
                 "at_risk": {"qty": 1400, "unit": "g"}, "expires_at": "…", "working_hours_left": 5.0}],
    "explanation": "Лосось, партия №17: 1,4 кг не успеет разойтись за 5 рабочих часов. 7 порций спасут весь остаток."
  }],
  "excluded": [{"dish_id": "caprese", "product_id": "tomato", "why": "Помидоров осталось на 5 рабочих часов"}]
}
```

`usual_portions` — сколько порций блюда ожидается при обычном спросе до `sell_until`. У алгоритма `a` `target_portions` равен 0.

## Аналитика (viewer)

- `GET /api/analytics/writeoffs?from=&to=` → по продуктам и причинам: количество и стоимость.
- `GET /api/analytics/shortages?from=&to=` → продукты по числу отклонённых заказов и недостающему количеству.
- `GET /api/analytics/bench` → `{"available": bool, "runs": 200, "main_algo": "b", "waste_reduction_vs_random_pct": 39.0, "waste_reduction_vs_nearest_pct": 27.0}`; `available: false`, если результата bench нет.

## Стенд

- `GET /api/clock` → `{"now", "speed", "timezone", "simulated": bool, "simulator_running": bool}` — открыт всем.
- С токеном (`Authorization: Bearer <DEMO_TOKEN>`, иначе `401`):
  - `POST /api/demo/clock/speed` `{"speed": 600}` (0…3600);
  - `POST /api/demo/clock/jump` `{"hours": 6}` (`0 < hours ≤ 720`);
  - `POST /api/demo/reset`.
- Без токена: `POST /api/demo/simulator` `{"running": true}`; `POST /api/demo/burst` `{"dish_id": "salmon-pasta", "orders": 10}` (1…50 параллельных заказов по одной порции).
- При `DEMO_MODE=false` все `/api/demo/*` отвечают `404`.
- `GET /healthz` → `200`, когда БД доступна и миграции применены. Открыт без входа.

## Доступ

- `POST /api/auth/login` `{"login", "password"}` → `200` `{"login", "role"}` и cookie `session` (`HttpOnly`, `SameSite=Strict`, `Secure`); `401` — неверные данные (ответ одинаков для неверного логина и пароля); `429 too_many_attempts` — больше 5 неудачных попыток на один `login` за 15 минут.
- `POST /api/auth/logout` → `204`, сессия удалена.
- `GET /api/auth/me` → `{"login", "role", "auth_required": bool}`; при `AUTH_REQUIRED=false` без учётных данных — `{"login": null, "role": "operator", "auth_required": false}`.
- Только `admin`:
  - `GET /api/users`, `POST /api/users` `{"login", "password", "role"}`, `DELETE /api/users/{id}` (последнего `admin` удалить нельзя — `409`);
  - `GET /api/api-keys`, `POST /api/api-keys` `{"name", "role"}` → `201` с полем `key` (показывается один раз), `DELETE /api/api-keys/{id}`.

При `AUTH_REQUIRED=true` любой запрос к `/api/*` и `/events` без действительной сессии или ключа — `401 unauthorized`; исключения — `GET /healthz` и `POST /api/auth/login`.

## События SSE: `GET /events` (viewer)

Формат: `event: <тип>`, `data: <JSON>`, `id: <монотонный номер>`. Каждое событие содержит `{"at": <время стенда>}`. События — сигналы «перечитай»; полезная нагрузка минимальна. Клиент при подключении и переподключении перечитывает все данные; сервер шлёт `ping` каждые 15 с реального времени.

```bash
curl -N http://localhost:8080/events
```

```
event: order.accepted
id: 128
data: {"at":"2026-10-09T12:00:00+03:00","order_id":"5b0c0c3e-…","product_ids":["salmon","pasta"]}
```

| Тип | Когда | Данные |
|---|---|---|
| `delivery.accepted` | принята новая поставка (не replay) | `delivery_id`, `product_ids` |
| `order.accepted` | заказ принят | `order_id`, `product_ids` |
| `order.rejected` | заказ отклонён | `order_id`, `product_ids` (недостающие) |
| `batch.expiring` | партия перешла в «истекает» (один раз на партию) | `batch_id`, `product_id`, `expires_at` |
| `batch.expired` | проход «дворника» списал партии (одно событие на проход) | `batch_ids` (не более 50), `product_ids`, `count` |
| `stock.changed` | после любого из событий выше | `product_ids` |
| `stoplist.changed` | состав стоп-листа изменился | `added`, `removed` (id блюд) |
| `hotoffers.changed` | список рекомендаций изменился | `algo` |
| `clock.changed` | смена скорости, прыжок, пауза, сброс | `now`, `speed` |
| `stand.reset` | сброс завершён | — |

Если партия из-за прыжка времени стала просроченной, минуя «истекает», `batch.expiring` для неё не шлётся — только `batch.expired`.
