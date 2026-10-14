-- Ручная правка горячих предложений менеджером (FS-159): закрепить блюдо (pin) или скрыть его (hide).
-- Одна запись на блюдо; блюдо удалено из справочника — запись уходит вместе с ним.
CREATE TABLE hot_overrides (
    dish_id text        PRIMARY KEY REFERENCES dishes (id) ON DELETE CASCADE,
    mode    text        NOT NULL CHECK (mode IN ('pin', 'hide')),
    note    text        NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    set_at  timestamptz NOT NULL,
    set_by  text        NOT NULL DEFAULT ''
);
