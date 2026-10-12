package store

import (
	"sort"
	"time"

	"umirhack/internal/domain"
)

// WriteoffForecast — «если ничего не делать»: деньги под риском списания по партиям со сроком
// раньше now+hours. Количество под риском — domain.RiskByBatch (2.6), как у горячих предложений;
// сами предложения в расчёте не участвуют.
type WriteoffForecast struct {
	GeneratedAt time.Time
	Hours       int
	TotalMinor  int64
	Products    []ForecastProduct // по убыванию суммы, затем по id
	Days        []ForecastDay     // по возрастанию даты
}

// ForecastProduct — разбивка по продуктам.
type ForecastProduct struct {
	ProductID  string
	Name       string
	Unit       domain.Unit
	AtRisk     domain.Qty
	TotalMinor int64
}

// ForecastDay — разбивка по дням (дата срока партии в зоне ресторана, YYYY-MM-DD).
type ForecastDay struct {
	Date       string
	TotalMinor int64
}

// ForecastWriteoffs считает прогноз по снимку. Чистая функция снимка: без БД и часов.
// Стоимость партии = round(риск × price_minor_per_1000 / 1000); итоги — суммы стоимостей партий,
// поэтому разбивки сходятся с total_minor копейка в копейку.
func ForecastWriteoffs(snap Snapshot, hours int) WriteoffForecast {
	out := WriteoffForecast{GeneratedAt: snap.Now, Hours: hours, Products: []ForecastProduct{}, Days: []ForecastDay{}}
	horizon := snap.Now.Add(time.Duration(hours) * time.Hour)
	loc := snap.Schedule.Location
	if loc == nil {
		loc = time.UTC
	}
	byProduct := map[string][]domain.Batch{}
	for _, b := range snap.Batches {
		byProduct[b.ProductID] = append(byProduct[b.ProductID], b)
	}
	ids := make([]string, 0, len(byProduct))
	for id := range byProduct {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	days := map[string]int64{}
	for _, id := range ids {
		batches := byProduct[id]
		expires := map[int64]time.Time{}
		for _, b := range batches {
			expires[b.ID] = b.ExpiresAt
		}
		// Проекция идёт по всем партиям продукта: ранние партии съедают спрос раньше поздних.
		row := ForecastProduct{ProductID: id, Name: snap.Products[id].Name, Unit: snap.Products[id].Unit}
		for _, r := range domain.RiskByBatch(batches, snap.RateHour[id], snap.Now, snap.Schedule) {
			exp := expires[r.BatchID]
			if r.Risk <= 0 || !exp.Before(horizon) {
				continue
			}
			cost := (int64(r.Risk)*snap.Price[id] + 500) / 1000
			row.AtRisk += r.Risk
			row.TotalMinor += cost
			days[exp.In(loc).Format("2006-01-02")] += cost
		}
		if row.AtRisk > 0 {
			out.Products = append(out.Products, row)
			out.TotalMinor += row.TotalMinor
		}
	}
	sort.SliceStable(out.Products, func(i, j int) bool { return out.Products[i].TotalMinor > out.Products[j].TotalMinor })
	for d, v := range days {
		out.Days = append(out.Days, ForecastDay{Date: d, TotalMinor: v})
	}
	sort.Slice(out.Days, func(i, j int) bool { return out.Days[i].Date < out.Days[j].Date })
	return out
}
