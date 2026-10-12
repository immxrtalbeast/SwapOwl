# SwapOwl

SwapOwl — надстройка над iiko для линейки Swap интегратора «Флагман» (статус — концепт). iiko остаётся учётной системой; SwapOwl ведёт склад по партиям со сроками годности: FEFO-списание, просрочка, стоп-лист как функция остатков, горячие предложения и предложение поставок. Стек: Go (API + SSE), PostgreSQL, React.

Стенд: https://<домен> (заглушка: адрес подставит команда после развёртывания). Стенд открыт без входа (`AUTH_REQUIRED=false`), примеры ниже работают и на нём, подставьте адрес вместо `http://localhost:8080`.

## Запуск одной командой

```sh
cp .env.example .env
docker compose -f deploy/docker-compose.yml --env-file .env up --build
```

API (и `/healthz`) — на `http://localhost:8080`. Пароль PostgreSQL берётся из `POSTGRES_PASSWORD` в `.env` (без него контейнер базы не стартует). Миграции применяются при старте; на пустой базе загружается демо-сид (`SEED_ON_EMPTY=true` по умолчанию). Проверка: `curl http://localhost:8080/healthz`.

## Тесты

```sh
# Go: тесты с БД (сторожа tests/invariants, internal/store, internal/seed, internal/stand)
# без PostgreSQL молча пропускаются
docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=ci -e POSTGRES_USER=ci -e POSTGRES_DB=ci postgres:16-alpine
export TEST_DATABASE_URL='postgres://ci:ci@localhost:5432/ci?sslmode=disable'
export INVARIANTS_DATABASE_URL="$TEST_DATABASE_URL"
gofmt -l . && go vet ./... && go test -race -p 1 ./...

# фронтенд
cd frontend && npm ci && npm run typecheck && npm test && npm run build

# e2e (Playwright, демо-сценарий) — против запущенного стенда
cd e2e && npm ci && npx playwright install chromium && E2E_BASE_URL=https://<стенд> E2E_DEMO_TOKEN=<токен> npm test
```

e2e сбрасывает стенд и двигает его время, подробности и локальный стенд — в `e2e/README.md`.

## Решения по неоднозначностям

Полная таблица — `.ai/solution.md`, раздел 5; контракты — `docs/api.md`.

| Вопрос | Решение |
|---|---|
| Срок хранения | В позиции поставки ровно одно из `shelf_life_hours` (1…87600) и `expires_on` (`YYYY-MM-DD`). `expires_at` считается один раз при приёме: `received_at + срок` либо начало следующего дня после `expires_on` по времени ресторана. `received_at` — время стенда в момент приёма; `created_at` — реквизит документа и в расчёт не входит, поэтому пример с `shelf_life_hours` работает при любом времени стенда |
| Единицы | Всё хранится как `int64` в `g`, `ml`, `pcs`. `quantity` на входе — только JSON-строка (`"2.5"`) и `unit` из `g, kg, ml, l, pcs`; JSON-число, экспонента, запятая, дробь мельче базовой единицы, дробные штуки, ноль и значения выше `10^12` — `400`. Float в остатках нет |
| Заказ | Всё или ничего: если не хватает хотя бы одного продукта, заказ отклоняется целиком (`409 insufficient_stock` с перечнем `missing`) и сохраняется как `rejected`. Идемпотентность по `id`: тот же `id` и тело — повтор (`replayed: true`), другое тело — `409 order_conflict` |
| Продажа из кассы | `"source": "pos"`: блюдо в iiko уже продано, отказать нельзя. Списывается всё доступное по FEFO, недостача записывается как расхождение (`shortfalls`), ответ всегда `201` |
| Часовой пояс | Одна зона на экземпляр (`RESTAURANT_TZ`, по умолчанию `Europe/Moscow`). «Годен до даты» — до начала следующего локального дня. Зоны с переводом часов в полночь не поддерживаются |
| Окно опоздания | Необязательное `occurred_at` у поставок и заказов допустимо в пределах `LATE_EVENTS_HOURS` (по умолчанию 2 ч) до «сейчас»; иначе `400` — для старых событий нужна корректировка. «Дворник» списывает просроченную партию не раньше `expires_at` + окно, но остатки и стоп-лист считаются по `now < expires_at` |
| Просрочка | Партия доступна, пока `now < expires_at` — это предикат в каждом запросе, а не фоновый процесс. Списание «дворником» пишется со временем `expires_at` |
| Почему блокируется продукт, а не партия | Все писатели партий (заказ, поставка, «дворник», прыжок времени) сначала берут `SELECT … FOR UPDATE` на затронутые продукты по отсортированному списку id, и только потом читают часы и партии. FEFO и сумма потребности смотрят на все партии продукта сразу, поэтому блокировка одной партии не защитит от двойной продажи и «оживления» просроченной; общий порядок по продукту исключает дедлок. Проверяют сторожа №3 (`tests/invariants/inv03_concurrency_test.go`, 50 параллельных заказов) и №4 (`inv04_wiper_race_test.go`, гонка заказа с «дворником») |

## Три запроса curl

Проверены на локальном стенде. Количество — строкой, срок — в часах, поэтому примеры не зависят от времени стенда. Продукт `beef` и блюдо `burger` есть в демо-сиде; на чистом экземпляре поставка с явным `unit` создаёт продукт сама, а блюда заводятся через `PUT /api/catalog`.

```sh
API=http://localhost:8080

# 1. Поставка: 2,5 кг говядины на 72 часа -> 201, партия с expires_at = received_at + 72 ч
curl -i -X POST $API/api/deliveries -H 'Content-Type: application/json' -d '{
  "id": "README-D-1", "number": "1", "created_at": "2026-10-09T08:00:00+03:00",
  "items": [{"product_id": "beef", "name": "Говяжья вырезка", "quantity": "2.5", "unit": "kg", "shelf_life_hours": 72}]}'

# 2. Заказ: 2 бургера -> 201, writeoffs по партиям в порядке FEFO
curl -i -X POST $API/api/orders -H 'Content-Type: application/json' -d '{
  "id": "6f1c2a3e-8a51-4d0e-9d0b-000000000102", "items": [{"dish_id": "burger", "portions": 2}]}'

# 3. Повтор поставки (тот же id и тело) -> 200, "replayed": true, остатки не меняются
curl -i -X POST $API/api/deliveries -H 'Content-Type: application/json' -d '{
  "id": "README-D-1", "number": "1", "created_at": "2026-10-09T08:00:00+03:00",
  "items": [{"product_id": "beef", "name": "Говяжья вырезка", "quantity": "2.5", "unit": "kg", "shelf_life_hours": 72}]}'
```

Повторный запуск тех же команд безопасен: поставка и заказ вернут `200` с `replayed: true`. Тот же `id` поставки с другим телом даёт `409 delivery_conflict`; заказ, который не хватает остатков (например, `"portions": 9999`), — `409 insufficient_stock`. Остальное API — `docs/api.md`.

## Переменные окружения

Полный список — `.ai/solution.md`, раздел 7.6; образец — `.env.example`.

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `DATABASE_URL` | — | Строка подключения PostgreSQL (в compose собирается из `POSTGRES_*`) |
| `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | `umirhack`, —, `umirhack` | База в compose; пароль обязателен |
| `API_PORT` / `PORT` | `8080` | Порт API (compose / процесс) |
| `RESTAURANT_TZ` | `Europe/Moscow` | Зона ресторана |
| `OPEN_AT`, `CLOSE_AT` | `10:00`, `23:00` | Часы работы |
| `LATE_EVENTS_HOURS` | `2` | Окно опоздания `occurred_at` (0…168 ч) |
| `CLOCK_MODE` | `real` | `real` или `simulated` |
| `DEMO_MODE`, `DEMO_TOKEN` | `false`, — | Демо-ручки `/api/demo/*` и токен для сброса и времени |
| `SEED_ON_EMPTY` | `true` | Загружать демо-сид на пустую БД |
| `AUTH_REQUIRED` | `true` | Требовать вход; см. «Доступ» |
| `ADMIN_LOGIN`, `ADMIN_PASSWORD` | — | Первый администратор |
| `SESSION_TTL_HOURS` | `12` | Срок сессии |
| `SITE_DOMAIN` | — | Домен для HTTPS |
| `EXPIRING_SHARE`, `EXPIRING_MIN_HOURS`, `EXPIRING_MAX_SHARE` | `0.2`, `12`, `0.5` | Порог «истекает» |
| `DEMAND_WINDOW_DAYS`, `DEMAND_PRIOR_DAYS` | `14`, `3` | Оценка спроса |
| `SUPPLY_COVER_DAYS`, `SUPPLY_MIN_PORTIONS` | `3`, `10` | Предложение поставок |
| `HOTOFFERS_ALGO` | `b` | Алгоритм горячих предложений (`docs/bench.md`) |
| `HOT_TOP_N`, `HOT_W_MIN_HOURS`, `HOT_COVER_HOURS`, `HOT_MAX_ITER`, `HOT_HORIZON_HOURS` | `3`, `0.5`, `12`, `200`, `24` | Параметры алгоритмов |
| `LOG_LEVEL`, `DB_POOL_SIZE` | `info`, `20` | Журнал и пул соединений |

Дальше: `docs/onboarding.md` — регламент подключения ресторана к iiko, `docs/iiko.md` — адаптер, `docs/operations.md` — резервные копии, `docs/notify.md` — уведомления, `docs/bench.md` — результат сравнения алгоритмов.

## Доступ

Правила — `.ai/solution.md`, раздел 2.9; эндпоинты и коды — 7.4.

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `AUTH_REQUIRED` | `true` | При `false` запрос без учётных данных идёт с ролью `operator`; при `true` — `401` на `/api/*` и `/events` (кроме `/healthz` и `POST /api/auth/login`) |
| `ADMIN_LOGIN`, `ADMIN_PASSWORD` | — | Первый администратор; создаётся при старте, если таблица пользователей пуста. При `AUTH_REQUIRED=true`, пустой таблице и без этих переменных сервис не запускается |
| `SESSION_TTL_HOURS` | `12` | Срок сессии |
| `SITE_DOMAIN` | — | Если задан, cookie `session` ставится с `Secure`; для локального HTTP не задавайте |

- Роли: `viewer` (чтение, `/events`), `operator` (плюс изменяющие запросы), `admin` (плюс `/api/users`, `/api/api-keys`).
- Вход: `POST /api/auth/login` `{"login","password"}` → cookie `session`; `POST /api/auth/logout`; `GET /api/auth/me`. Больше 5 неудачных входов по логину за 15 минут — `429 too_many_attempts`; счётчик хранится в БД (`login_attempts`) и переживает рестарт.
- Смена своего пароля: `POST /api/auth/password` `{"old","new"}` → `204`; остальные сессии пользователя удаляются, текущая остаётся. Неверный старый пароль — `401`, больше 5 неверных за 15 минут — `429`.
- Журнал действий в БД (`audit_log`: вход и отказ во входе, выход, пользователи, ключи, смена пароля, демо-ручки, импорт справочников): `GET /api/audit?limit=` (1…1000, по умолчанию 100), только `admin`. Открытие и закрытие SSE-соединений в таблицу не пишутся — они остаются в журнале запросов.
- Ключи интеграции: `Authorization: Bearer <ключ>`; создаёт и отзывает admin через `/api/api-keys` (ключ показывается один раз).
- Изменяющие запросы принимаются только с `Content-Type: application/json`.
- В журнале на каждый запрос — роль и id пользователя или ключа; записи аудита помечены `audit=true`. Пароли, токены и ключи в журнал не попадают.

## Заказы

- `POST /api/orders` `{"id": "<UUID>", "items": [{"dish_id", "portions"}]}` — потребность суммируется по всем блюдам; заказ списывает всё по FEFO либо ничего. `201` — принят (`writeoffs`: партия — количество), `409 insufficient_stock` — отклонён с перечнем `missing` (заказ сохраняется как `rejected`), повтор того же id и тела — `200`/`409` с `replayed: true`, другое тело — `409 order_conflict`.
- `GET /api/orders?status=accepted|rejected&limit=` — история; `GET /api/dishes` — блюда с рецептами.
- Партия доступна при `now < expires_at` независимо от фонового прохода.

## Экземпляр для ресторана

Один ресторан — один экземпляр сервиса со своим конфигом (`.ai/solution.md`, 8.1). Экземпляр стартует с пустыми справочниками, на реальных часах и с обязательным входом.

| Переменная | Значение | Зачем |
|---|---|---|
| `DEMO_MODE` | `false` | `/api/demo/*` отвечают `404` |
| `CLOCK_MODE` | `real` | реальные часы |
| `LATE_EVENTS_HOURS` | `2` | окно `occurred_at` у поставок и продаж (0…168 ч); «дворник» списывает партию не раньше `expires_at` + окно |
| `SEED_ON_EMPTY` | `false` | демо-данные не загружаются |
| `AUTH_REQUIRED` | `true` | без входа `401` |
| `DATABASE_URL` | строка подключения | обязательна |
| `ADMIN_LOGIN`, `ADMIN_PASSWORD` | первый администратор | обязательны при пустой таблице пользователей |
| `RESTAURANT_TZ`, `OPEN_AT`, `CLOSE_AT` | зона и часы работы ресторана | по умолчанию `Europe/Moscow`, `10:00`–`23:00` |
| `SCHEDULE` | расписание по дням недели, заменяет `OPEN_AT`/`CLOSE_AT`; интервал через полночь относится к дню открытия, день без записи — выходной | не задан; пример `mon-thu 10:00-23:00; fri-sat 11:00-02:00; sun 11:00-22:00` |

`SEED_ON_EMPTY=false` вместе с `DEMO_MODE=true` — сервис не запускается и сообщает об этом в ошибке конфигурации.

Порядок первого запуска:
1. Задайте переменные выше и запустите `cmd/api`; миграции применяются при старте.
2. Войдите под `ADMIN_LOGIN` (`POST /api/auth/login`) и создайте пользователей с ролью `operator` через `POST /api/users`; для кассы/адаптера выпустите ключ через `POST /api/api-keys`.
3. Отправьте первую поставку `POST /api/deliveries` с явным `unit` у каждой позиции — продукт и партия создаются из неё.
4. Блюда и рецепты заведите через каталог API; до этого стоп-лист и предложения пусты.
