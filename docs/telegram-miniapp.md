# Вход в mini-app через Telegram (FS-163)

Mini-app менеджера и официанта открывается из бота и входит без пароля: фронтенд отдаёт API
`Telegram.WebApp.initData`, API проверяет подпись и открывает обычную сессию.

## Включение

Задайте API `TELEGRAM_BOT_TOKEN` — токен **того бота, из которого открывается mini-app**
(не токен уведомлений `notifier`, если это другой бот). Без токена маршруты ниже не регистрируются.
Токен секретный: в журнал, ответы и аудит не попадает.

## Вход

`POST /api/auth/telegram` (публичный, как `/api/auth/login`):

```json
{ "init_data": "<Telegram.WebApp.initData как есть>" }
```

Проверка — по [документации Telegram](https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app):
`secret = HMAC_SHA256("WebAppData", TELEGRAM_BOT_TOKEN)`, `hash = hex(HMAC_SHA256(secret, data_check_string))`,
сравнение за постоянное время. `auth_date` не старше 24 часов по *реальным* часам сессий (`auth.Options.Now`), а не по ускоренным часам стенда (`CLOCK_MODE=simulated`): Telegram ставит настоящее время.

| Ответ | Когда |
|---|---|
| `200` | как `GET /api/auth/me` (`login`, `role`, `auth_required`) и cookie `session` (те же флаги и срок, что у `login`) |
| `400 validation_error` | нет `init_data` или тело не JSON |
| `401 unauthorized` | подпись неверна, `auth_date` старше 24 ч или «из будущего» (больше 5 минут) |
| `403 telegram_not_linked` | подпись верна, но аккаунт не привязан; `error.details.telegram_user_id` — id для привязки |

Повтор одних и тех же `init_data` в пределах 24 ч снова входит и выдаёт новую сессию (так устроена
подпись Telegram, одноразовости в ней нет); защита от чужого перехвата — срок `auth_date`.

**Демо-стенд** (`AUTH_REQUIRED=false`): подпись проверяется так же, но после неё API отвечает
гостем-оператором (`login: null`, `role: operator`, `auth_required: false`) без cookie и без привязки.

## Поддерживаемые клиенты

Только нативные клиенты Telegram (iOS, Android, Desktop). Cookie `session` имеет `SameSite=Strict`
и не уйдёт из iframe `web.telegram.org`, поэтому Telegram Web не поддерживается.

## Привязка аккаунта (admin)

- `PUT /api/users/{login}/telegram` `{ "telegram_user_id": 123456789 }` — привязать (заменяет прежнюю
  привязку пользователя). `404` — нет пользователя, `409` — аккаунт уже у другого пользователя.
- `DELETE /api/users/{login}/telegram` — снять (`204`, `404` если привязки не было).

Один аккаунт Telegram — один пользователь, у пользователя не больше одного аккаунта. Привязка удаляется
вместе с пользователем. Сброс стенда её не трогает.

Как привязать: сотрудник открывает mini-app → получает `403 telegram_not_linked` с `telegram_user_id`
→ админ вызывает `PUT` с этим id → сотрудник открывает mini-app снова.

## Аудит

`telegram_login_ok`, `telegram_login_failed` (с причиной), `telegram_login_unlinked`, `telegram_linked`,
`telegram_unlinked`. `init_data` и токен в аудит не пишутся.
