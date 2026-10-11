# Фейковый iiko (`cmd/fake-iiko`)

Сервер повторяет подмножество iikoServer API, чтобы адаптер (план 3.6) писался без доступа к настоящему iiko. Позиционирование — `.ai/solution.md`, 8.0. Ядро сервиса фейк не знает и не вызывает. Типы формата — `internal/iiko/model.go`, сервер — `internal/iiko/fake`.

**Источники.** Пути и формы ответов взяты из публичной документации iiko (https://ru.iiko.help/articles/#!api-documentation/, https://api-ru.iiko.services/) по памяти автора; при подготовке задачи сверка со страницами документации не выполнялась (внешнего доступа не было), поэтому все детали формата следует считать требующими сверки. Отдельно помечено `[ПРЕДПОЛОЖЕНО]` то, чего в документации, по нашим сведениям, нет.

## Запуск

```
FAKE_IIKO_LOGIN=demo FAKE_IIKO_PASSWORD=demo go run ./cmd/fake-iiko -addr :8081 -seed 1 -origin 2026-01-01
```

Логин и пароль задаются флагами или переменными окружения; в коде их нет.

## Данные и время

- Справочники — `internal/seed`: 12 продуктов (`GOODS`) и 6 блюд (`DISH`). `id` = внешний id из сида (в настоящем iiko это GUID).
- Накладные и продажи — `internal/generator.Generate(seed, from, to)`. Часовой пояс — ресторана (Europe/Moscow).
- Сервер не вызывает `time.Now()`: время — параметры запроса (`from`/`to`/`timestamp`). Один интервал — не более 366 суток.
- Количества отдаются в основной единице продукта (г→кг, мл→л, шт→шт), как в iiko.

## Эндпоинты

Все, кроме `auth`, требуют `?key=<токен>`; без него или с чужим — `401`.

| Метод и путь | Назначение | Формат |
|---|---|---|
| `POST /resto/api/auth?login=&pass=` | токен; `pass` — SHA-1 пароля в hex | text/plain |
| `POST /resto/api/logout?key=` | выход (токен остаётся валидным) | text/plain |
| `GET /resto/api/v2/entities/products/list?key=` | номенклатура продуктов и блюд | JSON |
| `GET /resto/api/v2/assemblyCharts/getAll?key=` | техкарты (`dateFrom`/`dateTo` принимаются и игнорируются) | JSON |
| `GET /resto/api/documents/export/incomingInvoice?key=&from=yyyy-MM-dd&to=yyyy-MM-dd` | приходные накладные; `to` включительно | XML |
| `POST /resto/api/v2/reports/olap?key=` | продажи, `reportType=SALES` | JSON |
| `GET /resto/api/v2/reports/balance/stores?key=&timestamp=yyyy-MM-ddTHH:mm:ss[&product=]` | остатки на момент | JSON |
| `GET`/`POST /resto/api/v2/stopLists?key=` | чтение / замена стоп-листа | JSON |

OLAP: интервал `DateRange` — `To` **не входит** (в накладных `to` входит по дню). Фильтр `OpenDate.Typed` типа `DateRange` обязателен; группировки `DishId`, `DishName`, `OpenDate.Typed`; агрегаты `DishAmountInt`, `DishSumInt`. Прочие поля — `400`.

## [ПРЕДПОЛОЖЕНО]

- **Стоп-лист.** В iikoServer записи стоп-листа не нашлось (стоп-листы — в iikoCloud `/api/1/stop_lists`, с организациями и терминальными группами). Фейк даёт простой путь `/resto/api/v2/stopLists` с телом `{"items":[{"productId","balance"}]}`; `POST` заменяет список целиком, id проверяются по справочнику. Реальный путь и форма для подключения — уточнить.
- **Срок годности.** В iiko партионных сроков нет (`solution.md`, 8.3 п. 3). Фейк добавляет в продукт нестандартное поле `shelfLifeHours` из сида, иначе адаптеру нечего подставить в `expires_at`.
- **Остатки.** `balance` считает историю от `-origin`, а интервал ограничен 366 сутками: при `timestamp` позже origin+366 суток ответ `400` — сдвигайте `-origin`. Остаток = приход − расход по техкартам с `-origin` до `timestamp`, не ниже нуля после каждого расхода (дефицит не «съедает» будущие поставки); сроки годности не учитываются. Склад один (`main-store`), поставщик один (`supplier-1`).
- **Продажи.** Только OLAP по заказам из генератора; выгрузка заказов (`/api/1/deliveries`, `orders`) не реализована. Цена продажи = цена блюда из сида.
- **Накладные.** Состав полей `document` сокращён (id, номер, дата, статус, поставщик, склад, позиции `product/amount/price/sum`). Цена — по сидовой цене продукта.
- **Токен.** Детерминированный (из seed, логина и пароля), не истекает; в настоящем iiko токен привязан к лицензии и сессии.
- **Техкарты.** Одна на блюдо, без потерь при обработке (`amountIn=amountMiddle=amountOut`).

# Адаптер (`cmd/iiko-adapter`)

Отдельный процесс: ходит в iiko (сейчас — `cmd/fake-iiko`) и в наш API по ключу `Authorization: Bearer`; о БД ничего не знает (`.ai/solution.md`, 8.0–8.2). Клиент iiko — `internal/iiko/client`, переводы полей — `internal/iiko/mapping`.

## Запуск

```
IIKO_URL=http://localhost:8081 IIKO_LOGIN=demo IIKO_PASSWORD=demo \
API_URL=http://localhost:8080 API_KEY=<ключ operator или admin> \
go run ./cmd/iiko-adapter          # цикл; флаг -once — один проход
```

Один проход: справочники → накладные → продажи → стоп-лист. Сбой этапа не останавливает остальные; процесс повторяет проход через `ADAPTER_INTERVAL`.

## Переменные окружения

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `IIKO_URL`, `IIKO_LOGIN`, `IIKO_PASSWORD` | — (обязательны) | доступ к iiko |
| `API_URL`, `API_KEY` | — (обязательны) | наш API и ключ; `PUT /api/catalog` требует `admin` или ключ `operator` |
| `ADAPTER_STATE_FILE` | `iiko-adapter-state.json` | файл курсоров |
| `ADAPTER_INTERVAL` | `5m` | пауза между проходами |
| `ADAPTER_TZ` | `Europe/Moscow` | часовой пояс ресторана |
| `ADAPTER_START_DATE` | вчера | `yyyy-MM-dd`, с какого дня читать при пустом курсоре |
| `ADAPTER_DEFAULT_PRICE_MINOR_PER_1000` | `10000` | цена продукта, если в накладных за 30 дней её нет |
| `ADAPTER_DEFAULT_LEAD_DAYS` | `2` | срок поставки продукта |
| `ADAPTER_DEFAULT_PACK_SIZE` | `1000` | кратность заказа поставщику |
| `ADAPTER_DEFAULT_PRIOR_DAILY` | `10` | априорный дневной спрос блюда |

## Соответствие полей

| iiko | Наш API |
|---|---|
| `products/list`, `type=GOODS` → `id`, `name`, `mainUnit` (кг/л/шт), `shelfLifeHours` | `PUT /api/catalog` `products[]`: `id`, `name`, `unit` (g/ml/pcs), `shelf_life_hours` |
| цена продукта: последняя `price` позиции накладной за 30 дней (руб за основную единицу) | `price_minor_per_1000` = цена·100·1000 / (1000 для кг/л, 1 для шт) |
| `type=DISH` → `id`, `name`, `defaultSalePrice` | `dishes[]`: `id`, `name`, `price_minor` (копейки) |
| действующая техкарта (`dateTo=null`): `items[].productId`, `amountIn` / `assembledAmount` | `recipe[]`: `product_id`, `quantity` (целое число базовых единиц, не меньше 1), `unit` (g/ml/pcs) |
| приходная накладная (`status=PROCESSED`): `id`, `documentNumber`, `dateIncoming`, `items[]` | `POST /api/deliveries`: `id` (= id накладной), `number`, `created_at`, `items[]`: `product_id`, `quantity` (целые базовые единицы), `unit` (g/ml/pcs), `shelf_life_hours` |
| OLAP `SALES` за закрытые дни: `DishId`, `OpenDate.Typed`, `DishAmountInt` | `POST /api/orders`, `source: "pos"`: один заказ на день, `items[]`: `dish_id`, `portions`; `id` = UUID v5 от `iiko-sales:<yyyy-MM-dd>` |
| `GET /api/stop-list` (блюда с `available=false`) | `POST stopLists`: `items[]`: `productId` = id блюда, `balance` = 0 (заменяет список целиком, только при изменении) |

Не передаются: удалённые позиции, блюда без действующей техкарты или с неизвестным продуктом в ней, накладные не в статусе `PROCESSED`, позиции с неизвестным продуктом (всё это — строкой в журнале).

## Курсоры и повторы

Курсоры — даты `yyyy-MM-dd` в файле состояния: `invoices` (последний прочитанный день; сегодняшний день читается заново) и `sales` (первый непереданный день). Продажи передаются только за закрытые дни. Курсор двигается после успешной передачи; повтор после сбоя безопасен: поставка с тем же id и заказ с тем же id принимаются API повторно (`200`). Ответ `4xx` (кроме 401, 403, 429), например `409` при том же id и другом содержимом или `400` на невалидную позицию, пишется в журнал, элемент пропускается, курсор идёт дальше; сеть и `5xx` останавливают проход, повтор на следующем. Продажи блюд, которых нет в принятом каталоге, пропускаются с записью в журнал.

## Допущения

- В OLAP-отчёте нет id заказов iiko, поэтому заказ `pos` — сводный за день, а не по чекам.
- Время события в API — время приёма (`.ai/solution.md`, 8.3 п. 1): история, загруженная пакетом, получает время загрузки.
- `lead_days`, `pack_size`, `prior_daily` в iiko нет — берутся из переменных `ADAPTER_DEFAULT_*`; каждый проход обновляет каталог (в том числе цены) теми же правилами.
- Стоп-лист в iiko — `[ПРЕДПОЛОЖЕНО]` (см. выше); блюда передаются как `productId`.
