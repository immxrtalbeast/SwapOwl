# HTTP API и события SSE

Источник: `спецификация`, разделы 7.4 и 7.5. Контракт заморожен: изменение — только отдельным тикетом с меткой `core`.

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
| `delivery_conflict`, `order_conflict`, `insufficient_stock`, `unit_conflict` | 409 |
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

`occurred_at` (RFC 3339, необязательно) — когда поставка пришла фактически, для пакетной загрузки из iiko: `received_at = occurred_at`, срок считается от него. Допустимо `now − LATE_EVENTS_HOURS ≤ occurred_at ≤ now` (по умолчанию 2 ч, времена стенда), иначе `400 validation_error` с подсказкой оформить корректировку остатка; ничего не записано. Поле входит в хэш тела: тот же id с другим или без `occurred_at` — `409 delivery_conflict`.

В позиции не больше одного из `shelf_life_hours` (целое, 1…87600) и `expires_on` (`YYYY-MM-DD`); оба сразу — `400`. `unit` обязателен только для неизвестного продукта.

**Правило срока (FS-160).** Срок годности обязателен в каждой позиции: ровно одно из `expires_on` и `shelf_life_hours`. Позиция без срока — `400 validation_error` с полем `items[i].expires_on`, ничего не записано. Срок хранения продукта из каталога (`products.shelf_life_hours`) в поставку сам не подставляется: это обычный срок поставщика, который клиент передаёт явно (например, «Принять предложенное» берёт его из `GET /api/supply-suggestions`, поле `shelf_life_hours`).

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

## Справочники

### `PUT /api/catalog` (admin или ключ operator)

Импорт справочников из учётной системы (iiko): добавление и обновление, **без удаления** — продукты и блюда, которых нет в теле, остаются. Вошедший по паролю `operator` получает `403`.

```bash
curl -X PUT 'http://localhost:8080/api/catalog?dry_run=true' \
  -H 'Authorization: Bearer <key>' -H 'Content-Type: application/json' \
  -d '{
    "products": [
      {"id": "rice", "name": "Рис", "unit": "g", "price_minor_per_1000": 15000,
       "lead_days": 2, "pack_size": 1000, "shelf_life_hours": 720}
    ],
    "dishes": [
      {"id": "pilaf", "name": "Плов", "price_minor": 45000, "prior_daily": 12.5,
       "recipe": [{"product_id": "rice", "quantity": "0.15", "unit": "kg"}]}
    ]
  }'
```

- Продукт: `unit` — `g`, `ml` или `pcs`; `price_minor_per_1000` (копейки за 1000 базовых единиц, ≥ 0), `lead_days`, `pack_size` обязательны; `shelf_life_hours` (1…87600) необязателен — обычный срок поставщика: его показывает предложение поставок и берёт излишек инвентаризации; в поставку сам не подставляется (см. «Правило срока» выше). Если поле не передано, прежний срок у существующего продукта сохраняется (очистить его через API нельзя).
- Блюдо: `price_minor`, `prior_daily` обязательны; `recipe` непуст и **заменяет** прежний рецепт целиком. `quantity` — строка по правилам поставок (`"0.15"`), `unit` необязателен (по умолчанию базовая единица продукта; `kg`, `l` допустимы). Продукт рецепта берётся из тела или из БД, иначе `400 unknown_product`.
- Продукт, автосозданный из поставки, при импорте становится обычным справочным (`auto_created = false`) и не удаляется сбросом стенда.
- `?dry_run=true` — вернуть разницу, ничего не записав.
- Проверки старта (2.2) применяются к результату импорта по всему справочнику: у каждого продукта из рецептов цена > 0, рецепт каждого блюда непуст.
- Одна транзакция: любая ошибка — ничего не записано.

`200`:
```json
{"dry_run": true,
 "products": [{"id": "rice", "action": "created"}],
 "dishes": [{"id": "pilaf", "action": "updated", "fields": ["price_minor", "recipe"]}]}
```
`action` — `created`, `updated` или `unchanged`; `fields` — что изменилось при `updated`.

Ошибки: `400 validation_error` (в том числе `details.problems` — нарушения проверок старта), `400 unknown_product`, `409 unit_conflict` — у существующего продукта другая `unit` (`details.product_id`, `current_unit`, `import_unit`), `403 forbidden`.

## Склад и мониторинг (viewer)

- `GET /api/products` → `[{"id", "name", "unit", "available": Qty, "expiring": Qty, "batches_count", "nearest_expires_at"}]`
- `GET /api/batches?product_id=&status=` → `[{"id", "product_id", "delivery_id", "delivery_created_at", "quantity", "remaining", "received_at", "expires_at", "time_left_seconds", "status", "expiry_source"}]`. `expiry_source` ∈ `delivery | product_default | confirmed`. По умолчанию без списанных в ноль, порядок `(expires_at, id)`. `status` ∈ `ok | expiring | expired`.
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

`occurred_at` (RFC 3339, необязательно) — когда продажа произошла фактически (опрос кассы, CSV). Партии выбираются на этот момент: `received_at ≤ occurred_at < expires_at` и `remaining > 0`, затем FEFO; `orders.at`, `movements.at` и `at` в ответе равны `occurred_at`. Окно и ошибка — как у поставки (`LATE_EVENTS_HOURS`, `400 validation_error`); поле входит в хэш тела. «Дворник» списывает партию не раньше `expires_at + LATE_EVENTS_HOURS` (движение — со временем `expires_at`); остатки и стоп-лист по-прежнему считаются по `now < expires_at`.

`id` — UUID от клиента, обязателен. `source` — `"api"` (по умолчанию) или `"pos"` (продажа из кассы); входит в хэш тела, другое значение — `400`. `portions` — целое 1…10000. Одно блюдо — не более одной строки.

- `201` — `{"id", "source", "status": "accepted", "at", "replayed": false, "writeoffs": [{"product_id", "batch_id", "qty": Qty, "expires_at"}], "shortfalls": [{"product_id", "product_name", "quantity": Qty}]}`. Для `api` `shortfalls` всегда пуст.
- `source: "pos"` — касса уже продала блюдо, отказать нельзя: по FEFO списывается всё доступное, остаток потребности по каждому продукту — недостача (`shortfalls`, таблица `shortfalls`, расхождение для сверки склада). Ответ всегда `201` со статусом `accepted`, `409 insufficient_stock` для `pos` не бывает. Остатки партий не уходят в минус; сумма `writeoffs` + `shortfalls` по продукту равна потребности.
- `409 insufficient_stock` — `details: {"order_id", "status": "rejected", "at", "missing": [{"product_id", "product_name", "required", "available", "missing"}]}`; заказ сохранён как отклонённый.
- Повтор с тем же id и телом: принятый — `200` и то же тело с `"replayed": true`; отклонённый — `409 insufficient_stock` и то же тело с `"replayed": true`. Тот же id, другое тело — `409 order_conflict`.

### `GET /api/orders?status=` (viewer)

История, новые сверху: `[{"id", "source", "status", "at", "replayed", "items", "writeoffs", "missing", "shortfalls"}]`. Отклонённые отделены полем `status` (`accepted` / `rejected`).

- `writeoffs` — из какой партии сколько списано: `[{"product_id", "product_name", "batch_id", "qty": Qty, "expires_at"}]`; `shortfalls` — недостача заказа из кассы (`source: "pos"`), у `api` пуст; `missing` — нехватка отклонённого заказа.
- `limit` — размер страницы, 1…500 (по умолчанию 100).
- `before` — id заказа, после которого продолжить: передайте id последнего заказа предыдущей страницы. Порядок `at DESC, id`, ключ точный: заказы с одинаковым `at` не теряются и не повторяются. Неизвестный или не UUID — `400 validation_error`.
- Заголовок `X-Total-Count` — число заказов с этим статусом целиком (без `status` — всех). Его показывает заголовок «Принятые · 1 284»; размер страницы (`limit`) — не счётчик.

## Приёмка со сроками (2.1а, миграция `0004`)

Партии с `expiry_source = product_default` — принятые до FS-160 со сроком из карточки; новые поставки без срока не принимаются.

- `GET /api/receiving` (viewer) → `[{"id", "product_id", "product_name", "delivery_id", "delivery_number", "quantity", "remaining", "received_at", "expires_at", "time_left_seconds", "expiry_source"}]` — действующие партии с `product_default`, принятые за последние 48 ч стенда, новые сверху.
- `PATCH /api/batches/{id}/expiry` (operator) — `{"expires_on": "YYYY-MM-DD"}` (конец дня по поясу ресторана) или `{"shelf_life_hours": N}` (от `received_at`), ровно одно поле. `200` — `{"batch_id", "product_id", "expires_at", "expiry_source": "confirmed", "received_at", "time_left_seconds"}`. Идёт под блокировкой продукта, движение не пишется, в журнал аудита (`batch_expiry_confirmed`) уходят старый и новый срок; SSE `stock.changed`. Ошибки: `400 validation_error`, `404 unknown_batch`, `409 batch_closed` (партия просрочена или списана в ноль), `409 expiry_not_future` (новый срок не позже времени стенда).

## Корректировки остатка (operator)

Каждая операция идемпотентна по своему `id` (строка 1…128 символов, ключ — в таблице `corrections`, миграция `0005`): тот же `id` и то же тело — `200` и прежний ответ с `"replayed": true`, другое тело — `409 correction_conflict`. Отклонённая операция ничего не записывает, и её `id` остаётся свободным. Количества — JSON-строки, `unit` необязателен (по умолчанию базовая единица продукта). Все операции идут по протоколу блокировок 2.3 и меняют остаток относительными `UPDATE`. После новой операции уходят SSE `stock.changed` и, если состав стоп-листа изменился, `stoplist.changed`.

Виды движений: `receipt`/`return` увеличивают остаток партии, `sale`/`writeoff`/`adjustment` уменьшают; `receipt + return − sale − writeoff − adjustment = remaining`. Значения — в `internal/store/kinds.go`.

### `POST /api/writeoffs`

`{"id", "batch_id", "qty": "1.5", "unit": "kg", "reason": "spoilage" | "damage"}` — списывает брак с партии: движение `writeoff` с этой причиной. `201` — `{"id", "batch_id", "product_id", "qty", "reason", "remaining", "at", "replayed"}`. Ошибки: `400 validation_error`, `404 unknown_batch`, `409 insufficient_batch` (`qty` больше остатка партии).

### `POST /api/inventory`

`{"id", "product_id", "counted": "2.5", "unit": "kg"}` — пересчитанное количество продукта (допустим `"0"`); сверяется с суммой остатков непросроченных партий.
- Недостача — списывается по FEFO движениями `adjustment` / `inventory_shortage`.
- Излишек — новая партия поставки `INV-<id>` (`quantity = remaining`), срок — `shelf_life_hours` продукта от момента операции, движение `receipt` / `inventory_surplus`. Нет срока по умолчанию — `400 validation_error`.
- Совпало с учётом — ничего не пишется.

`201` — `{"id", "product_id", "counted", "shortage": [{"batch_id", "product_id", "qty"}], "surplus": null | {партия}, "at", "replayed"}`. Неизвестный продукт — `400 unknown_product`.

### `POST /api/orders/{id}/cancel`

`{"id"}` — `id` операции; `{id}` в пути — отменяемый заказ. Допустима один раз и только для принятого заказа. Проданное возвращается в те же партии, если они не просрочены (`now < expires_at`): движение `return`. Проданное из просроченных партий остаток не восстанавливает: пишутся `return` и `adjustment` / `return_expired` на то же количество (баланс партии не меняется).

`201` — `{"id", "order_id", "returned": [{"batch_id", "product_id", "qty"}], "expired": [...], "at", "replayed"}`. Ошибки: `404 unknown_order`, `409 order_not_cancellable` (заказ отклонён), `409 already_cancelled` (другая операция уже отменила заказ). Недостача `pos`-заказа (`shortfalls`) не пересматривается.

## Горячие предложения

### `GET /api/rescue-ladder` (viewer)

«Лестница спасения»: для каждой истекающей партии — ступени `sell` (продать горячим), `discount` (скидка гостю с витрины), `prep` (заготовка, пока без расчёта) и `transfer` (передача в точку сети, считает хаб) с деньгами в копейках: `saved_minor` (сохранённая себестоимость), `cost_minor` (цена ступени, для скидки — сама скидка), `net_minor`. `best` — расчётная ступень с наибольшим `net_minor`, `combined_minor` — «сначала продать, остаток со скидкой».
### `GET /api/analytics/forecast-accuracy?days=7` (viewer)

Точность прогноза спроса: для каждых из последних `days` (1…30, по умолчанию 7) суток берётся λ, какой её посчитал бы
движок на начало суток, и сравнивается с фактом. `wape_pct` — ошибка с обучением, `baseline_wape_pct` — «только априор»;
`dishes` — по блюдам, с наибольшей ошибкой сверху. `enough_data=false`, если проверяемых суток меньше трёх.

### Ручная пауза блюда: `GET /api/dish-holds` (viewer), `PUT` и `DELETE /api/dish-holds/{dish_id}` (operator)

Менеджер или повар снимает блюдо с продажи вне зависимости от остатков: `PUT` с `{"note":"сломана плита"}` (причина необязательна, до 200 символов), `DELETE` снимает паузу. Заказ с блюдом на паузе отклоняется целиком: `409 dish_on_hold`, в `details.dishes` — `dish_id` и `note`; склад не меняется, отказ сохраняется и виден в «Отклонённых». Автоматический стоп-лист (`GET /api/stop-list`, `available` в `GET /api/dishes`) по-прежнему считается только по остаткам и пауз не знает. Продажа из кассы (`source=pos`) паузой не отклоняется. `POST /api/demo/reset` снимает все паузы; установка и снятие пишутся в журнал (`hold_set`, `hold_delete`).

### `GET /api/hot-offers?algo=` (viewer)

У предложения есть `margin_minor` (маржа порции: цена блюда − закупочная стоимость рецепта, копейки; поля нет без цены) и `expected_margin_minor` (маржа × `target_today`). Порядок задаёт алгоритм; при строго равном `priority` и равном `sell_until` выше предложение с большей маржой (раньше срок — раньше в списке, как требует спецификация).

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

## «Помощник»: `POST /api/assistant/ask` (viewer, ничего не записывает)

Ответ на вопрос своими словами. Разбор по правилам, без модели: тот же вопрос на тех же данных всегда даёт тот же ответ. Данные — те же, что на экранах: остатки, предупреждения, стоп-лист, предложения поставок, горячие предложения.

```bash
curl -X POST http://localhost:8080/api/assistant/ask -H 'Authorization: Bearer <key>' -H 'Content-Type: application/json' \
  -d '{"question": "Сколько осталось лосося?"}'
# {"intent":"product","answer":"Лосось: на складе 1,4 кг (2 партии). Ближайший срок — сегодня в 18:00. Истекает 600 г — сегодня в 18:00."}
```

- `intent`: `stop`, `expiring`, `order`, `hot`, `plan` (ближайшие дни: что истекает и что заказать), `product`, `dish`, `money` (деньги под риском на 7 дней — как «Под угрозой» на «Сегодня», и списано за период: сегодня, вчера, за неделю, за месяц, за N дней; рубли и топ-3 продукта), `ladder` (лестница спасения по партии: лучшая ступень и «не спишем ≈ N ₽»), `margin` (что выгоднее продавать, маржа блюда), `accuracy` (WAPE прогноза с обучением против априора; «пока мало истории», если данных мало), `help`, `unknown` (непонятый вопрос — подсказки в `answer`, с ближайшей темой, если она угадана).
- Опечатки: слова от 5 букв сравниваются с ключевыми корнями по расстоянию Дамерау–Левенштейна ≤ 1. Если источник темы не подключён (маршрут выключен), ответ — «эти данные сейчас недоступны», а не пустой. Лестница спасения и маржа появятся в ответах, когда в `main` будут `/api/rescue-ladder` и `margin_minor` (#187, #188).
- Название продукта или блюда ищется по общему корню слова («лосося» → «Лосось»): блюдо «Стейк из лосося» отвечает по порциям, продукт — по остатку.
- Вопрос 1…300 символов, иначе `400 validation_error` с `details.field: question`. Тот же ответ даёт бот: `/ask вопрос` или обычное сообщение.

## «Что если»: `POST /api/what-if` (viewer, ничего не записывает)

Пересчёт прогноза списаний по сценариям на копии снимка склада; часы — часы API, случайности нет (тот же снимок и тело → тот же ответ).

```bash
curl -X POST http://localhost:8080/api/what-if -H 'Authorization: Bearer <key>' -H 'Content-Type: application/json' \
  -d '{"horizon_days": 3, "scenarios": [
        {"type": "discount", "dish_id": "caesar", "percent": 20},
        {"type": "order_less", "product_id": "chicken", "percent": 30},
        {"type": "demand", "dish_id": "caesar", "factor": 0.5}]}'
```

- `horizon_days` 1…7; до 5 сценариев; расчёт ограничен 5 с (`503 overloaded` с текстом, что уменьшить).
- `discount`: `dish_id`, `percent` 5…50 — спрос ×(1 + 1,5·percent/100); коэффициент — допущение (`simulator.DiscountElasticity`). `order_less`: `product_id`, `percent` 10…100 — из остатка убирается доля, начиная с самых свежих партий. `demand`: `dish_id`, `factor` 0.5…2.
- Ответ: `base` (`total_minor` и продукты — как `GET /api/forecast/writeoffs`) и по каждому сценарию `total_minor`, `delta_minor`, `delta_pct`, `products` и `assumptions` словами. Пример — в `docs/openapi.yaml`.
- `400 validation_error` с `details.field` вида `scenarios[0].percent`.

## `GET /api/public/rescue` (без входа)

Витрина «Спаси продукты»: блюда из горячих предложений, которые выгодно заказать сегодня со скидкой. Только чтение. Включается `PUBLIC_RESCUE=true` (по умолчанию выключено — `404`). Не больше 50 строк, лимит 60 запросов в минуту с адреса (`429`), `Cache-Control: public, max-age=15`.

```
curl -i http://localhost:8080/api/public/rescue
[{"dish_id": "salmon-steak", "dish_name": "Стейк из лосося", "servings_left": 4, "discount_percent": 20,
  "valid_until": "2026-10-09T16:00:00+03:00", "note": "Скидка на блюдо дня"}]
```

В список не попадают блюда из стоп-листа и скрытые менеджером; `servings_left` не больше порций, собираемых из доступного остатка. `discount_percent` — скидка экрана «Сегодня» (20, допустимо 5–50). Партий, себестоимости, причин и остатков в ответе нет.

## `GET /api/public/menu` (без входа)

Меню гостя со стоп-листом: у каждого блюда `dish_id`, `dish_name` и `status` — `available`, `few` (до трёх порций) или `stopped`. Рецептов, причин стопа, остатков и партий в ответе нет. Включается тем же `PUBLIC_RESCUE=true`, что и витрина «Спаси продукты» (иначе `404`); лимит 60 запросов в минуту с адреса (`429`), `Cache-Control: public, max-age=15`. Страница — `#/guest-menu`.

## Аналитика (viewer)

- `GET /api/analytics/writeoffs?from=&to=` → по продуктам и причинам: количество и стоимость.
- `GET /api/analytics/shortages?from=&to=` → продукты по числу отклонённых заказов и недостающему количеству.
- `GET /api/analytics/bench` → `{"available": bool, "runs": 200, "main_algo": "b", "waste_reduction_vs_random_pct": 39.0, "waste_reduction_vs_nearest_pct": 27.0}`; `available: false`, если результата bench нет.

## Привязка Telegram по коду

| Метод | Путь | Кто | Ответ |
|---|---|---|---|
| POST | `/api/auth/telegram-link-code` | вошёл в веб (любая роль) | `200 {"code":"ABCD-EFGH","expires_in_seconds":600}`; новый вызов отзывает прежний код этого пользователя |
| POST | `/api/auth/telegram-link` `{"init_data","code"}` | публично | `200` как у `/api/auth/telegram` (сессия и роль) и привязка пользователя к Telegram-аккаунту; код одноразовый |
| GET | `/api/auth/telegram-link` | вошёл в веб | `{"linked":true\|false}` |
| DELETE | `/api/auth/telegram-link` | вошёл в веб | `204`, снимает свою привязку |

Ошибки `POST /api/auth/telegram-link`: `400 invalid_link_code` — код неверный, просрочен или использован (одно
сообщение на все три случая); `401` — неверная подпись `init_data` (код не сгорает); `429` — слишком много
попыток; `409` — Telegram уже привязан к другому логину. Подробности — [`telegram-miniapp.md`](telegram-miniapp.md).

## Стенд

- `GET /api/clock` → `{"now", "speed", "timezone", "simulated": bool, "simulator_running": bool}` — открыт всем.
- С токеном (`Authorization: Bearer <DEMO_TOKEN>`, иначе `401`):
  - `POST /api/demo/clock/speed` `{"speed": 600}` (0…3600);
  - `POST /api/demo/clock/jump` `{"hours": 6}` (`0 < hours ≤ 720`);
  - `POST /api/demo/reset`;
  - `POST /api/demo/simulator` `{"running": true}`;
  - `POST /api/demo/burst` `{"dish_id": "salmon-pasta", "orders": 10}` (1…50 параллельных заказов по одной порции).
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


### Сопоставление кодов поставщика: `GET /api/supplier-codes` (viewer), `PUT` и `DELETE /api/supplier-codes/{code}` (operator)

Код поставщика (GTIN из маркировки или артикул, `[0-9A-Za-z._-]{1,64}`) → продукт справочника. Приёмка сканом сначала ищет код здесь, затем в локальной памяти устройства; «Запомнить» пишет сюда, поэтому другое устройство не спрашивает продукт снова. `PUT {"product_id"}` идемпотентен и заменяет прежний продукт; неизвестный продукт — `404 unknown_product`; `DELETE` — `204` (повтор тоже). Продукт удалён — запись уходит вместе с ним; сброс стенда справочник очищает.
