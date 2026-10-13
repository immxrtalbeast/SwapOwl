package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"umirhack/internal/clock"
	"umirhack/internal/domain"
)

// openedDB — склад с лососем без срока после вскрытия и сливками со сроком 48 ч и упаковкой 1000.
func openedDB(t *testing.T) (*DB, *clock.Fixed) {
	t.Helper()
	db, clk := orderDB(t)
	if _, err := db.Pool.Exec(context.Background(),
		`UPDATE products SET shelf_after_open_hours = 48, pack_size = 1000 WHERE id = 'cream'`); err != nil {
		t.Fatal(err)
	}
	return db, clk
}

func receiveCream(t *testing.T, db *DB, clk *clock.Fixed, id string, qty int64, hours int) {
	t.Helper()
	if _, err := db.ReceiveDelivery(context.Background(), DeliveryInput{ID: id, Number: "1", CreatedAt: t0,
		Items: []DeliveryItem{{ProductID: "cream", Unit: domain.UnitMl, Qty: qty, ShelfLifeHours: hours}}}, clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
}

// balanceHoldsOpened — инвариант 1 с переносом: receipt + transfer_in + return − sale − writeoff − adjustment − transfer_out = remaining.
func balanceHoldsOpened(t *testing.T, db *DB) {
	t.Helper()
	if bad := cnt(t, db, `
		SELECT count(*) FROM batches b WHERE b.remaining <> COALESCE((
			SELECT SUM(CASE WHEN kind IN ('receipt','return','transfer_in') THEN quantity ELSE -quantity END)
			FROM movements WHERE batch_id = b.id), 0) OR b.remaining < 0 OR b.remaining > b.quantity`); bad != 0 {
		t.Fatalf("%d batches break quantity conservation", bad)
	}
	if in, out := cnt(t, db, `SELECT COALESCE(SUM(quantity),0)::int FROM movements WHERE kind = 'transfer_in'`),
		cnt(t, db, `SELECT COALESCE(SUM(quantity),0)::int FROM movements WHERE kind = 'transfer_out'`); in != out {
		t.Fatalf("transfer_in %d != transfer_out %d", in, out)
	}
}

func TestOpenBatchSplitsAndShortensShelf(t *testing.T) {
	db, clk := openedDB(t)
	ctx := context.Background()
	receiveCream(t, db, clk, "D1", 5000, 240) // 10 суток
	before, _ := db.ListProductStock(ctx, clk.Now())

	res, err := db.OpenBatch(ctx, OpenInput{ID: "o1", BatchID: 1}, clk.Now)
	if err != nil || res.Replayed {
		t.Fatalf("open: %v %+v", err, res)
	}
	want := clk.Now().Add(48 * time.Hour)
	if res.Opened.Qty != 1000 || res.ParentLeft.Qty != 4000 || !res.Child.ExpiresAt.Equal(want) || res.ShelfHours != 48 || res.ParentBatchID != 1 {
		t.Fatalf("result: %+v", res)
	}
	if batchRemaining(t, db, 1) != 4000 || batchRemaining(t, db, res.Child.ID) != 1000 {
		t.Fatalf("remaining: parent %d, child %d", batchRemaining(t, db, 1), batchRemaining(t, db, res.Child.ID))
	}
	if cnt(t, db, `SELECT count(*) FROM batches WHERE id = $1 AND parent_batch_id = 1 AND opened_at = $2 AND quantity = 1000 AND expires_at = $3`,
		res.Child.ID, clk.Now(), want) != 1 {
		t.Fatal("child batch fields")
	}
	if cnt(t, db, `SELECT count(*) FROM movements WHERE reason = 'opened' AND quantity = 1000 AND
		((kind = 'transfer_out' AND batch_id = 1) OR (kind = 'transfer_in' AND batch_id = $1))`, res.Child.ID) != 2 {
		t.Fatal("movements")
	}
	// Партия родителя сохраняет срок; остаток продукта не изменился.
	after, _ := db.ListProductStock(ctx, clk.Now())
	for i := range before {
		if before[i].ID == "cream" && before[i].Available.Qty != after[i].Available.Qty {
			t.Fatalf("available %d → %d", before[i].Available.Qty, after[i].Available.Qty)
		}
	}
	if cnt(t, db, `SELECT count(*) FROM batches WHERE id = 1 AND expires_at = received_at + interval '240 hours'`) != 1 {
		t.Fatal("parent expiry changed")
	}
	balanceHoldsOpened(t, db)

	// Партии в списке: дочерняя видна с пометкой и идёт раньше родителя (FEFO).
	list, err := db.ListBatches(ctx, clk.Now(), "cream", "")
	if err != nil || len(list) != 2 || list[0].ID != res.Child.ID || list[0].ParentBatchID == nil || *list[0].ParentBatchID != 1 ||
		list[0].OpenedAt == nil || !list[0].OpenedAt.Equal(clk.Now()) || list[1].ParentBatchID != nil || list[1].OpenedAt != nil {
		t.Fatalf("list: %v %+v", err, list)
	}
}

func TestOpenBatchFEFOSellsOpenedFirst(t *testing.T) {
	db, clk := openedDB(t)
	ctx := context.Background()
	receiveCream(t, db, clk, "D1", 5000, 240)
	res, err := db.OpenBatch(ctx, OpenInput{ID: "o1", BatchID: 1}, clk.Now)
	if err != nil {
		t.Fatal(err)
	}
	// Блюдо «соус» — 50 мл сливок на порцию; 25 порций = 1250 мл: вся дочерняя (1000) и 250 из родителя.
	if _, err := db.Pool.Exec(ctx, `INSERT INTO recipe_items VALUES ('sauce','cream',50) ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	resp, err := db.PlaceOrder(ctx, OrderInput{ID: o1, Items: []OrderItem{{DishID: "sauce", Portions: 25}},
		Needs: []domain.Need{{ProductID: "cream", Qty: 1250}}}, clk.Now)
	if err != nil || resp.Status != OrderAccepted {
		t.Fatalf("order: %v %+v", err, resp)
	}
	if batchRemaining(t, db, res.Child.ID) != 0 || batchRemaining(t, db, 1) != 3750 {
		t.Fatalf("child %d parent %d", batchRemaining(t, db, res.Child.ID), batchRemaining(t, db, 1))
	}
	balanceHoldsOpened(t, db)
}

func TestOpenBatchExpiresByOpenedShelf(t *testing.T) {
	db, clk := openedDB(t)
	ctx := context.Background()
	receiveCream(t, db, clk, "D1", 2000, 240)
	res, err := db.OpenBatch(ctx, OpenInput{ID: "o1", BatchID: 1}, clk.Now)
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(48 * time.Hour) // ровно expires_at дочерней: недоступна, родитель жив
	stock, _ := db.ListProductStock(ctx, clk.Now())
	for _, s := range stock {
		if s.ID == "cream" && s.Available.Qty != 1000 {
			t.Fatalf("available %d, want 1000 (child expired)", s.Available.Qty)
		}
	}
	if _, err := db.OpenBatch(ctx, OpenInput{ID: "o2", BatchID: res.Child.ID}, clk.Now); !errors.Is(err, ErrAlreadyOpened) {
		t.Fatalf("expired child (already opened wins): %v", err)
	}
}

func TestOpenBatchNeverExtendsShelf(t *testing.T) {
	db, clk := openedDB(t)
	receiveCream(t, db, clk, "D1", 2000, 10) // 10 ч < 48 ч
	res, err := db.OpenBatch(context.Background(), OpenInput{ID: "o1", BatchID: 1}, clk.Now)
	if err != nil || !res.Child.ExpiresAt.Equal(clk.Now().Add(10*time.Hour)) {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestOpenBatchExplicitQtyAndWholeBatch(t *testing.T) {
	db, clk := openedDB(t)
	ctx := context.Background()
	receiveCream(t, db, clk, "D1", 2500, 240)
	q := int64(700)
	res, err := db.OpenBatch(ctx, OpenInput{ID: "o1", BatchID: 1, Qty: &q}, clk.Now)
	if err != nil || res.Opened.Qty != 700 || batchRemaining(t, db, 1) != 1800 {
		t.Fatalf("%v %+v", err, res)
	}
	all := int64(1800)
	if _, err := db.OpenBatch(ctx, OpenInput{ID: "o2", BatchID: 1, Qty: &all}, clk.Now); err != nil || batchRemaining(t, db, 1) != 0 {
		t.Fatalf("whole batch: %v", err)
	}
	// Пустая партия — 409.
	if _, err := db.OpenBatch(ctx, OpenInput{ID: "o3", BatchID: 1}, clk.Now); !errors.Is(err, domain.ErrOpenClosed) {
		t.Fatalf("empty: %v", err)
	}
	balanceHoldsOpened(t, db)
}

func TestOpenBatchDefaultsToWholeBatchWithoutPackSize(t *testing.T) {
	db, clk := openedDB(t)
	ctx := context.Background()
	receiveCream(t, db, clk, "D1", 2500, 240)
	if _, err := db.Pool.Exec(ctx, `UPDATE products SET pack_size = 1 WHERE id = 'cream'`); err != nil {
		t.Fatal(err)
	}
	res, err := db.OpenBatch(ctx, OpenInput{ID: "o1", BatchID: 1}, clk.Now)
	if err != nil || res.Opened.Qty != 2500 || batchRemaining(t, db, 1) != 0 {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestOpenBatchErrors(t *testing.T) {
	db, clk := openedDB(t)
	ctx := context.Background()
	receiveCream(t, db, clk, "D1", 2000, 24)
	receive(t, db, clk, "D2", 500, 72) // лосось: срока после вскрытия нет → батч 2
	over := int64(2001)
	neg := int64(-5)
	cases := []struct {
		name string
		in   OpenInput
		want error
	}{
		{"unknown batch", OpenInput{ID: "e1", BatchID: 99}, ErrUnknownBatch},
		{"no shelf after open", OpenInput{ID: "e2", BatchID: 2}, domain.ErrOpenNoShelf},
		{"qty above remainder", OpenInput{ID: "e3", BatchID: 1, Qty: &over}, domain.ErrOpenQty},
		{"negative qty", OpenInput{ID: "e4", BatchID: 1, Qty: &neg}, domain.ErrOpenQty},
	}
	for _, c := range cases {
		if _, err := db.OpenBatch(ctx, c.in, clk.Now); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	// Отказ ничего не записал и не заняла ключ идемпотентности.
	if cnt(t, db, `SELECT count(*) FROM batches WHERE parent_batch_id IS NOT NULL`) != 0 || cnt(t, db, `SELECT count(*) FROM corrections WHERE kind = 'open'`) != 0 ||
		cnt(t, db, `SELECT count(*) FROM movements WHERE reason = 'opened'`) != 0 {
		t.Fatal("rejected open left traces")
	}
	// Просроченная партия — 409.
	clk.Advance(24 * time.Hour)
	if _, err := db.OpenBatch(ctx, OpenInput{ID: "e5", BatchID: 1}, clk.Now); !errors.Is(err, domain.ErrOpenClosed) {
		t.Errorf("expired: %v", err)
	}
	balanceHoldsOpened(t, db)
}

func TestOpenBatchIdempotent(t *testing.T) {
	db, clk := openedDB(t)
	ctx := context.Background()
	receiveCream(t, db, clk, "D1", 5000, 240)
	in := OpenInput{ID: "o1", BatchID: 1}
	first, err := db.OpenBatch(ctx, in, clk.Now)
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Hour)
	again, err := db.OpenBatch(ctx, in, clk.Now)
	if err != nil || !again.Replayed || again.Child.ID != first.Child.ID || !again.OpenedAt.Equal(first.OpenedAt) || batchRemaining(t, db, 1) != 4000 {
		t.Fatalf("replay: %v %+v", err, again)
	}
	q := int64(10)
	if _, err := db.OpenBatch(ctx, OpenInput{ID: "o1", BatchID: 1, Qty: &q}, clk.Now); !errors.Is(err, ErrCorrectionConflict) {
		t.Fatalf("other body: %v", err)
	}
	if cnt(t, db, `SELECT count(*) FROM batches WHERE parent_batch_id IS NOT NULL`) != 1 {
		t.Fatal("replay created a second child")
	}
}

func TestOpenBatchConcurrentSameID(t *testing.T) {
	db, clk := openedDB(t)
	receiveCream(t, db, clk, "D1", 5000, 240)
	var wg sync.WaitGroup
	var mu sync.Mutex
	replays := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := db.OpenBatch(context.Background(), OpenInput{ID: "o1", BatchID: 1}, clk.Now)
			if err != nil {
				t.Error(err)
				return
			}
			if res.Replayed {
				mu.Lock()
				replays++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if replays != 9 || batchRemaining(t, db, 1) != 4000 || cnt(t, db, `SELECT count(*) FROM batches WHERE parent_batch_id IS NOT NULL`) != 1 {
		t.Fatalf("replays=%d remaining=%d", replays, batchRemaining(t, db, 1))
	}
	balanceHoldsOpened(t, db)
}

// Параллельные вскрытия разных операций над одной партией не вскрывают больше остатка.
func TestOpenBatchConcurrentDifferentIDs(t *testing.T) {
	db, clk := openedDB(t)
	receiveCream(t, db, clk, "D1", 3000, 240)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := db.OpenBatch(context.Background(), OpenInput{ID: "o" + string(rune('a'+i)), BatchID: 1}, clk.Now)
			switch {
			case err == nil:
				mu.Lock()
				ok++
				mu.Unlock()
			case errors.Is(err, domain.ErrOpenClosed):
			default:
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if ok != 3 || batchRemaining(t, db, 1) != 0 {
		t.Fatalf("opened %d times, remaining %d", ok, batchRemaining(t, db, 1))
	}
	balanceHoldsOpened(t, db)
}

func TestCatalogImportShelfAfterOpen(t *testing.T) {
	db, _ := openedDB(t)
	ctx := context.Background()
	h := 72
	in := CatalogInput{Products: []CatalogProduct{{ID: "cream", Name: "Сливки", Unit: domain.UnitMl, PriceMinorPer1000: 1, LeadDays: 1, PackSize: 1000, ShelfAfterOpenHours: &h},
		{ID: "salmon", Name: "Лосось", Unit: domain.UnitG, PriceMinorPer1000: 1, LeadDays: 1, PackSize: 1000}}}
	diff, err := db.ImportCatalog(ctx, in, false)
	if err != nil || len(diff.Products) != 2 || diff.Products[0].Action != CatalogUpdated {
		t.Fatalf("%v %+v", err, diff)
	}
	found := false
	for _, f := range diff.Products[0].Fields {
		found = found || f == "shelf_after_open_hours"
	}
	if !found {
		t.Fatalf("fields: %v", diff.Products[0].Fields)
	}
	if cnt(t, db, `SELECT count(*) FROM products WHERE id = 'cream' AND shelf_after_open_hours = 72`) != 1 {
		t.Fatal("not stored")
	}
	// Не передано — прежнее значение сохраняется.
	in.Products[0].ShelfAfterOpenHours = nil
	if _, err := db.ImportCatalog(ctx, in, false); err != nil || cnt(t, db, `SELECT count(*) FROM products WHERE id = 'cream' AND shelf_after_open_hours = 72`) != 1 {
		t.Fatalf("kept: %v", err)
	}
}

func TestOpenBatchRejectsReopen(t *testing.T) {
	db, clk := openedDB(t)
	ctx := context.Background()
	receiveCream(t, db, clk, "D1", 5000, 240)
	first, err := db.OpenBatch(ctx, OpenInput{ID: "o1", BatchID: 1}, clk.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.OpenBatch(ctx, OpenInput{ID: "o2", BatchID: first.Child.ID}, clk.Now); !errors.Is(err, ErrAlreadyOpened) {
		t.Fatalf("reopen child: %v", err)
	}
	if cnt(t, db, `SELECT count(*) FROM batches WHERE parent_batch_id IS NOT NULL`) != 1 || cnt(t, db, `SELECT count(*) FROM corrections WHERE kind = 'open'`) != 1 {
		t.Fatal("rejected reopen left traces")
	}
	// Повтор исходного запроса по-прежнему идемпотентен.
	if again, err := db.OpenBatch(ctx, OpenInput{ID: "o1", BatchID: 1}, clk.Now); err != nil || !again.Replayed {
		t.Fatalf("replay: %v %+v", err, again)
	}
	balanceHoldsOpened(t, db)
}
