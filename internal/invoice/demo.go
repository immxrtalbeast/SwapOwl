package invoice

import (
	"context"
	"sync"
	"time"
)

// Demo — распознаватель без модели: на любое фото отвечает образцом накладной по справочнику стенда.
// Включается только при DEMO_MODE и пустом ключе; Result.Demo = true, интерфейс так и подписывает.
type Demo struct{}

func ptr[T any](v T) *T { return &v }

// Recognize — образец «ТОРГ-12 № 4471» с типичными для фото случаями: фасовка, «годен до» в строке,
// плохо читаемая строка и позиция не из справочника.
func (Demo) Recognize(_ context.Context, _ Image, catalog []Product) (Result, error) {
	res := Result{Number: "4471", Date: "2026-10-14", Supplier: "ООО «Свежие продукты»", Model: "demo", Demo: true, Lines: []Line{
		{RawName: "Молоко 3,2% 1 л (уп. 12 шт)", ProductID: ptr("milk"), Quantity: "12", Unit: "l", ExpiresOn: nil, ShelfLifeHours: ptr(120), Confidence: "high"},
		{RawName: "Сливки 33% 0,5 л × 6", ProductID: ptr("cream"), Quantity: "3", Unit: "l", ShelfLifeHours: ptr(96), Confidence: "high"},
		{RawName: "Сыр пармезан, кг", ProductID: ptr("parmesan"), Quantity: "1.2", Unit: "kg", ShelfLifeHours: ptr(720), Confidence: "low", Note: "вес плохо читается: 1,2 или 1,7"},
		{RawName: "Томаты черри 250 г × 8", ProductID: ptr("tomato"), Quantity: "2", Unit: "kg", ShelfLifeHours: ptr(144), Confidence: "high"},
		{RawName: "Йогурт греческий 2% 140 г", ProductID: nil, Quantity: "20", Unit: "pcs", ShelfLifeHours: ptr(168), Confidence: "low", Note: "нет в справочнике"},
	}}
	return Clean(res, catalog)
}

// Limiter — не больше perHour распознаваний за скользящий час на экземпляр (один экземпляр — один ресторан).
type Limiter struct {
	mu      sync.Mutex
	perHour int
	times   []time.Time
}

// NewLimiter — perHour < 1 считается 1: нулевой лимит не должен ронять Allow.
func NewLimiter(perHour int) *Limiter { return &Limiter{perHour: max(perHour, 1)} }

// Allow отмечает попытку в момент now; false — лимит исчерпан, retry — когда освободится место.
func (l *Limiter) Allow(now time.Time) (ok bool, retry time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-time.Hour)
	kept := l.times[:0]
	for _, t := range l.times {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	l.times = kept
	if len(l.times) >= l.perHour {
		return false, l.times[0].Sub(cut)
	}
	l.times = append(l.times, now)
	return true, 0
}
