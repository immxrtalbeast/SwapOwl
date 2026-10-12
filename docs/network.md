# Сеть ресторанов: hub (`cmd/hub`)

Один экземпляр SwapOwl — один ресторан. Hub — отдельный сервис, который раз в `HUB_POLL` обходит точки по их HTTP API и отдаёт общую сводку. Контракты API экземпляров не меняются.

```
 точка A (/api, read-only ключ) ─┐
 точка B                         ├─ hub (опрос, параллельно, таймаут на точку) ─ GET /api/network/* ─ клиент
 точка C                         ┘
```

## Запуск

| Переменная | Назначение |
|---|---|
| `HUB_CONFIG` | путь к файлу со списком точек (обязательно) |
| `HUB_TOKEN` | Bearer-токен самого hub (обязательно) |
| `HUB_POLL` | период опроса: `45s`, `2m` или число секунд; по умолчанию 60 с |
| `HUB_ADDR` | адрес прослушивания, по умолчанию `:8090` |

Таймаут опроса точки — половина `HUB_POLL`, не больше 10 с. Ключи в логи не пишутся.

## Конфиг

YAML (поддерживается только этот плоский вид) или JSON:

```yaml
points:
  - id: arbat
    name: Арбат
    url: https://arbat.example.com
    api_key: sk_...            # либо api_key_env: ARBAT_KEY — ключ из окружения
  - id: tverskaya
    name: Тверская
    url: https://tver.example.com
    api_key_env: TVER_KEY
```

JSON: `{"points":[{"id":"arbat","name":"Арбат","url":"...","api_key":"..."}]}`. `id` уникален и без `/`, пробелов, `?`, `#`. Файл с ключами держите с правами `0600`.

## Как выдать hub ключ точки

На каждой точке администратор открывает «Пользователи и ключи» (`POST /api/api-keys`) и создаёт ключ с ролью **viewer** (read-only; hub делает только GET). Значение показывается один раз — кладётся в `api_key` или в переменную из `api_key_env`. Отзыв — `DELETE /api/api-keys/{id}`: точка получит статус `unauthorized`.

## Откуда цифры (маршруты точки)

`GET /api/clock` (время стенда, пояс), `/api/alerts` (истекающие и просроченные партии), `/api/stop-list`, `/api/hot-offers` (берётся первое), `/api/analytics/writeoffs` (`total_minor`, копейки; «сегодня» — сутки стенда по поясу точки, «7 дней» — шесть предыдущих суток и сегодня).

**Чего не хватает в API точки:** цены/стоимости партии. `/api/alerts` и `/api/batches` отдают остаток, но не деньги (цена `price_minor_per_1000` есть только на записи `PUT /api/catalog`). Поэтому `expiring_sum_minor` всегда `null`. Нужно поле `value_minor` (стоимость остатка, копейки) в `Alert` либо `GET /api/catalog` с ценами.

## API hub

Авторизация: `Authorization: Bearer $HUB_TOKEN` (кроме `/healthz`).

- `GET /healthz` → `{"status":"ok"}`.
- `GET /api/network/summary` → `{"points":[…],"totals":{…}}`.
- `GET /api/network/points/{id}` → то же по одной точке плюс ленты `alerts` и `stop_list`; `404 unknown_point`.

Точка:

```json
{
  "id": "arbat", "name": "Арбат",
  "status": "ok",
  "last_attempt_at": "…", "last_success_at": "…", "stand_time": "…",
  "expiring_batches": 2, "expired_batches": 0, "expiring_sum_minor": null,
  "stop_list_count": 1, "stop_list_names": ["Паста с лососем"],
  "writeoffs_today_minor": 1500, "writeoffs_7d_minor": 9900,
  "top_hot_offer": {"dish_id": "salmon-steak", "dish_name": "Стейк из лосося", "explanation": "…"}
}
```

`status`: `ok`; `unreachable` — нет соединения или таймаут; `unauthorized` — точка ответила 401/403; `error` — иной ответ. Не-`ok` точка не роняет сводку: цифры пустые, `error` — краткая причина, `last_success_at` — последний успешный опрос (или `null`). Деньги — копейки.

`totals`: `points`, `ok`, `unavailable`, суммы `expiring_batches`, `stop_list_count`, `writeoffs_today_minor`, `writeoffs_7d_minor` — только по точкам со статусом `ok`.

UI и деплой — отдельными тикетами.
