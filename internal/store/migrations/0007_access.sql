-- Доступ в эксплуатации (.ai/solution.md, 2.9 и 8.3 п. 6): счётчик неудачных входов и журнал
-- действий живут в БД, а не в памяти процесса. Сброс стенда эти таблицы не трогает.

CREATE TABLE login_attempts (
    id     bigserial PRIMARY KEY,
    login  text        NOT NULL,
    at     timestamptz NOT NULL
);
CREATE INDEX login_attempts_login_at_idx ON login_attempts (login, at);
CREATE INDEX login_attempts_at_idx ON login_attempts (at);

CREATE TABLE audit_log (
    id       bigserial PRIMARY KEY,
    at       timestamptz NOT NULL DEFAULT now(),
    actor    text        NOT NULL,
    action   text        NOT NULL,
    details  jsonb       NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX audit_log_at_idx ON audit_log (at DESC, id DESC);
