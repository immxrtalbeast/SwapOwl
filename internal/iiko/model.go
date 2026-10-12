// Package iiko — типы формата iikoServer API для фейкового сервера и адаптера.
// Пути, поля и формат описаны в docs/iiko.md; всё, что не найдено в публичной
// документации, помечено там [ПРЕДПОЛОЖЕНО].
package iiko

import "encoding/xml"

// Пути фейкового сервера (подмножество iikoServer API).
const (
	PathAuth            = "/resto/api/auth"
	PathLogout          = "/resto/api/logout"
	PathProducts        = "/resto/api/v2/entities/products/list"
	PathAssemblyCharts  = "/resto/api/v2/assemblyCharts/getAll"
	PathIncomingInvoice = "/resto/api/documents/export/incomingInvoice"
	PathOLAP            = "/resto/api/v2/reports/olap"
	PathBalanceStores   = "/resto/api/v2/reports/balance/stores"
	PathStopLists       = "/resto/api/v2/stopLists"
)

// Формат даты и времени запросов (часовой пояс ресторана).
const (
	DateLayout     = "2006-01-02"
	DateTimeLayout = "2006-01-02T15:04:05"
)

// Product — элемент номенклатуры (products/list). Тип: GOODS — продукт, DISH — блюдо.
type Product struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Num      string  `json:"num"`
	Type     string  `json:"type"`
	MainUnit string  `json:"mainUnit"` // кг, л, шт
	Price    float64 `json:"defaultSalePrice,omitempty"`
	Deleted  bool    `json:"deleted"`
	// ShelfLifeHours — срок хранения по умолчанию. [ПРЕДПОЛОЖЕНО]: в iiko такого поля нет.
	ShelfLifeHours int `json:"shelfLifeHours,omitempty"`
}

// AssemblyChart — техкарта блюда (assemblyCharts/getAll, элемент assemblyCharts).
type AssemblyChart struct {
	ID                 string      `json:"id"`
	AssembledProductID string      `json:"assembledProductId"`
	DateFrom           string      `json:"dateFrom"`
	DateTo             *string     `json:"dateTo"`
	AssembledAmount    float64     `json:"assembledAmount"`
	Items              []ChartItem `json:"items"`
}

// ChartItem — строка техкарты. Количество — в основной единице продукта.
type ChartItem struct {
	SortWeight   float64 `json:"sortWeight"`
	ProductID    string  `json:"productId"`
	AmountIn     float64 `json:"amountIn"`
	AmountMiddle float64 `json:"amountMiddle"`
	AmountOut    float64 `json:"amountOut"`
	PackageCount float64 `json:"packageCount"`
}

// AssemblyChartsResponse — ответ assemblyCharts/getAll.
type AssemblyChartsResponse struct {
	KnownRevision     int             `json:"knownRevision"`
	RawAssemblyCharts []AssemblyChart `json:"assemblyCharts"`
}

// IncomingInvoices — корень XML экспорта приходных накладных.
type IncomingInvoices struct {
	XMLName   xml.Name          `xml:"incomingInvoiceDtoes"`
	Documents []IncomingInvoice `xml:"document"`
}

// IncomingInvoice — приходная накладная.
type IncomingInvoice struct {
	ID             string        `xml:"id"`
	DocumentNumber string        `xml:"documentNumber"`
	DateIncoming   string        `xml:"dateIncoming"`
	Status         string        `xml:"status"`
	Supplier       string        `xml:"supplier"`
	DefaultStore   string        `xml:"defaultStore"`
	Items          []InvoiceItem `xml:"items>item"`
}

// InvoiceItem — позиция накладной; Amount в основной единице продукта.
type InvoiceItem struct {
	Num     int     `xml:"num"`
	Product string  `xml:"product"`
	Amount  float64 `xml:"amount"`
	Price   float64 `xml:"price"`
	Sum     float64 `xml:"sum"`
}

// OLAPRequest — тело POST reports/olap.
type OLAPRequest struct {
	ReportType       string                `json:"reportType"`
	GroupByRowFields []string              `json:"groupByRowFields"`
	AggregateFields  []string              `json:"aggregateFields"`
	Filters          map[string]OLAPFilter `json:"filters"`
}

// OLAPFilter — фильтр отчёта; для дат filterType=DateRange.
type OLAPFilter struct {
	FilterType string `json:"filterType"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
}

// OLAPResponse — ответ reports/olap; значения строк — строки или числа.
type OLAPResponse struct {
	Data []map[string]any `json:"data"`
}

// Имена полей OLAP, которые поддерживает фейк.
const (
	OLAPDishID    = "DishId"
	OLAPDishName  = "DishName"
	OLAPOpenDate  = "OpenDate.Typed"
	OLAPOrderID   = "UniqOrderId.Id" // id чека
	OLAPCloseTime = "CloseTime"      // время закрытия чека, yyyy-MM-ddTHH:mm:ss (время ресторана)
	OLAPAmount    = "DishAmountInt"
	OLAPSum       = "DishSumInt"
)

// BalanceItem — строка остатков по складу (reports/balance/stores).
type BalanceItem struct {
	Store   string  `json:"store"`
	Product string  `json:"product"`
	Amount  float64 `json:"amount"`
	Sum     float64 `json:"sum"`
}

// StopItem — позиция стоп-листа. [ПРЕДПОЛОЖЕНО]: в iikoServer записи стоп-листа нет,
// форма заимствована из iikoCloud (stop_lists: productId, balance).
type StopItem struct {
	ProductID string  `json:"productId"`
	Balance   float64 `json:"balance"`
}

// StopListResponse — ответ чтения стоп-листа.
type StopListResponse struct {
	Items []StopItem `json:"items"`
}
