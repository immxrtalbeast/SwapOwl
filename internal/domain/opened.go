package domain

import (
	"errors"
	"time"
)

// Ошибки вскрытия упаковки.
var (
	// ErrOpenNoShelf — у продукта нет срока после вскрытия: вскрытие срок не меняет.
	ErrOpenNoShelf = errors.New("opening does not change the shelf life")
	// ErrOpenClosed — партия просрочена, списана или пуста: вскрывать нечего.
	ErrOpenClosed = errors.New("batch is expired or empty")
	// ErrOpenQty — вскрыто больше остатка партии или не положительное количество.
	ErrOpenQty = errors.New("opened quantity exceeds the batch remainder")
)

// OpenRules — правила вскрытия продукта.
type OpenRules struct {
	// AfterOpenHours — срок после вскрытия в часах; 0 — не задан.
	AfterOpenHours int
	// PackSize — содержимое упаковки (бутылки, бруска) в базовых единицах; 1 — упаковки нет.
	PackSize Qty
}

// OpenPlan — результат вскрытия: сколько уходит в дочернюю партию и до какого момента она годна.
type OpenPlan struct {
	Qty            Qty
	ChildExpiresAt time.Time
}

// PlanOpen делит партию вскрытием. qty == nil — вскрыта одна упаковка (PackSize, но не больше
// остатка), а если PackSize ≤ 1 — вся партия. Срок дочерней партии — меньший из
// исходного срока и now + AfterOpenHours: вскрытие срок только сокращает.
func PlanOpen(b Batch, rules OpenRules, qty *Qty, now time.Time) (OpenPlan, error) {
	switch {
	case rules.AfterOpenHours < 1:
		return OpenPlan{}, ErrOpenNoShelf
	case b.Remaining <= 0 || !now.Before(b.ExpiresAt):
		return OpenPlan{}, ErrOpenClosed
	}
	q := b.Remaining
	switch {
	case qty != nil:
		q = *qty
	case rules.PackSize > 1 && rules.PackSize < q:
		q = rules.PackSize
	}
	if q <= 0 || q > b.Remaining {
		return OpenPlan{}, ErrOpenQty
	}
	expires := now.Add(time.Duration(rules.AfterOpenHours) * time.Hour)
	if b.ExpiresAt.Before(expires) {
		expires = b.ExpiresAt
	}
	return OpenPlan{Qty: q, ChildExpiresAt: expires}, nil
}
