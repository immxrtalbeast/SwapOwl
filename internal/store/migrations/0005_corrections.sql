-- Корректировки остатка (брак, инвентаризация, отмена заказа): таблица идемпотентности.
-- Ключ — (kind, id): у каждой операции свой id. Отмена заказа допустима один раз:
-- subject — id заказа, уникален среди отмен. Номер миграции закреплён за FS-78.
CREATE TABLE corrections (
    kind       text        NOT NULL CHECK (kind IN ('writeoff', 'inventory', 'cancel')),
    id         text        NOT NULL,
    subject    text        NOT NULL,
    body_hash  bytea       NOT NULL,
    at         timestamptz NOT NULL,
    response   jsonb       NOT NULL,
    PRIMARY KEY (kind, id)
);

CREATE UNIQUE INDEX corrections_cancel_once_idx ON corrections (subject) WHERE kind = 'cancel';
