package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/domain/units"
	"umirhack/internal/store"
)

// Stock — то, что нужно маршрутам склада от хранилища.
type Stock interface {
	ProductUnits(ctx context.Context, ids []string) (map[string]domain.Unit, error)
	ReceiveDelivery(ctx context.Context, in store.DeliveryInput, now func() time.Time, loc *time.Location) (store.DeliveryResponse, error)
	ListDeliveries(ctx context.Context) ([]store.DeliveryResponse, error)
	GetDelivery(ctx context.Context, id string) (store.DeliveryResponse, error)
	ListProductStock(ctx context.Context, now time.Time) ([]store.ProductStock, error)
	ListBatches(ctx context.Context, now time.Time, productID, status string) ([]store.BatchView, error)
}

const (
	maxDeliveryItems = 500
	maxShelfHours    = 87600
)

// inputBase — базовая единица по единице ввода (для продукта, которого ещё нет в справочнике).
var inputBase = map[string]domain.Unit{
	"g": domain.UnitG, "kg": domain.UnitG,
	"ml": domain.UnitMl, "l": domain.UnitMl,
	"pcs": domain.UnitPcs,
}

type deliveryItemIn struct {
	ProductID      string `json:"product_id"`
	Name           string `json:"name"`
	Quantity       string `json:"quantity"`
	Unit           string `json:"unit"`
	ShelfLifeHours *int   `json:"shelf_life_hours"`
	ExpiresOn      string `json:"expires_on"`
}

type deliveryIn struct {
	ID        string `json:"id"`
	Number    string `json:"number"`
	CreatedAt string `json:"created_at"`
	// OccurredAt — когда поставка пришла фактически (RFC 3339, необязательно, 2.4а).
	OccurredAt string           `json:"occurred_at"`
	Items      []deliveryItemIn `json:"items"`
}

func (rt *Router) location() *time.Location {
	if rt.deps.Location != nil {
		return rt.deps.Location
	}
	return time.UTC
}

func (rt *Router) stockRoutes() {
	if rt.deps.Stock == nil {
		return
	}
	rt.Handle(http.MethodPost, "/api/deliveries", rt.postDelivery)
	rt.Handle(http.MethodGet, "/api/deliveries", rt.listDeliveries)
	rt.Handle(http.MethodGet, "/api/deliveries/{id}", rt.getDelivery)
	rt.Handle(http.MethodGet, "/api/products", rt.listProducts)
	rt.Handle(http.MethodGet, "/api/batches", rt.listBatches)
}

func (rt *Router) postDelivery(w http.ResponseWriter, r *http.Request) {
	var in deliveryIn
	if !decodeBody(w, r, &in) {
		return
	}
	switch {
	case !validLabel(in.ID):
		validationError(w, "id", "id must be 1..128 characters without control characters or edge spaces")
		return
	case !validLabel(in.Number):
		validationError(w, "number", "number must be 1..128 characters without control characters or edge spaces")
		return
	case len(in.Items) == 0 || len(in.Items) > maxDeliveryItems:
		validationError(w, "items", fmt.Sprintf("items must contain 1..%d positions", maxDeliveryItems))
		return
	}
	created, err := time.Parse(time.RFC3339, in.CreatedAt)
	if err != nil {
		validationError(w, "created_at", "created_at must be an RFC 3339 timestamp")
		return
	}
	occurred, ok := parseOccurredAt(w, in.OccurredAt)
	if !ok {
		return
	}
	ids := make([]string, 0, len(in.Items))
	for i, it := range in.Items {
		if !validLabel(it.ProductID) {
			validationError(w, fmt.Sprintf("items[%d].product_id", i), "product_id must be 1..128 characters without control characters or edge spaces")
			return
		}
		ids = append(ids, it.ProductID)
	}
	known, err := rt.deps.Stock.ProductUnits(r.Context(), ids)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}

	out := store.DeliveryInput{ID: in.ID, Number: in.Number, CreatedAt: created, OccurredAt: occurred, Items: make([]store.DeliveryItem, 0, len(in.Items))}
	for i, it := range in.Items {
		field := func(f string) string { return fmt.Sprintf("items[%d].%s", i, f) }
		hasHours, hasDate := it.ShelfLifeHours != nil, it.ExpiresOn != ""
		if hasHours && hasDate {
			validationError(w, field("shelf_life_hours"), "shelf_life_hours and expires_on are mutually exclusive")
			return
		}
		di := store.DeliveryItem{ProductID: it.ProductID, Name: strings.TrimSpace(it.Name)}
		if len(di.Name) > maxLoginLen {
			validationError(w, field("name"), "name must be at most 128 bytes")
			return
		}
		switch {
		case hasHours:
			if *it.ShelfLifeHours < 1 || *it.ShelfLifeHours > maxShelfHours {
				validationError(w, field("shelf_life_hours"), fmt.Sprintf("shelf_life_hours must be an integer in 1..%d", maxShelfHours))
				return
			}
			di.ShelfLifeHours = *it.ShelfLifeHours
		case hasDate:
			if _, err := time.Parse("2006-01-02", it.ExpiresOn); err != nil {
				validationError(w, field("expires_on"), "expires_on must be a date YYYY-MM-DD")
				return
			}
			di.ExpiresOn = it.ExpiresOn
		}
		base, ok := known[it.ProductID]
		unit := it.Unit
		switch {
		case ok && unit == "":
			unit = string(base)
		case !ok && unit == "":
			WriteError(w, http.StatusBadRequest, CodeUnknownProduct,
				fmt.Sprintf("unknown product %q: unit is required to create it", it.ProductID),
				map[string]any{"field": field("unit"), "example": map[string]any{"product_id": it.ProductID, "name": "Название", "quantity": "1.5", "unit": "kg", "shelf_life_hours": 72}})
			return
		case !ok:
			b, valid := inputBase[unit]
			if !valid {
				validationError(w, field("unit"), "unit must be one of g, kg, ml, l, pcs")
				return
			}
			base = b
		}
		q, err := units.Parse(it.Quantity, unit, units.Unit(base))
		if err != nil {
			bad := "quantity"
			if errors.Is(err, units.ErrUnknownUnit) {
				bad = "unit"
			}
			validationError(w, field(bad), quantityMessage(err, bad))
			return
		}
		di.Unit, di.Qty = base, int64(q)
		out.Items = append(out.Items, di)
	}

	resp, err := rt.deps.Stock.ReceiveDelivery(r.Context(), out, rt.deps.Now, rt.location())
	switch {
	case err == nil:
	case errors.Is(err, store.ErrDeliveryConflict):
		WriteError(w, http.StatusConflict, CodeDeliveryConf, "delivery id already used with different content", map[string]any{"delivery_id": in.ID})
		return
	case errors.Is(err, store.ErrEventOutsideWindow):
		eventWindowError(w, err)
		return
	case errors.Is(err, store.ErrNoShelfLife):
		var nsl *store.NoShelfLifeError
		errors.As(err, &nsl)
		validationError(w, fmt.Sprintf("items[%d].shelf_life_hours", nsl.Index),
			fmt.Sprintf("product %q has no default shelf life: pass shelf_life_hours or expires_on", nsl.ProductID))
		return
	case errors.Is(err, store.ErrUnitMismatch):
		validationError(w, "items", "unit does not match the product's base unit")
		return
	default:
		rt.internalError(w, r, err)
		return
	}
	status := http.StatusCreated
	if resp.Replayed {
		status = http.StatusOK
	}
	WriteJSON(w, status, resp)
}

// quantityMessage называет поле и причину отказа: field — имя поля без пути (quantity, qty, counted).
func quantityMessage(err error, field string) string {
	switch {
	case errors.Is(err, units.ErrFormat):
		return field + " must be a string like \"1.5\": digits with an optional fractional part"
	case errors.Is(err, units.ErrUnknownUnit):
		return field + ": unit must be one of g, kg, ml, l, pcs"
	case errors.Is(err, units.ErrDimension):
		return field + ": unit does not match the product's dimension"
	case errors.Is(err, units.ErrFraction):
		return field + " is finer than the base unit"
	case errors.Is(err, units.ErrFractionalPcs):
		return field + ": pieces cannot be fractional"
	case errors.Is(err, units.ErrZero):
		return field + " must be greater than zero"
	case errors.Is(err, units.ErrLimit), errors.Is(err, units.ErrOverflow):
		return field + " exceeds the limit"
	}
	return field + " is not a valid quantity"
}

func (rt *Router) listDeliveries(w http.ResponseWriter, r *http.Request) {
	items, err := rt.deps.Stock.ListDeliveries(r.Context())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, items)
}

func (rt *Router) getDelivery(w http.ResponseWriter, r *http.Request) {
	d, err := rt.deps.Stock.GetDelivery(r.Context(), r.PathValue("id"))
	switch {
	case err == nil:
		WriteJSON(w, http.StatusOK, d)
	case errors.Is(err, store.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "delivery not found", nil)
	default:
		rt.internalError(w, r, err)
	}
}
