-- Срок хранения по умолчанию у продукта (спецификация, 8.3 п. 3): позиция поставки
-- без shelf_life_hours и expires_on берёт его; null — срока по умолчанию нет.
ALTER TABLE products ADD COLUMN shelf_life_hours int CHECK (shelf_life_hours BETWEEN 1 AND 87600);
