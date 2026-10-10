// Package hotoffers — общие типы алгоритмов горячих предложений
// (internal/hotoffers/a — R, internal/hotoffers/b — Bg).
package hotoffers

import (
	"time"

	"umirhack/internal/domain"
)

// Config — параметры алгоритмов.
type Config struct {
	TopN       int     // HOT_TOP_N
	WMin       float64 // HOT_W_MIN_HOURS
	CoverHours float64 // HOT_COVER_HOURS
	MaxIter    int     // HOT_MAX_ITER
}

// Input — снимок для ранжирования. Чистые функции от Input.
type Input struct {
	Now      time.Time
	Batches  []domain.Batch     // только доступные
	Dishes   []domain.Dish      // с рецептами
	Stopped  map[string]bool    // id блюд в стоп-листе
	Price    map[string]int64   // product_id → копеек за 1000 базовых единиц
	RateHour map[string]float64 // ожидаемая потребность в рабочий час; R её не читает
	Schedule domain.Schedule
	Config   Config
}

// Offer — горячее предложение.
type Offer struct {
	DishID         string
	Priority       float64
	TargetPortions int64    // 0 у R
	Reasons        []Reason // не пусто
}

// Reason — ссылка на конкретную партию.
type Reason struct {
	ProductID        string
	BatchID          int64
	AtRisk           domain.Qty // у R — остаток опорных партий
	ExpiresAt        time.Time
	WorkingHoursLeft float64
}

// Excluded — блюдо, исключённое предохранителем Bg.
type Excluded struct{ DishID, ProductID, Why string }

// Ranker — общая сигнатура Rank у обоих алгоритмов.
type Ranker func(in Input) (offers []Offer, excluded []Excluded)
