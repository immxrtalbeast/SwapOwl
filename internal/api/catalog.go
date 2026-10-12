package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"

	"umirhack/internal/auth"
	"umirhack/internal/domain"
	"umirhack/internal/domain/units"
	"umirhack/internal/store"
)

// CodeUnitConflict — смена единицы у существующего продукта (409).
const CodeUnitConflict = "unit_conflict"

// Catalog — то, что нужно импорту справочников от хранилища (8.3 п. 5).
type Catalog interface {
	ProductUnits(ctx context.Context, ids []string) (map[string]domain.Unit, error)
	ProductPacking(ctx context.Context, ids []string) (map[string]store.Packing, error)
	ImportCatalog(ctx context.Context, in store.CatalogInput, dryRun bool) (store.CatalogDiff, error)
}

const (
	maxCatalogRows  = 5000
	maxRecipeItems  = 100
	maxLeadDays     = 3650
	maxPackSize     = 1_000_000_000_000
	maxPriceMinor   = 1_000_000_000_000
	maxPriorDaily   = 1_000_000
	catalogBaseUnit = "g, ml or pcs"
)

type catalogProductIn struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Unit              string `json:"unit"`
	PriceMinorPer1000 *int64 `json:"price_minor_per_1000"`
	LeadDays          *int   `json:"lead_days"`
	PackSize          *int64 `json:"pack_size"`
	ShelfLifeHours    *int   `json:"shelf_life_hours"`
	// PieceWeight — g или ml в одной штуке (только unit=pcs), PieceWeightUnit — g (по умолчанию) или ml.
	PieceWeight     *int64 `json:"piece_weight"`
	PieceWeightUnit string `json:"piece_weight_unit"`
	// PackUnit — метка упаковки; pack_size тогда — содержимое упаковки в базовых единицах.
	PackUnit *string `json:"pack_unit"`
}

// catalogRecipeIn — строка техкарты: сырьё {product_id, gross | net [+ loss_pct] | quantity}
// или полуфабрикат {prep_id, quantity}. quantity у сырья — то же, что gross (старый формат).
type catalogRecipeIn struct {
	ProductID string   `json:"product_id"`
	PrepID    string   `json:"prep_id"`
	Quantity  string   `json:"quantity"`
	Gross     string   `json:"gross"`
	Net       string   `json:"net"`
	LossPct   *float64 `json:"loss_pct"`
	Unit      string   `json:"unit"`
}

// catalogPrepIn — полуфабрикат: из recipe получается yield единиц (unit, по умолчанию g).
type catalogPrepIn struct {
	ID        string            `json:"id"`
	Unit      string            `json:"unit"`
	Yield     string            `json:"yield"`
	YieldUnit string            `json:"yield_unit"`
	Recipe    []catalogRecipeIn `json:"recipe"`
}

type catalogDishIn struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	PriceMinor *int64            `json:"price_minor"`
	PriorDaily *float64          `json:"prior_daily"`
	Recipe     []catalogRecipeIn `json:"recipe"`
}

type catalogIn struct {
	Products []catalogProductIn `json:"products"`
	Preps    []catalogPrepIn    `json:"preps"`
	Dishes   []catalogDishIn    `json:"dishes"`
}

func (rt *Router) catalogRoutes() {
	if rt.deps.Catalog == nil {
		return
	}
	rt.HandleRole(http.MethodPut, "/api/catalog", auth.Operator, rt.putCatalog)
}

// putCatalog: роль admin или ключ API (operator и выше, минимум держит роутер). Пользователь-operator
// по паролю и анонимный operator открытого стенда (AUTH_REQUIRED=false) импортировать не могут.
func (rt *Router) putCatalog(w http.ResponseWriter, r *http.Request) {
	if p, ok := PrincipalFrom(r.Context()); !ok || (p.Role != auth.Admin && p.Kind != auth.KindAPIKey) {
		WriteError(w, http.StatusForbidden, CodeForbidden, "catalog import needs the admin role or an operator API key", nil)
		return
	}
	dry := false
	switch r.URL.Query().Get("dry_run") {
	case "", "false":
	case "true":
		dry = true
	default:
		validationError(w, "dry_run", "dry_run must be true or false")
		return
	}
	var in catalogIn
	if !decodeBody(w, r, &in) {
		return
	}
	if len(in.Products)+len(in.Dishes) == 0 {
		validationError(w, "products", "products or dishes must not be empty")
		return
	}
	if len(in.Products) > maxCatalogRows || len(in.Dishes) > maxCatalogRows {
		validationError(w, "products", fmt.Sprintf("at most %d products and %d dishes per import", maxCatalogRows, maxCatalogRows))
		return
	}

	out := store.CatalogInput{}
	importUnit := map[string]domain.Unit{}
	importPiece := map[string]pieceWeight{}
	for i, p := range in.Products {
		field := func(f string) string { return fmt.Sprintf("products[%d].%s", i, f) }
		var msg, bad string
		switch {
		case !validLabel(p.ID):
			bad, msg = "id", "id must be 1..128 characters without control characters or edge spaces"
		case !validLabel(p.Name):
			bad, msg = "name", "name must be 1..128 characters without control characters or edge spaces"
		case p.Unit != "g" && p.Unit != "ml" && p.Unit != "pcs":
			bad, msg = "unit", "unit must be "+catalogBaseUnit
		case p.PriceMinorPer1000 == nil || *p.PriceMinorPer1000 < 0 || *p.PriceMinorPer1000 > maxPriceMinor:
			bad, msg = "price_minor_per_1000", "price_minor_per_1000 is required, an integer >= 0"
		case p.LeadDays == nil || *p.LeadDays < 0 || *p.LeadDays > maxLeadDays:
			bad, msg = "lead_days", fmt.Sprintf("lead_days is required, an integer in 0..%d", maxLeadDays)
		case p.PackSize == nil || *p.PackSize < 1 || *p.PackSize > maxPackSize:
			bad, msg = "pack_size", "pack_size is required, an integer >= 1"
		case p.ShelfLifeHours != nil && (*p.ShelfLifeHours < 1 || *p.ShelfLifeHours > maxShelfHours):
			bad, msg = "shelf_life_hours", fmt.Sprintf("shelf_life_hours must be an integer in 1..%d", maxShelfHours)
		case p.PieceWeight == nil && p.PieceWeightUnit != "":
			bad, msg = "piece_weight_unit", "piece_weight_unit needs piece_weight"
		case p.PieceWeight != nil && p.Unit != "pcs":
			bad, msg = "piece_weight", "piece_weight is allowed only for unit pcs"
		case p.PieceWeight != nil && (*p.PieceWeight < 1 || *p.PieceWeight > maxPackSize):
			bad, msg = "piece_weight", "piece_weight must be an integer >= 1 (g or ml per piece)"
		case p.PieceWeightUnit != "" && p.PieceWeightUnit != "g" && p.PieceWeightUnit != "ml":
			bad, msg = "piece_weight_unit", "piece_weight_unit must be g or ml"
		case p.PackUnit != nil && (!validLabel(*p.PackUnit) || len(*p.PackUnit) > 32):
			bad, msg = "pack_unit", "pack_unit must be 1..32 characters without control characters or edge spaces"
		}
		if _, dup := importUnit[p.ID]; dup && msg == "" {
			bad, msg = "id", fmt.Sprintf("duplicate product id %q", p.ID)
		}
		if msg != "" {
			validationError(w, field(bad), msg)
			return
		}
		importUnit[p.ID] = domain.Unit(p.Unit)
		cp := store.CatalogProduct{ID: p.ID, Name: p.Name, Unit: domain.Unit(p.Unit),
			PriceMinorPer1000: *p.PriceMinorPer1000, LeadDays: *p.LeadDays, PackSize: *p.PackSize, ShelfLifeHours: p.ShelfLifeHours,
			PieceWeight: p.PieceWeight, PackUnit: p.PackUnit}
		if p.PieceWeight != nil {
			cp.PieceWeightUnit = domain.UnitG
			if p.PieceWeightUnit == "ml" {
				cp.PieceWeightUnit = domain.UnitMl
			}
			importPiece[p.ID] = pieceWeight{*p.PieceWeight, cp.PieceWeightUnit}
		}
		out.Products = append(out.Products, cp)
	}

	// Продукты рецептов, которых нет в теле, берём из БД: нужны их единицы для разбора количества.
	var external []string
	collect := func(items []catalogRecipeIn) {
		for _, it := range items {
			if _, ok := importUnit[it.ProductID]; !ok && it.ProductID != "" {
				external = append(external, it.ProductID)
			}
		}
	}
	for _, d := range in.Dishes {
		collect(d.Recipe)
	}
	for _, pr := range in.Preps {
		collect(pr.Recipe)
	}
	known := map[string]domain.Unit{}
	knownPiece := map[string]pieceWeight{}
	if len(external) > 0 {
		var err error
		if known, err = rt.deps.Catalog.ProductUnits(r.Context(), external); err != nil {
			rt.internalError(w, r, err)
			return
		}
		packing, err := rt.deps.Catalog.ProductPacking(r.Context(), external)
		if err != nil {
			rt.internalError(w, r, err)
			return
		}
		for id, pk := range packing {
			if pk.PieceWeight > 0 {
				knownPiece[id] = pieceWeight{pk.PieceWeight, pk.PieceWeightUnit}
			}
		}
	}
	pieceOf := func(id string) (pieceWeight, bool) {
		if pw, ok := importPiece[id]; ok {
			return pw, true
		}
		if _, inBody := importUnit[id]; inBody {
			return pieceWeight{}, false // продукт из тела: вес штуки берём только из тела
		}
		pw, ok := knownPiece[id]
		return pw, ok
	}
	rawUnit := func(id string) (domain.Unit, bool) {
		if u, ok := importUnit[id]; ok {
			return u, true
		}
		u, ok := known[id]
		return u, ok
	}

	if len(in.Preps) > maxCatalogRows {
		validationError(w, "preps", fmt.Sprintf("at most %d preps per import", maxCatalogRows))
		return
	}
	preps, prepUnit := map[string]domain.Prep{}, map[string]domain.Unit{}
	for i, pr := range in.Preps {
		field := func(f string) string { return fmt.Sprintf("preps[%d].%s", i, f) }
		bad, msg := "", ""
		unit := pr.Unit
		if unit == "" {
			unit = "g"
		}
		switch {
		case !validLabel(pr.ID):
			bad, msg = "id", "id must be 1..128 characters without control characters or edge spaces"
		case unit != "g" && unit != "ml" && unit != "pcs":
			bad, msg = "unit", "unit must be "+catalogBaseUnit
		case len(pr.Recipe) == 0 || len(pr.Recipe) > maxRecipeItems:
			bad, msg = "recipe", fmt.Sprintf("recipe must contain 1..%d items", maxRecipeItems)
		}
		if _, dup := preps[pr.ID]; dup && msg == "" {
			bad, msg = "id", fmt.Sprintf("duplicate prep id %q", pr.ID)
		}
		if msg != "" {
			validationError(w, field(bad), msg)
			return
		}
		yu := pr.YieldUnit
		if yu == "" {
			yu = unit
		}
		y, err := units.Parse(pr.Yield, yu, units.Unit(unit))
		if err != nil {
			validationError(w, field("yield"), quantityMessage(err, "yield"))
			return
		}
		preps[pr.ID] = domain.Prep{ID: pr.ID, Yield: int64(y)}
		prepUnit[pr.ID] = domain.Unit(unit)
	}
	for i, pr := range in.Preps {
		lines, bad, msg := parseTechLines(pr.Recipe, fmt.Sprintf("preps[%d].recipe", i), rawUnit, pieceOf, prepUnit)
		if msg != "" {
			rt.techcardError(w, bad, msg, pr.ID)
			return
		}
		p := preps[pr.ID]
		p.Recipe = lines
		preps[pr.ID] = p
	}
	exp := domain.NewTechcardExpander(preps)
	// Цикл ловим сразу во всех полуфабрикатах, даже если их не использует ни одно блюдо.
	for i, pr := range in.Preps {
		if _, err := exp.Expand([]domain.TechLine{{PrepID: pr.ID, Qty: preps[pr.ID].Yield}}); err != nil {
			validationError(w, fmt.Sprintf("preps[%d].recipe", i), techcardMessage(err))
			return
		}
	}

	seenDish := map[string]bool{}
	for i, d := range in.Dishes {
		field := func(f string) string { return fmt.Sprintf("dishes[%d].%s", i, f) }
		var msg, bad string
		switch {
		case !validLabel(d.ID):
			bad, msg = "id", "id must be 1..128 characters without control characters or edge spaces"
		case seenDish[d.ID]:
			bad, msg = "id", fmt.Sprintf("duplicate dish id %q", d.ID)
		case !validLabel(d.Name):
			bad, msg = "name", "name must be 1..128 characters without control characters or edge spaces"
		case d.PriceMinor == nil || *d.PriceMinor < 0 || *d.PriceMinor > maxPriceMinor:
			bad, msg = "price_minor", "price_minor is required, an integer >= 0"
		case d.PriorDaily == nil || math.IsNaN(*d.PriorDaily) || *d.PriorDaily < 0 || *d.PriorDaily > maxPriorDaily:
			bad, msg = "prior_daily", "prior_daily is required, a number >= 0"
		case len(d.Recipe) == 0 || len(d.Recipe) > maxRecipeItems:
			bad, msg = "recipe", fmt.Sprintf("recipe must contain 1..%d items", maxRecipeItems)
		}
		if msg != "" {
			validationError(w, field(bad), msg)
			return
		}
		seenDish[d.ID] = true
		dish := store.CatalogDish{ID: d.ID, Name: d.Name, PriceMinor: *d.PriceMinor, PriorDaily: *d.PriorDaily}
		lines, bad, msg := parseTechLines(d.Recipe, fmt.Sprintf("dishes[%d].recipe", i), rawUnit, pieceOf, prepUnit)
		if msg != "" {
			rt.techcardError(w, bad, msg, d.ID)
			return
		}
		norms, err := exp.Expand(lines)
		if err != nil {
			validationError(w, fmt.Sprintf("dishes[%d].recipe", i), techcardMessage(err))
			return
		}
		for _, n := range norms {
			if n.Quantity > int64(units.Limit) {
				validationError(w, fmt.Sprintf("dishes[%d].recipe", i), fmt.Sprintf("expanded norm of %q exceeds the quantity limit", n.ProductID))
				return
			}
			dish.Recipe = append(dish.Recipe, store.CatalogRecipeItem{ProductID: n.ProductID, Quantity: n.Quantity})
		}
		out.Dishes = append(out.Dishes, dish)
	}

	diff, err := rt.deps.Catalog.ImportCatalog(r.Context(), out, dry)
	var uc *store.UnitChangeError
	var ci *store.CatalogInvalidError
	switch {
	case err == nil:
	case errors.As(err, &uc):
		WriteError(w, http.StatusConflict, CodeUnitConflict,
			fmt.Sprintf("product %q already exists with unit %s; unit cannot change", uc.ProductID, uc.Have),
			map[string]any{"product_id": uc.ProductID, "current_unit": uc.Have, "import_unit": uc.Got})
		return
	case errors.As(err, &ci):
		WriteError(w, http.StatusBadRequest, CodeValidation, "the imported catalog fails the start checks", map[string]any{"problems": ci.Problems})
		return
	default:
		rt.internalError(w, r, err)
		return
	}
	if !dry {
		rt.audit(r, "catalog.import", slog.Int("products", len(diff.Products)), slog.Int("dishes", len(diff.Dishes)))
	}
	WriteJSON(w, http.StatusOK, diff)
}

// techcardError отвечает на ошибку строки техкарты: неизвестный продукт — отдельным кодом.
func (rt *Router) techcardError(w http.ResponseWriter, field, msg, owner string) {
	if strings.HasPrefix(msg, unknownProductPrefix) {
		WriteError(w, http.StatusBadRequest, CodeUnknownProduct,
			fmt.Sprintf("recipe of %q uses unknown product %q: add it to products", owner, strings.TrimPrefix(msg, unknownProductPrefix)),
			map[string]any{"field": field})
		return
	}
	validationError(w, field, msg)
}

const unknownProductPrefix = "\x00unknown product:"

func techcardMessage(err error) string {
	switch {
	case errors.Is(err, domain.ErrTechcardCycle):
		return "recipe has a cycle of preps: " + err.Error()
	case errors.Is(err, domain.ErrTechcardUnknown):
		return "recipe references an unknown prep: " + err.Error()
	case errors.Is(err, domain.ErrOverflow):
		return "expanded norm overflows"
	}
	return err.Error()
}

// parseTechLines разбирает строки техкарты в базовые единицы. При ошибке возвращает поле и текст.
func parseTechLines(items []catalogRecipeIn, prefix string, rawUnit func(string) (domain.Unit, bool), pieceOf func(string) (pieceWeight, bool), prepUnit map[string]domain.Unit) ([]domain.TechLine, string, string) {
	if len(items) == 0 || len(items) > maxRecipeItems {
		return nil, prefix, fmt.Sprintf("recipe must contain 1..%d items", maxRecipeItems)
	}
	var lines []domain.TechLine
	seen := map[string]bool{}
	for j, it := range items {
		rf := fmt.Sprintf("%s[%d].", prefix, j)
		if (it.ProductID == "") == (it.PrepID == "") {
			return nil, rf + "product_id", "exactly one of product_id and prep_id is required"
		}
		id := it.ProductID
		if it.PrepID != "" {
			id = "prep:" + it.PrepID
		}
		if !validLabel(it.ProductID+it.PrepID) || (it.ProductID != "" && !validLabel(it.ProductID)) || (it.PrepID != "" && !validLabel(it.PrepID)) {
			return nil, rf + "product_id", "product_id and prep_id must be 1..128 characters without control characters or edge spaces"
		}
		if seen[id] {
			return nil, rf + "product_id", fmt.Sprintf("%q is listed twice in the recipe", it.ProductID+it.PrepID)
		}
		seen[id] = true

		var base domain.Unit
		qty, field := it.Quantity, "quantity"
		line := domain.TechLine{ProductID: it.ProductID, PrepID: it.PrepID}
		if it.PrepID != "" {
			var ok bool
			if base, ok = prepUnit[it.PrepID]; !ok {
				return nil, rf + "prep_id", fmt.Sprintf("unknown prep %q: add it to preps", it.PrepID)
			}
			if it.Gross != "" || it.Net != "" || it.LossPct != nil {
				return nil, rf + "quantity", "a prep line takes only quantity"
			}
		} else {
			var ok bool
			if base, ok = rawUnit(it.ProductID); !ok {
				return nil, rf + "product_id", unknownProductPrefix + it.ProductID
			}
			set := 0
			for _, v := range []string{it.Quantity, it.Gross, it.Net} {
				if v != "" {
					set++
				}
			}
			switch {
			case set != 1:
				return nil, rf + "gross", "exactly one of gross, net and quantity is required"
			case it.Gross != "":
				qty, field = it.Gross, "gross"
			case it.Net != "":
				qty, field = it.Net, "net"
			}
			if it.LossPct != nil && it.Net == "" {
				return nil, rf + "loss_pct", "loss_pct is allowed only together with net"
			}
			if it.LossPct != nil {
				lp := *it.LossPct
				bp := math.Round(lp * 100)
				if math.IsNaN(lp) || lp < 0 || lp >= 100 || math.Abs(lp*100-bp) > 1e-6 {
					return nil, rf + "loss_pct", "loss_pct must be a number in 0..99.99 with at most two decimals"
				}
				line.LossBP = int(bp)
			}
		}
		unit := it.Unit
		if unit == "" {
			unit = string(base)
		}
		q, err := units.Parse(qty, unit, units.Unit(base))
		if errors.Is(err, units.ErrDimension) && it.PrepID == "" && base == domain.UnitPcs {
			// Штучный продукт с нормой в g/ml: пересчёт по весу штуки (docs/catalog.md, «Штучные продукты»).
			if pw, ok := pieceOf(it.ProductID); ok {
				q, err = piecesFromWeight(qty, unit, pw, line.LossBP)
				if err == nil {
					line.LossBP = 0 // потери уже учтены в граммах
				}
			}
		}
		if err != nil {
			return nil, rf + field, quantityMessage(err, field)
		}
		line.Qty = int64(q)
		lines = append(lines, line)
	}
	return lines, "", ""
}

// pieceWeight — вес одной штуки продукта в g или ml.
type pieceWeight struct {
	Weight int64
	Unit   domain.Unit
}

// piecesFromWeight переводит норму в g/ml в штуки: потери net → gross считаются в весе, затем
// результат делится на вес штуки с округлением вверх до целой штуки на порцию. Целочисленно.
func piecesFromWeight(qty, unit string, pw pieceWeight, lossBP int) (units.Qty, error) {
	w, err := units.Parse(qty, unit, units.Unit(pw.Unit))
	if err != nil {
		return 0, err
	}
	if lossBP > 0 {
		den := int64(10000 - lossBP)
		if int64(w) > math.MaxInt64/10000 {
			return 0, units.ErrOverflow
		}
		w = units.Qty((int64(w)*10000 + den - 1) / den)
	}
	pcs := (int64(w) + pw.Weight - 1) / pw.Weight
	if pcs > int64(units.Limit) {
		return 0, units.ErrLimit
	}
	return units.Qty(pcs), nil
}
