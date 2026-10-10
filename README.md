# umirhack-2026

## Доступ (FS-44)

Правила — `.ai/solution.md`, раздел 2.9; эндпоинты и коды — 7.4.

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `AUTH_REQUIRED` | `true` | При `false` запрос без учётных данных идёт с ролью `operator`; при `true` — `401` на `/api/*` и `/events` (кроме `/healthz` и `POST /api/auth/login`) |
| `ADMIN_LOGIN`, `ADMIN_PASSWORD` | — | Первый администратор; создаётся при старте, если таблица пользователей пуста. При `AUTH_REQUIRED=true`, пустой таблице и без этих переменных сервис не запускается |
| `SESSION_TTL_HOURS` | `12` | Срок сессии |
| `SITE_DOMAIN` | — | Если задан, cookie `session` ставится с `Secure`; для локального HTTP не задавайте |

- Роли: `viewer` (чтение, `/events`), `operator` (плюс изменяющие запросы), `admin` (плюс `/api/users`, `/api/api-keys`).
- Вход: `POST /api/auth/login` `{"login","password"}` → cookie `session`; `POST /api/auth/logout`; `GET /api/auth/me`. Больше 5 неудачных входов по логину за 15 минут — `429 too_many_attempts`.
- Ключи интеграции: `Authorization: Bearer <ключ>`; создаёт и отзывает admin через `/api/api-keys` (ключ показывается один раз).
- Изменяющие запросы принимаются только с `Content-Type: application/json`.
- В журнале на каждый запрос — роль и id пользователя или ключа; записи аудита помечены `audit=true`. Пароли, токены и ключи в журнал не попадают.

## Заказы

- `POST /api/orders` `{"id": "<UUID>", "items": [{"dish_id", "portions"}]}` — потребность суммируется по всем блюдам; заказ списывает всё по FEFO либо ничего. `201` — принят (`writeoffs`: партия — количество), `409 insufficient_stock` — отклонён с перечнем `missing` (заказ сохраняется как `rejected`), повтор того же id и тела — `200`/`409` с `replayed: true`, другое тело — `409 order_conflict`.
- `GET /api/orders?status=accepted|rejected&limit=` — история; `GET /api/dishes` — блюда с рецептами.
- Партия доступна при `now < expires_at` независимо от фонового прохода.
