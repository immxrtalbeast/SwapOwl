package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// resetOrder — порядок удаления при сбросе стенда (7.1): сначала зависимые таблицы.
// shortfalls (миграция 0002) ссылается на orders, recipe_items автосозданных продуктов — на products.
// Пользователи, сессии и ключи доступа сбросом не затрагиваются.
var resetOrder = []string{
	`DELETE FROM movements`,
	`DELETE FROM shortfalls`,
	`DELETE FROM order_items`,
	`DELETE FROM orders`,
	`DELETE FROM batches`,
	`DELETE FROM deliveries`,
	`DELETE FROM clock_jumps`,
	`DELETE FROM recipe_items WHERE product_id IN (SELECT id FROM products WHERE auto_created)`,
	`DELETE FROM products WHERE auto_created`,
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
