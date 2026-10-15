-- Ручная пауза блюда менеджером: повар или менеджер снимает блюдо с продажи вне зависимости от остатков.
-- Автоматический стоп-лист по остаткам по-прежнему нигде не хранится; пауза — отдельное решение, как hot_overrides.
-- Одна запись на блюдо; блюдо удалено из справочника — запись уходит вместе с ним.
CREATE TABLE dish_holds (
    dish_id text        PRIMARY KEY REFERENCES dishes (id) ON DELETE CASCADE,
    note    text        NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    set_at  timestamptz NOT NULL,
    set_by  text        NOT NULL DEFAULT ''
);
