package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// resetOrder — порядок удаления при сбросе стенда (7.1): сначала зависимые таблицы.
// shortfalls (миграция 0002) ссылается на orders; справочники удаляются после зависимых таблиц.
// Пользователи, сессии, ключи доступа и привязки Telegram (telegram_links) сбросом не затрагиваются.
var resetOrder = []string{
	`DELETE FROM movements`,
	`DELETE FROM shortfalls`,
	`DELETE FROM order_items`,
	`DELETE FROM orders`,
	`DELETE FROM batches`,
	`DELETE FROM deliveries`,
	`DELETE FROM corrections`,
	`DELETE FROM clock_jumps`,
	// Справочники целиком лежат в сиде, а импорт каталога (PUT /api/catalog) мог добавить или
	// переписать блюда, рецепты и продукты: чистим всё и даём сиду загрузить эталон заново.
	`DELETE FROM hot_overrides`,
	`DELETE FROM dish_holds`,
	`DELETE FROM recipe_items`,
	`DELETE FROM dishes`,
	`DELETE FROM products`,
}

// DeleteStandData удаляет данные стенда в транзакции tx (DELETE, не TRUNCATE: тот берёт
// ACCESS EXCLUSIVE и даёт дедлок с заказом, 2.8). Вызывается под затвором на запись.
func DeleteStandData(ctx context.Context, tx pgx.Tx) error {
	for _, q := range resetOrder {
		if _, err := tx.Exec(ctx, q); err != nil {
			return fmt.Errorf("store: reset %q: %w", q, err)
		}
	}
	return nil
}
