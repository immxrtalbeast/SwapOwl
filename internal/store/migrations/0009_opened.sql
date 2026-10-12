-- Вскрытая упаковка (FS-139). Срок на упаковке — для закрытой; после вскрытия действует свой
-- срок (молоко и сливки 48 ч, соусы 72 ч), и его маркируют (СанПиН, ХАССП).
-- products.shelf_after_open_hours — срок после вскрытия в часах; null — вскрытие срок не меняет.
-- batches.parent_batch_id и opened_at — у дочерней партии, выделенной из исходной вскрытием;
-- оба null у обычных партий, оба заданы у дочерних. Номер миграции закреплён за FS-139.
ALTER TABLE products
    ADD COLUMN shelf_after_open_hours int CHECK (shelf_after_open_hours BETWEEN 1 AND 87600);

ALTER TABLE batches
    ADD COLUMN parent_batch_id bigint REFERENCES batches (id),
    ADD COLUMN opened_at timestamptz,
    ADD CONSTRAINT batches_opened_chk CHECK ((parent_batch_id IS NULL) = (opened_at IS NULL));

ALTER TABLE corrections DROP CONSTRAINT corrections_kind_check;
ALTER TABLE corrections
    ADD CONSTRAINT corrections_kind_check CHECK (kind IN ('writeoff', 'inventory', 'cancel', 'open'));
