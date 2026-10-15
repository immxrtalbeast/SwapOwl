package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"umirhack/internal/domain"
)

func intp(v int) *int { return &v }

func rice() CatalogProduct {
	return CatalogProduct{ID: "rice", Name: "Рис", Unit: domain.UnitG, PriceMinorPer1000: 15000, LeadDays: 2, PackSize: 1000, ShelfLifeHours: intp(720)}
}

func pilaf() CatalogDish {
	return CatalogDish{ID: "pilaf", Name: "Плов", PriceMinor: 45000, PriorDaily: 12.5,
		Recipe: []CatalogRecipeItem{{ProductID: "rice", Quantity: 150}}}
}

func count(t *testing.T, db *DB, table string) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestImportCatalogCreatesUpdatesAndIsIdempotent(t *testing.T) {
	db, _ := stockDB(t)
	ctx := context.Background()
	in := CatalogInput{Products: []CatalogProduct{rice()}, Dishes: []CatalogDish{pilaf()}}
	diff, err := db.ImportCatalog(ctx, in, false)
	if err != nil || diff.Products[0].Action != CatalogCreated || diff.Dishes[0].Action != CatalogCreated {
		t.Fatalf("create: %v %+v", err, diff)
	}
	if diff, err = db.ImportCatalog(ctx, in, false); err != nil || diff.Products[0].Action != CatalogUnchanged || diff.Dishes[0].Action != CatalogUnchanged {
		t.Fatalf("repeat: %v %+v", err, diff)
	}
	// Дифф показывает записанные (развёрнутые) нормы.
	if got := diff.Dishes[0].Recipe; len(got) != len(in.Dishes[0].Recipe) {
		t.Fatalf("diff recipe: %+v", got)
	}

	// Обновление: цена, срок убран из тела (остаётся прежний), рецепт заменён целиком.
	r := rice()
	r.PriceMinorPer1000, r.ShelfLifeHours = 16000, nil
	d := pilaf()
	d.Recipe = []CatalogRecipeItem{{ProductID: "rice", Quantity: 200}, {ProductID: "salmon", Quantity: 10}}
	if _, err := db.Pool.Exec(ctx, `UPDATE products SET price_minor_per_1000 = 100 WHERE id = 'salmon'`); err != nil {
		t.Fatal(err)
	}
	diff, err = db.ImportCatalog(ctx, CatalogInput{Products: []CatalogProduct{r}, Dishes: []CatalogDish{d}}, false)
	if err != nil || diff.Products[0].Action != CatalogUpdated || len(diff.Products[0].Fields) != 1 || diff.Products[0].Fields[0] != "price_minor_per_1000" ||
		diff.Dishes[0].Action != CatalogUpdated || diff.Dishes[0].Fields[0] != "recipe" {
		t.Fatalf("update: %v %+v", err, diff)
	}
	var price int64
	var shelf *int
	_ = db.Pool.QueryRow(ctx, `SELECT price_minor_per_1000, shelf_life_hours FROM products WHERE id='rice'`).Scan(&price, &shelf)
	if price != 16000 || shelf == nil || *shelf != 720 || count(t, db, "recipe_items") != 2 {
		t.Fatalf("state: price=%d shelf=%v recipe=%d", price, shelf, count(t, db, "recipe_items"))
	}
	// Без удаления: продукты вне тела остаются.
	if count(t, db, "products") != 3 {
		t.Fatalf("products: %d", count(t, db, "products"))
	}
}

func TestImportCatalogDryRunWritesNothing(t *testing.T) {
	db, _ := stockDB(t)
	ctx := context.Background()
	diff, err := db.ImportCatalog(ctx, CatalogInput{Products: []CatalogProduct{rice()}, Dishes: []CatalogDish{pilaf()}}, true)
	if err != nil || !diff.DryRun || diff.Products[0].Action != CatalogCreated {
		t.Fatalf("dry: %v %+v", err, diff)
	}
	if count(t, db, "products") != 2 || count(t, db, "dishes") != 0 || count(t, db, "recipe_items") != 0 {
		t.Fatal("dry run wrote rows")
	}
}

func TestImportCatalogUnitChangeRollsBackEverything(t *testing.T) {
	db, _ := stockDB(t)
	ctx := context.Background()
	bad := CatalogProduct{ID: "salmon", Name: "Лосось", Unit: domain.UnitMl, PriceMinorPer1000: 1, LeadDays: 1, PackSize: 1}
	// rice сортируется после... salmon, поэтому ставим перед ним новый продукт «apple», чтобы увидеть откат.
	apple := CatalogProduct{ID: "apple", Name: "Яблоко", Unit: domain.UnitPcs, PriceMinorPer1000: 1, LeadDays: 1, PackSize: 1}
	_, err := db.ImportCatalog(ctx, CatalogInput{Products: []CatalogProduct{apple, bad}}, false)
	var uc *UnitChangeError
	if !errors.Is(err, ErrUnitChange) || !errors.As(err, &uc) || uc.ProductID != "salmon" || uc.Have != domain.UnitG {
		t.Fatalf("err: %v", err)
	}
	if count(t, db, "products") != 2 {
		t.Fatal("apple survived rollback")
	}
}

func TestImportCatalogChecksResult(t *testing.T) {
	db, _ := stockDB(t)
	ctx := context.Background()
	// salmon создан в stockDB с ценой 0: в рецепте он недопустим (2.2). Ничего не записано.
	d := pilaf()
	d.Recipe = []CatalogRecipeItem{{ProductID: "salmon", Quantity: 10}}
	_, err := db.ImportCatalog(ctx, CatalogInput{Products: []CatalogProduct{rice()}, Dishes: []CatalogDish{d}}, false)
	var ci *CatalogInvalidError
	if !errors.Is(err, ErrCatalogInvalid) || !errors.As(err, &ci) || len(ci.Problems) != 1 {
		t.Fatalf("err: %v", err)
	}
	if count(t, db, "products") != 2 || count(t, db, "dishes") != 0 {
		t.Fatal("invalid import left rows")
	}
	// Блюдо без рецепта.
	d.Recipe = nil
	if _, err = db.ImportCatalog(ctx, CatalogInput{Dishes: []CatalogDish{d}}, false); !errors.Is(err, ErrCatalogInvalid) {
		t.Fatalf("empty recipe: %v", err)
	}
}

func TestImportCatalogAdoptsAutoCreatedProduct(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	in := delivery("D1")
	in.Items = []DeliveryItem{{ProductID: "ghost", Name: "Призрак", Unit: domain.UnitG, Qty: 10, ShelfLifeHours: 5}}
	if _, err := db.ReceiveDelivery(ctx, in, clk.Now, time.UTC); err != nil {
		t.Fatal(err)
	}
	g := CatalogProduct{ID: "ghost", Name: "Призрак", Unit: domain.UnitG, PriceMinorPer1000: 5, LeadDays: 1, PackSize: 1}
	if _, err := db.ImportCatalog(ctx, CatalogInput{Products: []CatalogProduct{g}}, false); err != nil {
		t.Fatal(err)
	}
	var auto bool
	_ = db.Pool.QueryRow(ctx, `SELECT auto_created FROM products WHERE id='ghost'`).Scan(&auto)
	if auto {
		t.Fatal("imported product is still auto_created")
	}
}

// FS-160: срок годности приходит в поставке; срок из карточки продукта поставку без срока не спасает.
func TestDeliveryIgnoresProductShelfLife(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `UPDATE products SET shelf_life_hours = 48 WHERE id = 'salmon'`); err != nil {
		t.Fatal(err)
	}
	in := DeliveryInput{ID: "D9", Number: "9", CreatedAt: t0, Items: []DeliveryItem{
		{ProductID: "salmon", Unit: domain.UnitG, Qty: 1000, ShelfLifeHours: 5},
		{ProductID: "salmon", Unit: domain.UnitG, Qty: 1000}, // срока нет, хотя у продукта он есть
	}}
	var ns *NoShelfLifeError
	if _, err := db.ReceiveDelivery(ctx, in, clk.Now, time.UTC); !errors.As(err, &ns) || ns.Index != 1 {
		t.Fatalf("no expiry: %v", err)
	}
	if count(t, db, "deliveries") != 0 || count(t, db, "batches") != 0 {
		t.Fatal("rejected delivery left rows")
	}
	in.Items = in.Items[:1]
	resp, err := db.ReceiveDelivery(ctx, in, clk.Now, time.UTC)
	if err != nil || !resp.Batches[0].ExpiresAt.Equal(t0.Add(5*time.Hour)) {
		t.Fatalf("receive: %v %+v", err, resp.Batches)
	}
}

func TestDeliveryWithoutAnyShelfLifeIsRejected(t *testing.T) {
	db, clk := stockDB(t)
	ctx := context.Background()
	in := DeliveryInput{ID: "D8", Number: "8", CreatedAt: t0, Items: []DeliveryItem{
		{ProductID: "cream", Unit: domain.UnitMl, Qty: 100, ShelfLifeHours: 5},
		{ProductID: "salmon", Unit: domain.UnitG, Qty: 1000},
	}}
	_, err := db.ReceiveDelivery(ctx, in, clk.Now, time.UTC)
	var ns *NoShelfLifeError
	if !errors.Is(err, ErrNoShelfLife) || !errors.As(err, &ns) || ns.Index != 1 {
		t.Fatalf("err: %v", err)
	}
	if count(t, db, "deliveries") != 0 || count(t, db, "batches") != 0 {
		t.Fatal("rejected delivery left rows")
	}
	// Автосозданный продукт срока по умолчанию не имеет.
	in = DeliveryInput{ID: "D7", Number: "7", CreatedAt: t0, Items: []DeliveryItem{{ProductID: "new", Unit: domain.UnitG, Qty: 1}}}
	if _, err := db.ReceiveDelivery(ctx, in, clk.Now, time.UTC); !errors.Is(err, ErrNoShelfLife) {
		t.Fatalf("auto-created: %v", err)
	}
	if count(t, db, "products") != 2 {
		t.Fatal("auto-created product survived rollback")
	}
}

func i64p(v int64) *int64   { return &v }
func strp(v string) *string { return &v }

func TestImportCatalogPiecesAndPacks(t *testing.T) {
	db, _ := stockDB(t)
	ctx := context.Background()
	egg := CatalogProduct{ID: "egg", Name: "Яйцо", Unit: domain.UnitPcs, PriceMinorPer1000: 1, LeadDays: 1, PackSize: 30,
		PieceWeight: i64p(55), PieceWeightUnit: domain.UnitG}
	cream := CatalogProduct{ID: "cream2", Name: "Сливки 0,5 л", Unit: domain.UnitMl, PriceMinorPer1000: 1, LeadDays: 1, PackSize: 500, PackUnit: strp("уп")}
	in := CatalogInput{Products: []CatalogProduct{egg, cream, rice()}}
	if diff, err := db.ImportCatalog(ctx, in, false); err != nil || diff.Products[0].Action != CatalogCreated {
		t.Fatalf("create: %v %+v", err, diff)
	}
	got, err := db.ProductPacking(ctx, []string{"egg", "cream2", "rice", "ghost"})
	if err != nil || len(got) != 3 {
		t.Fatalf("packing: %v %+v", err, got)
	}
	if e := got["egg"]; e.Unit != domain.UnitPcs || e.PieceWeight != 55 || e.PieceWeightUnit != domain.UnitG || e.PackUnit != "" {
		t.Errorf("egg: %+v", e)
	}
	if c := got["cream2"]; c.PackSize != 500 || c.PackUnit != "уп" || c.PieceWeight != 0 {
		t.Errorf("cream: %+v", c)
	}
	// Продукт без новых полей — как раньше.
	if r := got["rice"]; r.PieceWeight != 0 || r.PackUnit != "" || r.PackSize != 1000 {
		t.Errorf("rice: %+v", r)
	}

	// Повтор — без изменений; поля, не переданные в теле, остаются прежними.
	if diff, err := db.ImportCatalog(ctx, in, false); err != nil || diff.Products[0].Action != CatalogUnchanged || diff.Products[1].Action != CatalogUnchanged {
		t.Fatalf("repeat: %v %+v", err, diff)
	}
	bare := egg
	bare.PieceWeight, bare.PieceWeightUnit = nil, ""
	if diff, err := db.ImportCatalog(ctx, CatalogInput{Products: []CatalogProduct{bare}}, false); err != nil || diff.Products[0].Action != CatalogUnchanged {
		t.Fatalf("bare repeat: %v %+v", err, diff)
	}
	// Смена веса — обновление с полем piece_weight.
	egg.PieceWeight = i64p(60)
	diff, err := db.ImportCatalog(ctx, CatalogInput{Products: []CatalogProduct{egg}}, false)
	if err != nil || diff.Products[0].Action != CatalogUpdated || len(diff.Products[0].Fields) != 1 || diff.Products[0].Fields[0] != "piece_weight" {
		t.Fatalf("update: %v %+v", err, diff)
	}
	if got, _ = db.ProductPacking(ctx, []string{"egg"}); got["egg"].PieceWeight != 60 {
		t.Fatalf("weight not stored: %+v", got)
	}
}

// Вес штуки допустим только у штучных продуктов — это держит схема.
func TestPieceWeightOnlyForPcs(t *testing.T) {
	db, _ := stockDB(t)
	bad := rice()
	bad.PieceWeight, bad.PieceWeightUnit = i64p(5), domain.UnitG
	if _, err := db.ImportCatalog(context.Background(), CatalogInput{Products: []CatalogProduct{bad}}, false); err == nil {
		t.Fatal("piece_weight on a gram product must be rejected")
	}
}
