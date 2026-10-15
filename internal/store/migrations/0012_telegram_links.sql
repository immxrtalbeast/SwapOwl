-- Вход в mini-app через Telegram (FS-163): привязка Telegram-аккаунта к пользователю.
-- Один Telegram-аккаунт — один пользователь (PK), и у пользователя не больше одного аккаунта (UNIQUE).
-- Привязку задаёт admin (PUT /api/users/{login}/telegram); удаление пользователя снимает её каскадом.
-- Сброс стенда эту таблицу не трогает: пользователи при сбросе остаются.

CREATE TABLE telegram_links (
    telegram_user_id bigint PRIMARY KEY,
    login            text NOT NULL UNIQUE REFERENCES users (login) ON DELETE CASCADE,
    created_at       timestamptz NOT NULL DEFAULT now()
);
