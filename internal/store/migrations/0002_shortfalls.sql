-- Продажа из кассы (source: pos): недостача фиксируется расхождением, а не отказом.
ALTER TABLE orders ADD COLUMN source text NOT NULL DEFAULT 'api' CHECK (source IN ('api', 'pos'));

CREATE TABLE shortfalls (
    order_id    uuid        NOT NULL REFERENCES orders (id),
    product_id  text        NOT NULL REFERENCES products (id),
    quantity    bigint      NOT NULL CHECK (quantity > 0),
    at          timestamptz NOT NULL,
    PRIMARY KEY (order_id, product_id)
);
