package seed

import (
	"time"

	"umirhack/internal/domain"
	"umirhack/internal/seed/catalog"
)

// Типы каталога.
type (
	Product      = catalog.Product
	Dish         = catalog.Dish
	Hours        = catalog.Hours
	DeliveryItem = catalog.DeliveryItem
	Delivery     = catalog.Delivery
)

// WorkingHours — см. catalog.WorkingHours.
func WorkingHours() Hours { return catalog.WorkingHours() }

// Products — см. catalog.Products.
func Products() []Product { return catalog.Products() }

// Dishes — см. catalog.Dishes.
func Dishes() []Dish { return catalog.Dishes() }

// StartingDeliveries — см. catalog.StartingDeliveries.
func StartingDeliveries(t0 time.Time) []Delivery { return catalog.StartingDeliveries(t0) }

// DailyNeed — см. catalog.DailyNeed.
func DailyNeed() map[string]float64 { return catalog.DailyNeed() }

// RoundToPack — см. catalog.RoundToPack.
func RoundToPack(q, pack domain.Qty) domain.Qty { return catalog.RoundToPack(q, pack) }

// FormatQty — см. catalog.FormatQty.
func FormatQty(q domain.Qty, base domain.Unit) (string, string) { return catalog.FormatQty(q, base) }

// DemoProductIDs — см. catalog.DemoProductIDs.
func DemoProductIDs() map[string]bool { return catalog.DemoProductIDs() }
