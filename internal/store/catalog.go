package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/domain"
)

// ErrUnitChange — у существующего продукта пришла другая единица (.ai/solution.md, 7.1: unit не меняется).
var ErrUnitChange = errors.New("store: product unit cannot change")

// UnitChangeError несёт продукт и обе единицы; errors.Is(err, ErrUnitChange) истинно.
type UnitChangeError struct {
	ProductID string
	Have, Got domain.Unit
}

func (e *UnitChangeError) Error() string {
	return fmt.Sprintf("store: product %s has unit %s, import has %s", e.ProductID, e.Have, e.Got)
}
func (e *UnitChangeError) Is(target error) bool { return target == ErrUnitChange }

// ErrCatalogInvalid — результат импорта не проходит проверки старта (2.2).
var ErrCatalogInvalid = errors.New("store: catalog is invalid")

// CatalogInvalidError перечисляет нарушения; errors.Is(err, ErrCatalogInvalid) истинно.
type CatalogInvalidError struct{ Problems []string }

func (e *CatalogInvalidError) Error() string {
	return "store: catalog is invalid: " + strings.Join(e.Problems, "; ")
}
func (e *CatalogInvalidError) Is(target error) bool { return target == ErrCatalogInvalid }

// CatalogProduct — продукт импорта. ShelfLifeHours == nil — срок продукта не трогать.
type CatalogProduct struct {
	ID                string
	Name              string
	Unit              domain.Unit
	PriceMinorPer1000 int64
	LeadDays          int
	PackSize          int64
	ShelfLifeHours    *int
}

// CatalogRecipeItem — норма в базовой единице продукта.
type CatalogRecipeItem struct {
	ProductID string
	Quantity  int64
}

// CatalogDish — блюдо импорта; рецепт заменяет прежний целиком.
type CatalogDish struct {
	ID         string
	Name       string
	PriceMinor int64
	PriorDaily float64
	Recipe     []CatalogRecipeItem
}

// CatalogInput — тело импорта после разбора на границе API.
type CatalogInput struct {
	Products []CatalogProduct
	Dishes   []CatalogDish
}

// Действия в CatalogChange.
const (
	CatalogCreated   = "created"
	CatalogUpdated   = "updated"
	CatalogUnchanged = "unchanged"
)

// CatalogChange — что импорт делает с одной записью; Fields — изменившиеся поля обновления.
type CatalogChange struct {
	ID     string   `json:"id"`
	Action string   `json:"action"`
	Fields []string `json:"fields,omitempty"`
}

// CatalogDiff — разница импорта; при dry_run ровно то, что было бы записано.
type CatalogDiff struct {
	DryRun   bool            `json:"dry_run"`
	Products []CatalogChange `json:"products"`
	Dishes   []CatalogChange `json:"dishes"`
}

type productRow struct {
	name       string
	unit       domain.Unit
	price      int64
	lead       int
	pack       int64
	shelf      *int
	autoCreate bool
}

var errDryRun = errors.New("store: dry run")

// ImportCatalog добавляет и обновляет продукты, блюда и техкарты одной транзакцией, ничего не удаляя.
// Результат проходит проверки старта 2.2; любая ошибка откатывает всё. dryRun откатывает всегда.
func (d *DB) ImportCatalog(ctx context.Context, in CatalogInput, dryRun bool) (CatalogDiff, error) {
	diff := CatalogDiff{DryRun: dryRun, Products: []CatalogChange{}, Dishes: []CatalogChange{}}
	err := d.InTx(ctx, func(tx pgx.Tx) error {
		// Один и тот же порядок (по id) во всех импортах — иначе блокировки строк дают deadlock.
		prods := append([]CatalogProduct(nil), in.Products...)
		sort.Slice(prods, func(i, j int) bool { return prods[i].ID < prods[j].ID })
		for _, p := range prods {
			ch, err := upsertProduct(ctx, tx, p)
			if err != nil {
				return err
			}
			diff.Products = append(diff.Products, ch)
		}
		dishes := append([]CatalogDish(nil), in.Dishes...)
		sort.Slice(dishes, func(i, j int) bool { return dishes[i].ID < dishes[j].ID })
		for _, ds := range dishes {
			ch, err := upsertDish(ctx, tx, ds)
			if err != nil {
				return err
			}
			diff.Dishes = append(diff.Dishes, ch)
		}
		if err := checkCatalog(ctx, tx); err != nil {
			return err
		}
		if dryRun {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		err = nil
	}
	return diff, err
}

func upsertProduct(ctx context.Context, tx pgx.Tx, p CatalogProduct) (CatalogChange, error) {
	const sel = `SELECT name, unit, price_minor_per_1000, lead_days, pack_size, shelf_life_hours, auto_created
		FROM products WHERE id = $1 FOR UPDATE`
	scan := func() (productRow, bool, error) {
		var r productRow
		var u string
		err := tx.QueryRow(ctx, sel, p.ID).Scan(&r.name, &u, &r.price, &r.lead, &r.pack, &r.shelf, &r.autoCreate)
		if errors.Is(err, pgx.ErrNoRows) {
			return r, false, nil
		}
		r.unit = domain.Unit(u)
		return r, err == nil, err
	}
	cur, ok, err := scan()
	if err != nil {
		return CatalogChange{}, fmt.Errorf("store: catalog product %s: %w", p.ID, err)
	}
	if !ok {
		tag, err := tx.Exec(ctx,
			`INSERT INTO products (id, name, unit, price_minor_per_1000, lead_days, pack_size, shelf_life_hours, auto_created)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, false) ON CONFLICT (id) DO NOTHING`,
			p.ID, p.Name, string(p.Unit), p.PriceMinorPer1000, p.LeadDays, p.PackSize, p.ShelfLifeHours)
		if err != nil {
			return CatalogChange{}, fmt.Errorf("store: catalog insert product %s: %w", p.ID, err)
		}
		if tag.RowsAffected() == 1 {
			return CatalogChange{ID: p.ID, Action: CatalogCreated}, nil
		}
		// Параллельный импорт или поставка успели раньше: читаем их строку под блокировкой.
		if cur, ok, err = scan(); err != nil || !ok {
			return CatalogChange{}, fmt.Errorf("store: catalog product %s: %w", p.ID, err)
		}
	}
	if cur.unit != p.Unit {
		return CatalogChange{}, &UnitChangeError{ProductID: p.ID, Have: cur.unit, Got: p.Unit}
	}
	shelf := cur.shelf
	if p.ShelfLifeHours != nil {
		shelf = p.ShelfLifeHours
	}
	var fields []string
	add := func(name string, changed bool) {
		if changed {
			fields = append(fields, name)
		}
	}
	add("name", cur.name != p.Name)
	add("price_minor_per_1000", cur.price != p.PriceMinorPer1000)
	add("lead_days", cur.lead != p.LeadDays)
	add("pack_size", cur.pack != p.PackSize)
	add("shelf_life_hours", (cur.shelf == nil) != (shelf == nil) || (shelf != nil && *cur.shelf != *shelf))
	if len(fields) == 0 && !cur.autoCreate {
		return CatalogChange{ID: p.ID, Action: CatalogUnchanged}, nil
	}
	// Импортированный продукт — справочный: сброс стенда автосозданные удаляет, а этот остаётся.
	if _, err := tx.Exec(ctx,
		`UPDATE products SET name=$2, price_minor_per_1000=$3, lead_days=$4, pack_size=$5, shelf_life_hours=$6, auto_created=false WHERE id=$1`,
		p.ID, p.Name, p.PriceMinorPer1000, p.LeadDays, p.PackSize, shelf); err != nil {
		return CatalogChange{}, fmt.Errorf("store: catalog update product %s: %w", p.ID, err)
	}
	if len(fields) == 0 {
		return CatalogChange{ID: p.ID, Action: CatalogUnchanged}, nil
	}
	return CatalogChange{ID: p.ID, Action: CatalogUpdated, Fields: fields}, nil
}

type dishRow struct {
	name   string
	price  int64
	prior  float64
	recipe map[string]int64
}

func readDish(ctx context.Context, tx pgx.Tx, id string) (dishRow, bool, error) {
	var r dishRow
	err := tx.QueryRow(ctx, `SELECT name, price_minor, prior_daily::float8 FROM dishes WHERE id = $1 FOR UPDATE`, id).
		Scan(&r.name, &r.price, &r.prior)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	rows, err := tx.Query(ctx, `SELECT product_id, quantity FROM recipe_items WHERE dish_id = $1`, id)
	if err != nil {
		return r, false, err
	}
	defer rows.Close()
	r.recipe = map[string]int64{}
	for rows.Next() {
		var pid string
		var q int64
		if err := rows.Scan(&pid, &q); err != nil {
			return r, false, err
		}
		r.recipe[pid] = q
	}
	return r, true, rows.Err()
}

func upsertDish(ctx context.Context, tx pgx.Tx, ds CatalogDish) (CatalogChange, error) {
	cur, ok, err := readDish(ctx, tx, ds.ID)
	if err != nil {
		return CatalogChange{}, fmt.Errorf("store: catalog dish %s: %w", ds.ID, err)
	}
	if !ok {
		tag, err := tx.Exec(ctx,
			`INSERT INTO dishes (id, name, price_minor, prior_daily) VALUES ($1, $2, $3, $4::float8) ON CONFLICT (id) DO NOTHING`,
			ds.ID, ds.Name, ds.PriceMinor, ds.PriorDaily)
		if err != nil {
			return CatalogChange{}, fmt.Errorf("store: catalog insert dish %s: %w", ds.ID, err)
		}
		if tag.RowsAffected() == 1 {
			if err := writeRecipe(ctx, tx, ds); err != nil {
				return CatalogChange{}, err
			}
			return CatalogChange{ID: ds.ID, Action: CatalogCreated}, nil
		}
		if cur, ok, err = readDish(ctx, tx, ds.ID); err != nil || !ok {
			return CatalogChange{}, fmt.Errorf("store: catalog dish %s: %w", ds.ID, err)
		}
	}
	var fields []string
	if cur.name != ds.Name {
		fields = append(fields, "name")
	}
	if cur.price != ds.PriceMinor {
		fields = append(fields, "price_minor")
	}
	if cur.prior != ds.PriorDaily {
		fields = append(fields, "prior_daily")
	}
	same := len(cur.recipe) == len(ds.Recipe)
	for _, it := range ds.Recipe {
		if q, has := cur.recipe[it.ProductID]; !has || q != it.Quantity {
			same = false
		}
	}
	if !same {
		fields = append(fields, "recipe")
	}
	if len(fields) == 0 {
		return CatalogChange{ID: ds.ID, Action: CatalogUnchanged}, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE dishes SET name=$2, price_minor=$3, prior_daily=$4::float8 WHERE id=$1`,
		ds.ID, ds.Name, ds.PriceMinor, ds.PriorDaily); err != nil {
		return CatalogChange{}, fmt.Errorf("store: catalog update dish %s: %w", ds.ID, err)
	}
	if !same {
		if _, err := tx.Exec(ctx, `DELETE FROM recipe_items WHERE dish_id = $1`, ds.ID); err != nil {
			return CatalogChange{}, fmt.Errorf("store: catalog reset recipe %s: %w", ds.ID, err)
		}
		if err := writeRecipe(ctx, tx, ds); err != nil {
			return CatalogChange{}, err
		}
	}
	return CatalogChange{ID: ds.ID, Action: CatalogUpdated, Fields: fields}, nil
}

func writeRecipe(ctx context.Context, tx pgx.Tx, ds CatalogDish) error {
	for _, it := range ds.Recipe {
		if _, err := tx.Exec(ctx, `INSERT INTO recipe_items (dish_id, product_id, quantity) VALUES ($1, $2, $3)`,
			ds.ID, it.ProductID, it.Quantity); err != nil {
			return fmt.Errorf("store: catalog recipe %s/%s: %w", ds.ID, it.ProductID, err)
		}
	}
	return nil
}

// checkCatalog — проверки старта 2.2 по всему справочнику после импорта: у каждого продукта из
// рецептов цена > 0, рецепт каждого блюда непуст (единица и норма > 0 держат схема и разбор).
func checkCatalog(ctx context.Context, tx pgx.Tx) error {
	var problems []string
	collect := func(query, format string) error {
		rows, err := tx.Query(ctx, query)
		if err != nil {
			return fmt.Errorf("store: catalog check: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("store: catalog check: %w", err)
			}
			problems = append(problems, fmt.Sprintf(format, id))
		}
		return rows.Err()
	}
	if err := collect(`SELECT DISTINCT p.id FROM recipe_items r JOIN products p ON p.id = r.product_id
		WHERE p.price_minor_per_1000 <= 0 ORDER BY p.id`,
		"product %s is used in a recipe and needs price_minor_per_1000 > 0"); err != nil {
		return err
	}
	if err := collect(`SELECT d.id FROM dishes d
		WHERE NOT EXISTS (SELECT 1 FROM recipe_items r WHERE r.dish_id = d.id) ORDER BY d.id`,
		"dish %s has an empty recipe"); err != nil {
		return err
	}
	if len(problems) > 0 {
		return &CatalogInvalidError{Problems: problems}
	}
	return nil
}
