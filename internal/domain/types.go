package domain

import "time"

// Unit — базовая единица измерения продукта.
type Unit string

// Базовые единицы.
const (
	UnitG   Unit = "g"
	UnitMl  Unit = "ml"
	UnitPcs Unit = "pcs"
)

// Qty — количество в базовых единицах. Арифметика только через units.Add / units.Mul.
type Qty int64

// Batch — партия: одна позиция поставки.
type Batch struct {
	ID         int64
	ProductID  string
	Quantity   Qty
	Remaining  Qty
	ReceivedAt time.Time
	ExpiresAt  time.Time
}

// BatchStatus — статус партии по сроку.
type BatchStatus string

// Статусы партии.
const (
	StatusOK       BatchStatus = "ok"
	StatusExpiring BatchStatus = "expiring"
	StatusExpired  BatchStatus = "expired"
)

// ExpiryConfig — параметры порога «истекает»:
// t_ост < min(max(Share·срок, MinHours), MaxShare·срок).
type ExpiryConfig struct {
	Share    float64
	MinHours float64
	MaxShare float64
}

// Need — суммарная потребность заказа в продукте.
type Need struct {
	ProductID string
	Qty       Qty
}

// Writeoff — часть плана списания: сколько взять из партии.
type Writeoff struct {
	BatchID   int64
	ProductID string
	Qty       Qty
}

// Shortage — нехватка продукта для заказа.
type Shortage struct {
	ProductID string
	Required  Qty
	Available Qty
	Missing   Qty
}

// RecipeItem — норма продукта на одну порцию.
type RecipeItem struct {
	ProductID string
	Quantity  Qty
}

// Dish — блюдо с рецептом.
type Dish struct {
	ID     string
	Name   string
	Recipe []RecipeItem
}

// StoppedDish — блюдо в стоп-листе с причиной.
type StoppedDish struct {
	Dish    Dish
	Missing []Shortage // на одну порцию
}

// Schedule — часы работы ресторана в его часовом поясе.
type Schedule struct {
	Location *time.Location
	OpenAt   time.Duration // от начала суток
	CloseAt  time.Duration // от начала суток, OpenAt < CloseAt
}

// BatchRisk — часть остатка партии, которую не успеют израсходовать.
type BatchRisk struct {
	BatchID          int64
	Risk             Qty
	WorkingHoursLeft float64
	Doomed           bool // W(now, expires) < W_min
}

// SupplyUrgency — срочность предложения поставки.
type SupplyUrgency string

// Срочность предложения поставки.
const (
	UrgencyNow     SupplyUrgency = "now"
	UrgencySoon    SupplyUrgency = "soon"
	UrgencyPlanned SupplyUrgency = "planned"
)

// SupplyProduct — продукт для расчёта предложения поставки.
type SupplyProduct struct {
	ID            string
	Name          string
	LeadDays      int
	PackSize      Qty
	UsableStock   Qty     // Σ (remaining − risk)
	RatePerDay    float64 // ожидаемая потребность, базовых единиц в день
	MaxRecipeNorm Qty     // наибольшая норма среди блюд
	BlocksDishes  int     // сколько блюд из стоп-листа блокирует
}

// SupplyInput — вход SupplySuggestions.
type SupplyInput struct {
	Now         time.Time
	Products    []SupplyProduct
	CoverDays   int
	MinPortions int64
}

// SupplySuggestion — предложение поставки.
type SupplySuggestion struct {
	ProductID    string
	ProductName  string
	Quantity     Qty
	UsableStock  Qty
	RatePerDay   Qty
	StockoutAt   *time.Time // nil при нулевом темпе
	BlocksDishes int
	Urgency      SupplyUrgency
	Explanation  string
}

// Status возвращает статус партии на момент now. Заглушка.
func Status(b Batch, now time.Time, cfg ExpiryConfig) BatchStatus {
	return ""
// PlanWriteoff строит план FEFO по (ExpiresAt, ID). Партии уже отфильтрованы
// по remaining > 0 и now < ExpiresAt. Возвращает либо план, либо нехватку — не оба.
// Заглушка контракта: логика не реализована.
func PlanWriteoff(needs []Need, batches []Batch) ([]Writeoff, []Shortage) {
	return nil, nil
}

// Servings — сколько порций собирается из available: min floor(доступно/норма). Заглушка.
func Servings(recipe []RecipeItem, available map[string]Qty) int64 {
	return 0
}

// StopList возвращает блюда, для которых нельзя собрать ни одной порции. Заглушка.
func StopList(dishes []Dish, available map[string]Qty) []StoppedDish {
	return nil
}

// SupplySuggestions — предложение необходимых поставок. Заглушка.
func SupplySuggestions(in SupplyInput) []SupplySuggestion {
	return nil
}
