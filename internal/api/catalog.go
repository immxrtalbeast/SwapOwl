package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"

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
}

type catalogRecipeIn struct {
	ProductID string `json:"product_id"`
	Quantity  string `json:"quantity"`
	Unit      string `json:"unit"`
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
	Dishes   []catalogDishIn    `json:"dishes"`
}

func (rt *Router) catalogRoutes() {
	if rt.deps.Catalog == nil {
		return
	}
	rt.HandleRole(http.MethodPut, "/api/catalog", auth.Operator, rt.putCatalog)
}

// putCatalog: роль admin или ключ operator; вошедший по паролю operator импортировать не может.
func (rt *Router) putCatalog(w http.ResponseWriter, r *http.Request) {
	if p, ok := PrincipalFrom(r.Context()); ok && p.Kind == auth.KindSession && p.Role != auth.Admin {
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
		}
		if _, dup := importUnit[p.ID]; dup && msg == "" {
			bad, msg = "id", fmt.Sprintf("duplicate product id %q", p.ID)
		}
		if msg != "" {
			validationError(w, field(bad), msg)
			return
		}
		importUnit[p.ID] = domain.Unit(p.Unit)
		out.Products = append(out.Products, store.CatalogProduct{ID: p.ID, Name: p.Name, Unit: domain.Unit(p.Unit),
			PriceMinorPer1000: *p.PriceMinorPer1000, LeadDays: *p.LeadDays, PackSize: *p.PackSize, ShelfLifeHours: p.ShelfLifeHours})
	}

	// Продукты рецептов, которых нет в теле, берём из БД: нужны их единицы для разбора количества.
	var external []string
	for _, d := range in.Dishes {
		for _, it := range d.Recipe {
			if _, ok := importUnit[it.ProductID]; !ok {
				external = append(external, it.ProductID)
			}
		}
	}
	known := map[string]domain.Unit{}
	if len(external) > 0 {
		var err error
		if known, err = rt.deps.Catalog.ProductUnits(r.Context(), external); err != nil {
			rt.internalError(w, r, err)
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
		seenItem := map[string]bool{}
		for j, it := range d.Recipe {
			rf := fmt.Sprintf("dishes[%d].recipe[%d].", i, j)
			if !validLabel(it.ProductID) {
				validationError(w, rf+"product_id", "product_id must be 1..128 characters without control characters or edge spaces")
				return
			}
			if seenItem[it.ProductID] {
				validationError(w, rf+"product_id", fmt.Sprintf("product %q is listed twice in the recipe", it.ProductID))
				return
			}
			seenItem[it.ProductID] = true
			base, ok := importUnit[it.ProductID]
			if !ok {
				if base, ok = known[it.ProductID]; !ok {
					WriteError(w, http.StatusBadRequest, CodeUnknownProduct,
						fmt.Sprintf("recipe of dish %q uses unknown product %q: add it to products", d.ID, it.ProductID),
						map[string]any{"field": rf + "product_id"})
					return
				}
			}
			unit := it.Unit
			if unit == "" {
				unit = string(base)
			}
			q, err := units.Parse(it.Quantity, unit, units.Unit(base))
			if err != nil {
				validationError(w, rf+"quantity", quantityMessage(err))
				return
			}
			dish.Recipe = append(dish.Recipe, store.CatalogRecipeItem{ProductID: it.ProductID, Quantity: int64(q)})
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
