-- Схема по спецификация, раздел 7.1. Времена — timestamptz, количества — bigint
-- в базовой единице продукта, деньги — bigint в копейках.
-- movements.kind и movements.reason намеренно без CHECK и enum (раздел 8.2).

CREATE TABLE products (
    id                    text PRIMARY KEY,
    name                  text   NOT NULL,
    unit                  text   NOT NULL CHECK (unit IN ('g', 'ml', 'pcs')),
    price_minor_per_1000  bigint NOT NULL DEFAULT 0 CHECK (price_minor_per_1000 >= 0),
    lead_days             int    NOT NULL DEFAULT 1 CHECK (lead_days >= 0),
    pack_size             bigint NOT NULL DEFAULT 1 CHECK (pack_size > 0),
    auto_created          boolean NOT NULL DEFAULT false
);

CREATE TABLE dishes (
    id           text PRIMARY KEY,
    name         text    NOT NULL,
    price_minor  bigint  NOT NULL DEFAULT 0 CHECK (price_minor >= 0),
    prior_daily  numeric NOT NULL DEFAULT 0 CHECK (prior_daily >= 0)
);

CREATE TABLE recipe_items (
    dish_id     text   NOT NULL REFERENCES dishes (id),
    product_id  text   NOT NULL REFERENCES products (id),
    quantity    bigint NOT NULL CHECK (quantity > 0),
    PRIMARY KEY (dish_id, product_id)
);

CREATE TABLE deliveries (
    id           text PRIMARY KEY,
    number       text        NOT NULL,
    created_at   timestamptz NOT NULL,
    received_at  timestamptz NOT NULL,
    body_hash    bytea       NOT NULL,
    response     jsonb       NOT NULL
);

CREATE TABLE batches (
    id                    bigserial PRIMARY KEY,
    delivery_id           text        NOT NULL REFERENCES deliveries (id),
    line_no               int         NOT NULL,
    product_id            text        NOT NULL REFERENCES products (id),
    quantity              bigint      NOT NULL,
    remaining             bigint      NOT NULL,
    received_at           timestamptz NOT NULL,
    expires_at            timestamptz NOT NULL,
    expiring_notified_at  timestamptz,
    written_off_at        timestamptz,
    UNIQUE (delivery_id, line_no),
    CHECK (remaining >= 0 AND remaining <= quantity),
    CHECK (expires_at > received_at OR written_off_at IS NOT NULL)
);

CREATE INDEX batches_product_fefo_idx
    ON batches (product_id, expires_at, id) WHERE remaining > 0;

CREATE TABLE orders (
    id         uuid PRIMARY KEY,
    body_hash  bytea       NOT NULL,
    status     text        NOT NULL CHECK (status IN ('accepted', 'rejected')),
    at         timestamptz NOT NULL,
    response   jsonb       NOT NULL
);

CREATE TABLE order_items (
    order_id  uuid NOT NULL REFERENCES orders (id),
    dish_id   text NOT NULL REFERENCES dishes (id),
    portions  int  NOT NULL CHECK (portions > 0),
    was_hot   boolean NOT NULL DEFAULT false,
    PRIMARY KEY (order_id, dish_id)
);

CREATE TABLE movements (
    id          bigserial PRIMARY KEY,
    batch_id    bigint      NOT NULL REFERENCES batches (id),
    product_id  text        NOT NULL REFERENCES products (id),
    kind        text        NOT NULL,
    reason      text,
    quantity    bigint      NOT NULL CHECK (quantity > 0),
    order_id    uuid REFERENCES orders (id),
    at          timestamptz NOT NULL
);

CREATE TABLE clock_state (
    id          boolean PRIMARY KEY DEFAULT true CHECK (id),
    sim_base    timestamptz NOT NULL,
    wall_base   timestamptz NOT NULL,
    speed       numeric     NOT NULL DEFAULT 1 CHECK (speed >= 0),
    sim_saved   timestamptz NOT NULL
);

CREATE TABLE clock_jumps (
    from_at  timestamptz NOT NULL,
    to_at    timestamptz NOT NULL
);

CREATE TABLE users (
    id             bigserial PRIMARY KEY,
    login          text NOT NULL UNIQUE,
    password_hash  text NOT NULL,
    role           text NOT NULL CHECK (role IN ('viewer', 'operator', 'admin')),
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash  bytea PRIMARY KEY,
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at  timestamptz NOT NULL
);

CREATE TABLE api_keys (
    id          bigserial PRIMARY KEY,
    name        text  NOT NULL,
    key_hash    bytea NOT NULL UNIQUE,
    role        text  NOT NULL CHECK (role IN ('viewer', 'operator')),
    created_at  timestamptz NOT NULL DEFAULT now()
);
