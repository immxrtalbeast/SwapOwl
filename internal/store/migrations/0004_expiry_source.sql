-- Источник срока годности партии (2.1а): delivery — срок пришёл в поставке,
-- product_default — взят срок хранения продукта, confirmed — подтверждён кладовщиком.
-- Номер миграции закреплён за FS-79.
ALTER TABLE batches
    ADD COLUMN expiry_source text NOT NULL DEFAULT 'delivery'
    CHECK (expiry_source IN ('delivery', 'product_default', 'confirmed'));

CREATE INDEX batches_unconfirmed_idx ON batches (received_at) WHERE expiry_source = 'product_default';
