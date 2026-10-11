-- Исходящие сообщения для адаптера (.ai/solution.md, 8.0а): ядро копит их, адаптер забирает
-- по порядку и подтверждает доставку. kind и payload — наши, об iiko ядро не знает.
CREATE TABLE outbox (
    id           bigserial PRIMARY KEY,
    kind         text        NOT NULL,
    payload      jsonb       NOT NULL,
    created_at   timestamptz NOT NULL,
    delivered_at timestamptz,
    attempts     int         NOT NULL DEFAULT 0,
    last_error   text
);

-- Адаптер читает только недоставленные.
CREATE INDEX outbox_pending_idx ON outbox (id) WHERE delivered_at IS NULL;
