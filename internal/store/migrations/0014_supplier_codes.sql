-- Сопоставление кодов поставщика (GTIN или артикул) с продуктом справочника: общее для всех устройств приёмки.
-- Раньше «GTIN → продукт» жил в localStorage одного устройства. Один код — один продукт; продукт удалён — запись уходит.
CREATE TABLE supplier_codes (
    code       text        PRIMARY KEY CHECK (length(code) BETWEEN 1 AND 64),
    product_id text        NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    set_at     timestamptz NOT NULL,
    set_by     text        NOT NULL DEFAULT ''
);
CREATE INDEX supplier_codes_product ON supplier_codes (product_id);
