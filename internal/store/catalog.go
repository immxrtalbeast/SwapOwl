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

// ErrUnitChange — у существующего продукта пришла другая единица (спецификация, 7.1: unit не меняется).
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
	// ShelfAfterOpenHours — срок после вскрытия упаковки; nil — не трогать (очистить через импорт нельзя).
	ShelfAfterOpenHours *int
	// PieceWeight (g или ml на 1 шт, единица в PieceWeightUnit) и PackUnit — необязательны;
	// nil — поле не трогать (как ShelfLifeHours; очистить через импорт нельзя).
	PieceWeight     *int64
	PieceWeightUnit domain.Unit
	PackUnit        *string
}

// CatalogRecipeItem — норма в базовой единице продукта.
type CatalogRecipeItem struct {
	ProductID string `json:"product_id"`
	Quantity  int64  `json:"quantity"`
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
	// Recipe — у блюд: развёрнутые нормы сырья в базовых единицах, как они записаны (или были бы при dry_run).
	Recipe []CatalogRecipeItem `json:"recipe,omitempty"`
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
	afterOpen  *int
	autoCreate bool
	pieceW     *int64
	pieceU     *string
	packUnit   *string
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
	const sel = `SELECT name, unit, price_minor_per_1000, lead_days, pack_size, shelf_life_hours, auto_created,
		piece_weight, piece_weight_unit, pack_unit, shelf_after_open_hours FROM products WHERE id = $1 FOR UPDATE`
	scan := func() (productRow, bool, error) {
		var r productRow
		var u string
		err := tx.QueryRow(ctx, sel, p.ID).Scan(&r.name, &u, &r.price, &r.lead, &r.pack, &r.shelf, &r.autoCreate, &r.pieceW, &r.pieceU, &r.packUnit, &r.afterOpen)
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
			`INSERT INTO products (id, name, unit, price_minor_per_1000, lead_days, pack_size, shelf_life_hours, auto_created,
			 piece_weight, piece_weight_unit, pack_unit, shelf_after_open_hours)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, false, $8, $9, $10, $11) ON CONFLICT (id) DO NOTHING`,
			p.ID, p.Name, string(p.Unit), p.PriceMinorPer1000, p.LeadDays, p.PackSize, p.ShelfLifeHours,
			p.PieceWeight, pieceUnitArg(p), p.PackUnit, p.ShelfAfterOpenHours)
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
	afterOpen := cur.afterOpen
	if p.ShelfAfterOpenHours != nil {
		afterOpen = p.ShelfAfterOpenHours
	}
	pw, pu, pk := cur.pieceW, cur.pieceU, cur.packUnit
	if p.PieceWeight != nil {
		pw, pu = p.PieceWeight, pieceUnitArg(p)
	}
	if p.PackUnit != nil {
		pk = p.PackUnit
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
	add("shelf_after_open_hours", (cur.afterOpen == nil) != (afterOpen == nil) || (afterOpen != nil && *cur.afterOpen != *afterOpen))
	add("piece_weight", (cur.pieceW == nil) != (pw == nil) || (pw != nil && (*cur.pieceW != *pw || *cur.pieceU != *pu)))
	add("pack_unit", (cur.packUnit == nil) != (pk == nil) || (pk != nil && *cur.packUnit != *pk))
	if len(fields) == 0 && !cur.autoCreate {
		return CatalogChange{ID: p.ID, Action: CatalogUnchanged}, nil
	}
	// Импортированный продукт — справочный: сброс стенда автосозданные удаляет, а этот остаётся.
	if _, err := tx.Exec(ctx,
		`UPDATE products SET name=$2, price_minor_per_1000=$3, lead_days=$4, pack_size=$5, shelf_life_hours=$6, auto_created=false,
		piece_weight=$7, piece_weight_unit=$8, pack_unit=$9, shelf_after_open_hours=$10 WHERE id=$1`,
		p.ID, p.Name, p.PriceMinorPer1000, p.LeadDays, p.PackSize, shelf, pw, pu, pk, afterOpen); err != nil {
		return CatalogChange{}, fmt.Errorf("store: catalog update product %s: %w", p.ID, err)
	}
	if len(fields) == 0 {
		return CatalogChange{ID: p.ID, Action: CatalogUnchanged}, nil
	}
	return CatalogChange{ID: p.ID, Action: CatalogUpdated, Fields: fields}, nil
}

// pieceUnitArg — единица веса штуки для SQL: nil, если веса нет.
func pieceUnitArg(p CatalogProduct) *string {
	if p.PieceWeight == nil {
		return nil
	}
	u := string(p.PieceWeightUnit)
	return &u
}

// Packing — как продукт считается штуками и упаковками (FS-122). Нулевые поля — «не задано».
type Packing struct {
	Unit            domain.Unit
	PieceWeight     int64       // g или ml на 1 шт; 0 — не задан
	PieceWeightUnit domain.Unit // g или ml
	PackSize        int64       // содержимое упаковки в базовых единицах
	PackUnit        string      // метка упаковки; пусто — позиции в упаковках не принимаются
}

// ProductPacking возвращает базовую единицу, вес штуки и упаковку известных продуктов.
func (d *DB) ProductPacking(ctx context.Context, ids []string) (map[string]Packing, error) {
	rows, err := d.Pool.Query(ctx, `SELECT id, unit, piece_weight, piece_weight_unit, pack_size, pack_unit
		FROM products WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("store: product packing: %w", err)
	}
	defer rows.Close()
	out := map[string]Packing{}
	for rows.Next() {
		var id, u string
		var pw *int64
		var pwu, pk *string
		var pack Packing
		if err := rows.Scan(&id, &u, &pw, &pwu, &pack.PackSize, &pk); err != nil {
			return nil, fmt.Errorf("store: product packing: %w", err)
		}
		pack.Unit = domain.Unit(u)
		if pw != nil && pwu != nil {
			pack.PieceWeight, pack.PieceWeightUnit = *pw, domain.Unit(*pwu)
		}
		if pk != nil {
			pack.PackUnit = *pk
		}
		out[id] = pack
	}
	return out, rows.Err()
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
	ch, err := upsertDishRow(ctx, tx, ds)
	ch.Recipe = ds.Recipe
	return ch, err
}

func upsertDishRow(ctx context.Context, tx pgx.Tx, ds CatalogDish) (CatalogChange, error) {
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
