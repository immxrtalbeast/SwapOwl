package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"umirhack/internal/invoice"
)

const (
	// maxInvoiceBody — тело с фото в base64: интерфейс сжимает снимок до ~1600 px, это сотни КБ.
	maxInvoiceBody = 8 << 20
	// CodeNotConfigured — распознавание выключено: нет ключа модели и стенд не демо.
	CodeNotConfigured = "not_configured"
	// CodeUnreadable — на фото не нашлось позиций накладной.
	CodeUnreadable = "unreadable_invoice"
)

// Invoices — распознавание фото накладной (FS-158). nil Recognizer — маршрут отвечает 503 not_configured.
type Invoices struct {
	Recognizer invoice.Recognizer
	Limiter    *invoice.Limiter
	// Wall — реальные часы для лимита: лимит про деньги за вызовы модели, а не про время стенда.
	Wall func() time.Time
}

func (rt *Router) invoiceRoutes() {
	if rt.deps.Invoices == nil || rt.deps.Stock == nil {
		return
	}
	rt.Handle(http.MethodPost, "/api/invoices/recognize", rt.recognizeInvoice)
}

// recognizeInvoice — POST /api/invoices/recognize {"image_base64","media_type"} → черновик поставки.
// Поставку не создаёт: строки проверяет оператор, принятое уходит в POST /api/deliveries.
func (rt *Router) recognizeInvoice(w http.ResponseWriter, r *http.Request) {
	inv := rt.deps.Invoices
	if inv.Recognizer == nil {
		WriteError(w, http.StatusServiceUnavailable, CodeNotConfigured, "распознавание накладных не настроено: нужен ключ модели (ANTHROPIC_API_KEY)", nil)
		return
	}
	var in struct {
		ImageBase64 string `json:"image_base64"`
		MediaType   string `json:"media_type"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxInvoiceBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			validationError(w, "image_base64", "фото больше 8 МБ — уменьшите снимок")
			return
		}
		validationError(w, "", "некорректное тело запроса: ожидается JSON-объект {image_base64, media_type}")
		return
	}
	if !invoice.MediaTypes[in.MediaType] {
		validationError(w, "media_type", "media_type — image/jpeg, image/png или image/webp")
		return
	}
	data, err := base64.StdEncoding.DecodeString(in.ImageBase64)
	if err != nil || len(data) == 0 {
		validationError(w, "image_base64", "image_base64 — непустая строка base64 без префикса data:")
		return
	}
	if inv.Limiter != nil {
		if ok, retry := inv.Limiter.Allow(inv.Wall()); !ok {
			secs := int(math.Ceil(retry.Seconds()))
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			WriteError(w, http.StatusTooManyRequests, CodeTooManyTries, "лимит распознаваний за час исчерпан", map[string]any{"retry_after_seconds": secs})
			return
		}
	}
	products, err := rt.deps.Stock.ListProductStock(r.Context(), rt.deps.Now().UTC())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	catalog := make([]invoice.Product, 0, len(products))
	for _, p := range products {
		catalog = append(catalog, invoice.Product{ID: p.ID, Name: p.Name, Unit: string(p.Unit)})
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	res, err := inv.Recognizer.Recognize(ctx, invoice.Image{MediaType: in.MediaType, Data: data}, catalog)
	switch {
	case errors.Is(err, invoice.ErrUnreadable):
		WriteError(w, http.StatusUnprocessableEntity, CodeUnreadable, "на фото не нашлось позиций накладной — снимите документ целиком, ровно и при хорошем свете", nil)
		return
	case err != nil:
		rt.deps.Log.LogAttrs(r.Context(), slog.LevelError, "invoice recognize failed",
			slog.String("request_id", RequestID(r.Context())), slog.String("error", err.Error()))
		WriteError(w, http.StatusBadGateway, CodeInternal, "сервис распознавания не ответил — попробуйте ещё раз или загрузите CSV", nil)
		return
	}
	rt.audit(r, "invoice_recognize", slog.String("model", res.Model), slog.Int("lines", len(res.Lines)), slog.Int("bytes", len(data)))
	WriteJSON(w, http.StatusOK, res)
}
