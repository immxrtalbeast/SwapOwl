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

OLAP: фильтр `OpenDate.Typed` типа `DateRange` обязателен; группировки `DishId`, `DishName`, `OpenDate.Typed`; агрегаты `DishAmountInt`, `DishSumInt`. Прочие поля — `400`.

## [ПРЕДПОЛОЖЕНО]

- **Стоп-лист.** В iikoServer записи стоп-листа не нашлось (стоп-листы — в iikoCloud `/api/1/stop_lists`, с организациями и терминальными группами). Фейк даёт простой путь `/resto/api/v2/stopLists` с телом `{"items":[{"productId","balance"}]}`; `POST` заменяет список целиком, id проверяются по справочнику. Реальный путь и форма для подключения — уточнить.
- **Срок годности.** В iiko партионных сроков нет (`solution.md`, 8.3 п. 3). Фейк добавляет в продукт нестандартное поле `shelfLifeHours` из сида, иначе адаптеру нечего подставить в `expires_at`.
- **Остатки.** Остаток = приход − расход по техкартам с `-origin` до `timestamp`, не ниже нуля; сроки годности не учитываются. Склад один (`main-store`), поставщик один (`supplier-1`).
- **Продажи.** Только OLAP по заказам из генератора; выгрузка заказов (`/api/1/deliveries`, `orders`) не реализована. Цена продажи = цена блюда из сида.
- **Накладные.** Состав полей `document` сокращён (id, номер, дата, статус, поставщик, склад, позиции `product/amount/price/sum`). Цена — по сидовой цене продукта.
- **Токен.** Детерминированный (из seed, логина и пароля), не истекает; в настоящем iiko токен привязан к лицензии и сессии.
- **Техкарты.** Одна на блюдо, без потерь при обработке (`amountIn=amountMiddle=amountOut`).
