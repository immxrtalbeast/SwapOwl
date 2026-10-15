# SwapOwl

SwapOwl — самостоятельная система учёта партий, сроков годности и стоп-листа для ресторана. Работает без внешних систем: поставки, заказы, приёмка со сроками — свои экраны и REST API ([`docs/openapi.yaml`](docs/openapi.yaml), [`docs/api.md`](docs/api.md)). FEFO-списание, просрочка, стоп-лист как функция остатков, горячие предложения, предложение и заказ поставщику. Интеграции — отдельный слой поверх API: REST, импорт накладной поставщика (CSV, отдельная задача) и адаптер к учётной системе. iiko — *пример* такого адаптера ([`cmd/iiko-adapter`](cmd/iiko-adapter)), проверенный на эмуляторе. Стек: Go (API + SSE), PostgreSQL, React.

**Стенд:** https://swapowl.duckdns.org — открыт без входа (`AUTH_REQUIRED=false`), примеры ниже работают и на нём, и локально (`http://localhost:8080`).

**Тесты:** `gofmt -l . && go vet ./... && go test -race -p 1 ./...` (для тестов с БД нужен PostgreSQL, см. «Тесты»).

## Что сказал эксперт «Флагмана» → как это сделано → где проверить

Пересказ замечаний эксперта к основному процессу и то, как каждое закрыто. Команды выполняются на https://swapowl.duckdns.org (`API=https://swapowl.duckdns.org`) и локально (`API=http://localhost:8080`). Количество — строкой; демо-сид содержит продукт `beef`, блюдо `burger`.

| Что отметил эксперт | Как сделано | Где проверить |
|---|---|---|
| Срок годности обязателен при приёмке (FS-160) | В каждой позиции поставки ровно одно из `expires_on` и `shelf_life_hours`; позиция без срока — `400 validation_error`, поле `items[0].expires_on`, ничего не записано | экран «Приёмка»; команда 1 ниже; `docs/api.md`, «Правило срока» |
| Блюдо из стоп-листа нельзя добавить в заказ (#155) | Заказ через API — всё или ничего: недостающий продукт даёт `409 insufficient_stock` с перечнем `missing`, остатки не меняются, заказ сохраняется как отклонённый | экран «Заказы»; команда 2 ниже; тест `tests/invariants` (атомарность заказа) |
| Горячие предложения видны менеджеру и официанту; менеджер добавляет и скрывает (FS-159) | Расчёт горячих отдаётся обоим экранам; менеджер закрепляет (`pin`) или скрывает (`hide`) блюдо, открытые экраны обновляются по SSE `hotoffers.changed` | экраны «Горячие предложения» (менеджер) и «Для зала» (официант); `PUT /api/hot-offers/overrides/{dish_id}` — команда 3 ниже |
| «Годен до 12 октября» — годен весь день по времени ресторана | `expires_on` означает конец этого дня в `RESTAURANT_TZ`: `expires_at` — полночь начала 13 октября (по Москве; API отдаёт момент в UTC, `2026-10-12T21:00:00Z`); партия доступна, пока `now < expires_at` | команда 4 ниже: для `expires_on` = 2026-10-20 в ответе `expires_at` = `2026-10-20T21:00:00Z` — это полночь 21 октября по `Europe/Moscow`; граница суток — «Принятые решения», строка «Часовой пояс» |
| Автономная система с интеграциями, а не надстройка над iiko | Своё ядро, экраны и REST; iiko — один из адаптеров поверх API, проверенный на эмуляторе, не на живом iiko | [`docs/iiko.md`](docs/iiko.md), раздел «Интеграции и iiko» |

```sh
API=https://swapowl.duckdns.org

# 1. Поставка без срока -> 400 validation_error, items[0].expires_on
curl -i -X POST $API/api/deliveries -H 'Content-Type: application/json' -d '{
  "id": "EXPERT-D-1", "number": "1", "created_at": "2026-10-09T08:00:00+03:00",
  "items": [{"product_id": "beef", "name": "Говяжья вырезка", "quantity": "1", "unit": "kg"}]}'

# 2. Заказ, на который не хватает остатков -> 409 insufficient_stock, "missing": [...], остатки не изменились
curl -i -X POST $API/api/orders -H 'Content-Type: application/json' -d '{
  "id": "6f1c2a3e-8a51-4d0e-9d0b-000000000171", "items": [{"dish_id": "burger", "portions": 9999}]}'

# 3. Менеджер закрепляет блюдо -> 200 (скрыть: "mode": "hide"); снять: DELETE на тот же путь -> 204
curl -i -X PUT $API/api/hot-offers/overrides/burger -H 'Content-Type: application/json' \
  -d '{"mode": "pin", "note": "предлагать к обеду"}'
curl -s $API/api/hot-offers/overrides

# 4. «Годен до 20 октября»: expires_on -> expires_at = полночь 21 октября по Москве, в ответе — 2026-10-20T21:00:00Z
#    (дата должна быть позже текущей даты стенда: GET /api/clock; иначе партия придёт просроченной)
curl -i -X POST $API/api/deliveries -H 'Content-Type: application/json' -d '{
  "id": "EXPERT-D-2", "number": "2", "created_at": "2026-10-09T08:00:00+03:00",
  "items": [{"product_id": "cream", "name": "Сливки", "quantity": "600", "unit": "ml", "expires_on": "2026-10-20"}]}'
```

Выводы на стенде, цифры эффекта и порядок показа — [`docs/demo-script.md`](docs/demo-script.md), цифры — раздел «Цифры эффекта» ниже.

## Соответствие кейсу

Каждое требование кейса — где оно в коде, какой тест его держит и как увидеть его на стенде. Сторожа `tests/invariants/` гоняются на настоящем PostgreSQL в CI на каждом PR; e2e `e2e/tests/` проходит демо-сценарий на стенде.

**Поставки и партии**

| Требование | Код | Тест | На стенде |
|---|---|---|---|
| Приём поставки: id, номер, дата создания, позиции | `internal/api/deliveries.go`, `internal/store/deliveries.go` | `TestPostDeliveryValidation`, `TestPostDeliveryNormalizesUnits` | «Приёмка»; `POST /api/deliveries` (curl ниже) |
| Ввод поставки без ручного набора: фото накладной и CSV | `internal/invoice`, `internal/api/invoices.go`, `frontend/src/pages/stock/photoInvoice.ts` | `TestClaudeRequestAndParse`, `TestRecognizeInvoiceErrors` | «Склад» → «Фото накладной» / «Пример фото»: принять, недовоз, брак |
| Каждая позиция — отдельная партия со своим сроком | `internal/store/batches.go` | `TestInv01QuantityConservation` | «Склад» → строка продукта раскрывается в партии |
| Срок каждой партии и оставшееся время | `internal/store/expiry.go`; «годен до даты» — до конца дня по `RESTAURANT_TZ` | `TestInv06StrictExpiryBoundary`, `TestInv07EndOfDay` | «Склад»: «через 14 ч», точная дата во всплывающей подсказке |
| Учёт количества без float, единицы `g/kg/ml/l/pcs` | `internal/domain/units` | `TestInv12ParseTable`, `TestInv12AddMulBounds` | `"1.5" kg` → «1,5 кг»; `1e3`, `-5`, `1,5` → `400` с причиной |
| Повтор поставки не удваивает остаток | `internal/store/deliveries.go` | `TestPostDeliveryStatusMapping` | curl №3 ниже → `replayed: true`; на экране «уже принята» |
| Остаток = сумма непросроченных партий; журнал движений сходится | `internal/store/movements.go` | `TestInv01QuantityConservation`, `TestInv01ConservationWithCorrections` (600 случайных шагов) | `GET /api/movements?product_id=` |

**Заказы и стоп-лист**

| Требование | Код | Тест | На стенде |
|---|---|---|---|
| Проверка ингредиентов по сумме всех блюд заказа | `internal/domain/sale.go` | `TestPostOrderSumsNeedsAcrossDishes` | «Заказы»: два блюда с общим продуктом |
| Списание по FEFO, в ответе — из какой партии сколько | `internal/domain/sale.go`, `internal/store/orders.go` | `TestInv05FEFOTable`, `TestInv05FEFOProperty` | ответ `writeoffs`; e2e шаг 4 |
| Отказ при нехватке — целиком, с перечнем недостающего | `internal/domain/sale.go` | `TestInv02OrderAtomicity`, `TestPostOrderRejectedIs409WithMissing` | `409 insufficient_stock`, `missing`; на экране — по-человечески |
| Нет двойной продажи при параллельных заказах | `internal/store/locks.go` (`FOR UPDATE` по продуктам) | `TestInv03ConcurrentOrders` (50 параллельных), `TestInv04OrdersVersusWiper` | «Демо-панель» → залп 10 заказов на 3 порции (e2e шаг 5) |
| Повтор заказа с тем же id не списывает второй раз | `internal/store/orders.go` | `TestInv09OrderIdempotency` | тот же `id` → `replayed: true` |
| Автоматический стоп-лист с причиной и числом порций | `internal/domain/stoplist.go` | `TestInv08StopListIsFunctionOfStock`, `TestStopListReasons` | «Меню и стоп-лист»: чего и сколько не хватает, «можно N порций» |
| Блюдо уходит в стоп при истечении партии без других событий | `internal/janitor`, SSE `/events` | `TestInv08StopListOnlyClockJump` | прыжок времени — стоп-лист меняется без перезагрузки (e2e шаг 6) |

**Мониторинг сроков**

| Требование | Код | Тест | На стенде |
|---|---|---|---|
| Остатки и остатки по партиям | `internal/api/stock.go` | `TestStockReadsUseStandClock` | «Склад» |
| Выявление истекающих (порог от срока партии) и сигнал по партии | `internal/domain/alerts.go`, `internal/janitor` | `TestSweepReportsExpiringBeforeExpired` | «Предупреждения», счётчик в шапке; сигнал один раз на партию |
| Просроченная партия не участвует в остатке и заказах | предикат `now < expires_at` в каждом запросе | `TestInv06StrictExpiryBoundary`, `TestInv13ClockNeverGoesBack` | e2e шаг 6 |
| Анализ и предложение поставок | `internal/domain/supply.go`, `internal/api/supply.go` | `TestSupplyBlockingFirst` | «Поставки»: что заказать, «закончится через», сколько блюд блокирует → заказ поставщику |

**Горячие предложения**

| Требование | Код | Тест | На стенде |
|---|---|---|---|
| Приоритет от срока, количества, граммовки и числа блюд | `internal/hotoffers/b` (основной), `internal/hotoffers/a` (простой) | `TestRankTable`, `TestPropertyMonotoneByExpiry`, `TestInv11MonotoneInExpiry` | «Горячие предложения» |
| Объяснение: продукт, партия, количество под риском, срок | `internal/api/hotoffers.go` | `TestInv10HotOffersValid` | карточка: цель в порциях, до какого времени, сколько не спишем (≈ N ₽) |
| Нет блюд из стоп-листа и блюд, съедающих дефицитный продукт | `internal/hotoffers/b` | `TestStoppedAndDoomedSkipped`, `TestScaleScarceIngredientExcludedWithReason` | — |
| Цель по общему продукту не больше реального риска | `internal/hotoffers/b` | `TestScaleSharedProductTargetWithinRisk`, `TestSharedRiskTargetsWithinProductRisk` | — |
| Ручная правка менеджером и экран для зала | `internal/api/hot_overrides.go`, `applyOverrides` | `TestHotOffersApplyManagerOverrides`, `TestHotOverridesSetReplaceDelete` | «Горячие предложения»: «Закрепить», «Скрыть», «Добавить блюдо»; «Для зала» |
| Автообновление при изменении остатков | SSE `/events` | `TestEventsStreamFormatAndPing` | заказ в одной вкладке — список меняется в другой |
| Измеренный эффект | `internal/simulator` | `TestFollowHotReducesWriteoffsSameSeed` | «Аналитика»; `go run ./cmd/simulator bench -n 200`; «Сеть» — две точки со SwapOwl и без вживую |

**Дополнительно в кейсе**

| Пункт | Есть | Где |
|---|---|---|
| Автосписание просрочки | да | `internal/janitor`, причина «истёк срок» в журнале |
| Аналитика списаний | да | «Деньги», «Аналитика»; `GET /api/analytics/writeoffs` (в ₽) |
| Статистика дефицитных ингредиентов | да | `GET /api/analytics/shortages` — по отклонённым заказам |
| Прогноз момента стоп-листа | да | «Поставки»: `stockout_at` и `blocks_dishes`; `GET /api/forecast/writeoffs` — прогноз списаний |
| WebSocket/SSE | да | `GET /events` |
| Генератор поставок и заказов | да | `cmd/simulator`, «Демо-панель»; `TestStabilityUnderGenerator` |
| Дашборд склада | частично | свои экраны «Сегодня» и «Сеть»; `/metrics` в формате Prometheus, готового дашборда Grafana нет |
| Безопасность | да | роли, сессии, ключи интеграции, лимит входов, журнал аудита — «Доступ» ниже |
| Код на GitHub, деплой по ссылке | да | https://swapowl.duckdns.org, CI и автодеплой после зелёного CI |

**Новые возможности**

| Что | Маршрут / экран | Условие |
|---|---|---|
| «Что если» | `POST /api/what-if`, `#/what-if` | роль viewer |
| Помощник | `POST /api/assistant/ask`, бот `/ask`, тема «план на ближайшие дни» | роль viewer |
| Витрина «Спаси продукты» | `GET /api/public/rescue`, `#/rescue` | `PUBLIC_RESCUE=true`, без входа |
| Меню гостя | `GET /api/public/menu`, `#/guest-menu` | `PUBLIC_RESCUE=true`, без входа |
| Вход mini-app | `POST /api/auth/telegram` | подпись Telegram |

## Что умеет

- **Партии и сроки.** Остаток склада — сумма непросроченных партий; списание строго по FEFO; истекающие партии, количество под риском, стоп-лист пересчитываются от остатков.
- **Приёмка и заказ поставщику.** Экран «Приёмка» читает «годен до» из маркировки (DataMatrix / GS1) и фото накладной; позиция без срока не принимается; экран «Поставки» предлагает закупку и создаёт заказ поставщику (`POST /api/supply-orders`, идемпотентно по `id`). Доставка заказа во внешнюю систему — через адаптер ([`docs/iiko.md`](docs/iiko.md)).
- **Горячие предложения правит менеджер, видит зал.** Менеджер закрепляет или скрывает блюдо и добавляет своё с подсказкой («предлагать к обеду»); расчёт при этом не подменяется: добавленные блюда идут отдельным списком, блюдо из стоп-листа не предлагается. Экран «Для зала» — крупные карточки для официанта, обновляются сами (`PUT /api/hot-offers/overrides/{dish_id}`).
- **Приёмка по фото накладной.** Снимок ТОРГ-12/УПД → позиции по справочнику (Claude Haiku 5.5, фасовка пересчитывается) → по каждой строке «принять», «недовоз» или «брак» → поставка только с принятым и акт расхождений поставщику — [`docs/photo-invoice.md`](docs/photo-invoice.md).
- **Штучные продукты и упаковки.** Яйца приходят штуками, а норма — граммы (`piece_weight`); поставка в упаковках (`unit: "pack"`) — [`docs/catalog.md`](docs/catalog.md).
- **Сеть точек.** Один ресторан — один экземпляр; отдельный сервис hub (`cmd/hub`) опрашивает точки и отдаёт сводку, в интерфейсе — экран «Сеть» (виден, когда hub доступен). Много точек на одном сервере — `deploy/scripts/fleet.sh` — [`docs/network.md`](docs/network.md), [`docs/operations.md`](docs/operations.md).
- **Сторож эксплуатации.** `cmd/watchdog` проверяет `/healthz`, очередь исходящих и диск и пишет инженеру в Telegram — [`docs/operations.md`](docs/operations.md). Управляющему сообщения шлёт `cmd/notifier` — [`docs/notify.md`](docs/notify.md).
- **«Что если».** `POST /api/what-if`, экран `#/what-if`: скидка на блюдо, «заказать меньше», изменение спроса — прогноз списаний по сценариям на копии склада, ничего не записывается ([`docs/api.md`](docs/api.md)).
- **Помощник.** `POST /api/assistant/ask` и команда бота `/ask`: ответ на вопрос своими словами по правилам, без модели — стоп-лист, истекающее, что заказать, остаток продукта, порции блюда и тема «план на ближайшие дни» (что истекает и что заказать).
- **Витрина «Спаси продукты» и меню гостя без входа.** `#/rescue` (`GET /api/public/rescue`) и `#/guest-menu` (`GET /api/public/menu`) — только чтение, без партий, остатков и себестоимости. Публичные маршруты работают при `PUBLIC_RESCUE=true`, иначе `404`.
- **Официанту и менеджеру.** Тёмная тема (переключатель в шапке); офлайн-очередь заказов официанта — заказы, созданные без сети, уходят после её возвращения; вход mini-app по подписи Telegram ([`docs/telegram-miniapp.md`](docs/telegram-miniapp.md)); настройки бота `/settings` ([`docs/notify.md`](docs/notify.md)).
- **Формулировка эффекта.** Везде «не спишем ≈ N ₽» — сумма, которую не спишем, а не выручка. На карточке «Горячих предложений» это доля стоимости остатка по действию (скидка 80 %, стафф-питание 100 %, заготовка 70 % — допущения, `SAVE_SHARE` в `realistic.ts`). Цифры питча (6–11 %, +27 тыс. ₽) — из симулятора при допущении, что гость берёт предложенное блюдо в 30 % случаев (измеренной доли нет).
- **Проверка подключения к iiko.** `go run ./cmd/iiko-check -url … -login … -password …` показывает нераспознанные единицы, продукты без срока и недостающие поля — [`docs/onboarding.md`](docs/onboarding.md), п. 3а.

## Интеграции и iiko

Ядро не знает ни об iiko, ни о других системах. Внешняя система общается с ним через REST API по ключу роли `operator` (шлёт поставки, продажи, справочники) и читает стоп-лист и остатки. Адаптер — отдельный процесс; сейчас есть один, под iiko.

**Честно про iiko.** Адаптер работает с iikoServer API и проверен **только на эмуляторе** (`cmd/fake-iiko`); пути и поля, которых нет в документации по памяти авторов, помечены `[ПРЕДПОЛОЖЕНО]` в [`docs/iiko.md`](docs/iiko.md). Против настоящего iiko адаптер не запускался. iiko переносит функционал в iikoCloud API; следующий адаптер будет под него.

## Срок годности

Срок годности — обязательное поле каждой позиции поставки: его даёт поставщик (`expires_on` — «годен до», годен весь этот день по времени ресторана, или `shelf_life_hours`). Позиция без срока отклоняется `400` с полем `items[i].expires_on`, ничего не записывается; срока «по умолчанию» из карточки для поставки нет (так ответил эксперт «Флагмана»). Источники срока: форма приёмки, CSV, фото накладной (строку без срока не принять, пока не введён «годен до» с упаковки), скан DataMatrix / GS1 (AI 17), «Принять предложенное» (срок поставщика из карточки уходит в поставку явно и виден оператору), адаптер учётной системы. Партии со сроком из справочника, принятые до этого правила, подтверждаются в «Приёмке» (`GET /api/receiving`, `PATCH /api/batches/{id}/expiry`).

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

## Принятые решения по неоднозначностям кейса

Полная таблица — `.ai/solution.md`, раздел 5; контракты — `docs/api.md`.

| Вопрос | Решение |
|---|---|
| Срок хранения | В позиции поставки ровно одно из `shelf_life_hours` (1…87600) и `expires_on` (`YYYY-MM-DD`). `expires_at` считается один раз при приёме: `received_at + срок` либо начало следующего дня после `expires_on` по времени ресторана. `received_at` — время стенда в момент приёма; `created_at` — реквизит документа и в расчёт не входит, поэтому пример с `shelf_life_hours` работает при любом времени стенда |
| Единицы | Всё хранится как `int64` в `g`, `ml`, `pcs`. `quantity` на входе — только JSON-строка (`"2.5"`) и `unit` из `g, kg, ml, l, pcs`; JSON-число, экспонента, запятая, дробь мельче базовой единицы, дробные штуки, ноль и значения выше `10^12` — `400`. Float в остатках нет |
| Заказ | Всё или ничего: если не хватает хотя бы одного продукта, заказ отклоняется целиком (`409 insufficient_stock` с перечнем `missing`) и сохраняется как `rejected`. Идемпотентность по `id`: тот же `id` и тело — повтор (`replayed: true`), другое тело — `409 order_conflict` |
| Продажа из кассы | `"source": "pos"`: блюдо в кассе уже продано, отказать нельзя. Списывается всё доступное по FEFO, недостача записывается как расхождение (`shortfalls`), ответ всегда `201` |
| Часовой пояс | Одна зона на экземпляр (`RESTAURANT_TZ`, по умолчанию `Europe/Moscow`). «Годен до даты» — до начала следующего локального дня. Зоны с переводом часов в полночь не поддерживаются |
| Окно опоздания | Необязательное `occurred_at` у поставок и заказов допустимо в пределах `LATE_EVENTS_HOURS` (по умолчанию 2 ч) до «сейчас»; иначе `400` — для старых событий нужна корректировка. «Дворник» списывает просроченную партию не раньше `expires_at` + окно, но остатки и стоп-лист считаются по `now < expires_at` |
| Просрочка | Партия доступна, пока `now < expires_at` — это предикат в каждом запросе, а не фоновый процесс. Списание «дворником» пишется со временем `expires_at` |
| Почему блокируется продукт, а не партия | Все писатели партий (заказ, поставка, «дворник», прыжок времени) сначала берут `SELECT … FOR UPDATE` на затронутые продукты по отсортированному списку id, и только потом читают часы и партии. FEFO и сумма потребности смотрят на все партии продукта сразу, поэтому блокировка одной партии не защитит от двойной продажи и «оживления» просроченной; общий порядок по продукту исключает дедлок. Проверяют сторожа №3 (`tests/invariants/inv03_concurrency_test.go`, 50 параллельных заказов) и №4 (`inv04_wiper_race_test.go`, гонка заказа с «дворником») |

## Три запроса curl

Выполнены против локального стенда с демо-сидом (тот же образ, что на https://swapowl.duckdns.org). Количество — строкой, срок — в часах, поэтому примеры не зависят от времени стенда. Продукт `beef` и блюдо `burger` есть в демо-сиде; на чистом экземпляре поставка с явным `unit` создаёт продукт сама, а блюда заводятся через `PUT /api/catalog`.

```sh
API=http://localhost:8080

# 1. Поставка: 2,5 кг говядины на 72 часа -> 201, "replayed": false, партия 2500 g, expires_at = received_at + 72 ч
curl -i -X POST $API/api/deliveries -H 'Content-Type: application/json' -d '{
  "id": "README-D-1", "number": "1", "created_at": "2026-10-09T08:00:00+03:00",
  "items": [{"product_id": "beef", "name": "Говяжья вырезка", "quantity": "2.5", "unit": "kg", "shelf_life_hours": 72}]}'

# 2. Заказ: 2 бургера -> 201, "status": "accepted", writeoffs по партиям в порядке FEFO (говядина 240 g, булочка 2 pcs, …)
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
| `SIM_SEED` | `2026` | Seed генератора заказов на стенде: заказы зависят только от seed и времени стенда |
| `SIM_FOLLOW_HOT` | `true` | Демо «со SwapOwl и без»: гости генератора выбирают блюда из горячих предложений чаще (`false` — не слушаются). Флаг отдаёт `GET /api/clock` как `follow_hot` |
| `SIM_HOT_UPLIFT` | `1.5` | Подъём веса блюда на первой позиции списка, 1…10; на позиции `i` — `1 + (u−1)/i` |
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

Дальше: [`docs/onboarding.md`](docs/onboarding.md) — регламент подключения ресторана, [`docs/iiko.md`](docs/iiko.md) — адаптер к iiko, [`docs/network.md`](docs/network.md) — сеть точек, [`docs/operations.md`](docs/operations.md) — резервные копии, деплой, `fleet.sh`, сторож, [`docs/notify.md`](docs/notify.md) — уведомления, [`docs/catalog.md`](docs/catalog.md) — справочники, [`docs/demo-script.md`](docs/demo-script.md) — сценарий показа, [`docs/bench.md`](docs/bench.md) — сравнение алгоритмов.

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

## Цифры эффекта

Все цифры — из симулятора, не данные ресторана. **Основная экономия:** списывать первой партию, которая испортится раньше, вместо «как придётся» (повар берёт то сверху, то с дальней полки) — ≈ 6 тыс. ₽ в месяц на точку; «худший случай» (всегда берут последнюю поставку, ≈ 101 тыс. ₽) — только как приложение. Горячие предложения — второй рычаг: на партиях с излишком или коротким сроком у поставщика они снижают списания на 6–11 % (bench, 200 прогонов по 7 дней, против отсутствия подсказок), а при точной закупке — не списания, а отказы гостям: на 16 % меньше, ≈ +27 тыс. ₽ выручки (обе цифры — против той же кухни без подсказок) в месяц. Допущение: гость берёт предложенное блюдо в 30 % случаев, измеренной доли нет — проверим на пилоте. Расчёт и допущения — [`docs/pitch-numbers.md`](docs/pitch-numbers.md) (`go run ./cmd/pitchcalc -days 30 -runs 20`), сравнение алгоритмов — [`docs/bench.md`](docs/bench.md), текст ведущего — [`docs/demo-script.md`](docs/demo-script.md).

Измеренное сравнение алгоритмов: по сумме пяти сценариев — **−10,1 % списаний против случайных рекомендаций, −13,3 % против подсказки «по ближайшему сроку»**, 200 прогонов по 7 дней; воспроизводится одной командой `go run ./cmd/simulator bench -n 200` ([`docs/bench.md`](docs/bench.md)).

## Демо «со SwapOwl и без»

Две точки на одном `SIM_SEED`, отличаются только `SIM_FOLLOW_HOT`. Генератор берёт текущий список горячих предложений (тот же расчёт, что у `GET /api/hot-offers`) и при `true` повышает вес этих блюд при выборе (модель M2 из bench: число гостей фиксировано, меняется только выбор блюда). Склад, FEFO и все инварианты в обеих точках одни и те же. **Величина подъёма (`SIM_HOT_UPLIFT`) выдумана, как и в bench**: утверждается направление эффекта, а не процент. Тест `TestFollowHotReducesWriteoffsSameSeed` гонит 7 суток на одном seed и сравнивает списания в копейках; при одинаковом флаге они совпадают до копейки.
