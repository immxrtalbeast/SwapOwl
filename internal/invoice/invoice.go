// Package invoice — распознавание фото накладной поставщика в строки поставки.
//
// Распознаватель только читает документ и сопоставляет строки со справочником: поставку он не создаёт.
// Результат — черновик, который оператор проверяет в предпросмотре (принять, поправить количество,
// отметить брак) и отправляет обычным POST /api/deliveries; сроки считает сервер, как у CSV-импорта.
package invoice

import (
	"context"
	"errors"
)

// Product — строка справочника, по которой модель сопоставляет позиции накладной.
type Product struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Unit string `json:"unit"` // g, ml, pcs
}

// Line — позиция накладной. Quantity — строка с числом в Unit, как в POST /api/deliveries.
type Line struct {
	RawName        string  `json:"raw_name"`
	ProductID      *string `json:"product_id"`
	Quantity       string  `json:"quantity"`
	Unit           string  `json:"unit"`
	ExpiresOn      *string `json:"expires_on"`       // YYYY-MM-DD, если «годен до» напечатан в строке
	ShelfLifeHours *int    `json:"shelf_life_hours"` // если напечатан срок хранения
	Confidence     string  `json:"confidence"`       // high | low
	Note           string  `json:"note,omitempty"`
}

// Result — распознанная накладная. Date — дата документа, а не срок годности.
type Result struct {
	Number   string `json:"number"`
	Date     string `json:"date,omitempty"`
	Supplier string `json:"supplier,omitempty"`
	Lines    []Line `json:"lines"`
	Model    string `json:"model"`
	// Demo — результат демо-распознавателя (ключ модели не задан): образец, а не чтение фото.
	Demo bool `json:"demo"`
}

// Image — фото накладной: JPEG, PNG или WebP.
type Image struct {
	MediaType string
	Data      []byte
}

// Recognizer читает фото накладной.
type Recognizer interface {
	Recognize(ctx context.Context, img Image, catalog []Product) (Result, error)
}

// ErrUnreadable — на фото не нашлось накладной или ни одной позиции.
var ErrUnreadable = errors.New("invoice: no invoice lines on the photo")

// MediaTypes — допустимые типы изображения.
var MediaTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}
