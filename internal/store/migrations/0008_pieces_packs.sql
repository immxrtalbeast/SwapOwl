-- Штучные продукты с весовой нормой и упаковки (FS-122, спецификация 8.3 п. 9).
-- piece_weight — сколько g или ml в одной штуке (только у unit = 'pcs'); null — веса нет.
-- pack_unit — метка упаковки («уп», «пачка»); не null означает, что pack_size — содержимое
-- упаковки в базовых единицах и позиции поставки можно вводить в упаковках (unit = 'pack').
-- Остатки, партии и списания по-прежнему только в базовых единицах.
ALTER TABLE products
    ADD COLUMN piece_weight bigint CHECK (piece_weight > 0),
    ADD COLUMN piece_weight_unit text CHECK (piece_weight_unit IN ('g', 'ml')),
    ADD COLUMN pack_unit text CHECK (pack_unit <> '' AND length(pack_unit) <= 32),
    ADD CONSTRAINT products_piece_weight_chk CHECK (
        (piece_weight IS NULL) = (piece_weight_unit IS NULL) AND (piece_weight IS NULL OR unit = 'pcs'));
