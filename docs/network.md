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
| `HUB_AB_POLL` | период опроса точек группы `ab`, по умолчанию 2 с |

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

Необязательные поля точки: `label` — подпись в сравнении («Со SwapOwl», «Без подсказок»), `group` — группа; `ab` — пара для живого A/B (не больше двух точек в группе, иначе конфиг отвергается). Обе попадают в `/api/network/summary`.

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

## Живое A/B-сравнение двух точек

Для точек группы `ab` hub опрашивает точку каждые `HUB_AB_POLL` (по умолчанию 2 с) и добавляет в её снимок поле `run` — списания в ₽ по часам времени стенда **с начала текущего прогона**:

```json
"run": {"running": true, "started_at": "…", "stand_time": "…", "total_minor": 820000,
        "series": [{"hour": 0, "cost_minor": 0}, {"hour": 1, "cost_minor": 210000}, {"hour": 2.5, "cost_minor": 820000}]}
```

`hour` — часы стенда с начала прогона (у обеих точек ось общая), `cost_minor` — накопленные списания, копейки. Данные — из `GET /api/clock` и `GET /api/analytics/writeoffs` точки (по окну `[from, to)` на каждый час стенда; закрытые часы читаются один раз). API точки не менялся.

Прогон начинается, когда hub впервые видит идущий генератор (`simulator_running`): начало — целый час стенда этого момента. Часы стенда пошли назад (сброс) — прогон обнуляется. Пауза прогон не стирает: ряд остаётся до сброса. Вне группы `ab` поля `run` нет.

Экран «Сеть»: при группе `ab` из двух точек сверху блок сравнения — две цифры «списано, ₽», разница `−N ₽ (−X %)` (базовая точка — с подписью на «Без…», иначе вторая в конфиге; отрицательная разница — SwapOwl сэкономил), рукописная пометка и два графика на одной оси. Пока идёт прогон, экран обновляется раз в 2,5 с, иначе — раз в минуту.

### Как поднять две точки и запустить сравнение

```bash
fleet.sh up with-owl && fleet.sh up without-owl            # две точки на сервере
fleet.sh hub-key with-owl <<<"$VIEWER_KEY_1"                # viewer-ключи для hub (см. выше)
fleet.sh hub-key without-owl <<<"$VIEWER_KEY_2"
fleet.sh ab-set with-owl "Со SwapOwl"                       # группа ab + подпись (пишет HUB_GROUP/HUB_LABEL в .env точки)
fleet.sh ab-set without-owl "Без подсказок"
fleet.sh ab-start 24        # сброс обеих, скорость ×600, одновременный старт, пауза через 24 ч стенда
fleet.sh ab-stop            # остановить раньше
```

`ab-start [часов]` берёт `DEMO_TOKEN` каждой точки из её `.env`: `reset`, `clock/speed` (`AB_SPEED`, по умолчанию 600), затем параллельно включает генератор на обеих точках и печатает разницу моментов старта (цель < 1 с). Через `часов × 3600 / скорость` секунд реального времени генератор выключается и часы стенда останавливаются; ожидание идёт фоном, его завершает `ab-stop`. Адрес точки — `https://<slug>.<SITE_DOMAIN>` (для `localhost` — `http`), переопределяется `AB_URL_<SLUG>` (дефис → `_`, верхний регистр).

`POST /api/demo/simulator` не использует `DEMO_TOKEN`, а требует роль operator. При `AUTH_REQUIRED=false` скрипту хватает токена; иначе задайте ключ operator в `AB_SIM_KEY`.

Если у точки нет флага `SIM_FOLLOW_HOT` (отдельный тикет), точки различаются только подписью.

UI и деплой — отдельными тикетами.
