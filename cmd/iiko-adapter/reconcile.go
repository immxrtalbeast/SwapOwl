package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"umirhack/internal/iiko/mapping"
	"umirhack/internal/iiko/reconcile"
)

// runReconcile — подкоманда reconcile: остатки iiko против GET /api/products.
// Только чтение с обеих сторон; ничего не корректирует. По умолчанию — только расхождения.
func (a *adapter) runReconcile(ctx context.Context, w io.Writer, asJSON, all bool) error {
	ours, err := a.api.productStock(ctx)
	if err != nil {
		return fmt.Errorf("наши остатки: %w", err)
	}
	now := a.now()
	balances, err := a.iiko.Balance(ctx, now.In(a.loc))
	if err != nil {
		return fmt.Errorf("остатки iiko: %w", err)
	}
	products, err := a.iiko.Products(ctx)
	if err != nil {
		return err
	}
	today := dayStart(now, a.loc)
	invs, err := a.iiko.Invoices(ctx, today.AddDate(0, 0, -priceWindow), today)
	if err != nil {
		return err
	}
	rows, err := reconcile.Compare(ours, balances, products, mapping.Prices(invs))
	if err != nil {
		return err
	}
	if !all {
		rows = reconcile.Discrepancies(rows)
	}
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	if len(rows) == 0 {
		_, err = fmt.Fprintln(w, "расхождений нет")
		return err
	}
	return reconcile.WriteTable(w, rows)
}
