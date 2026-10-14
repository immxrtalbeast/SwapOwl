package invoice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Claude — распознаватель на Claude (vision + структурированный ответ по JSON-схеме).
type Claude struct {
	client anthropic.Client
	model  string
}

// NewClaude — клиент с ключом apiKey; model — id модели (INVOICE_MODEL); opts — для тестов (адрес сервера).
func NewClaude(apiKey, model string, opts ...option.RequestOption) *Claude {
	return &Claude{client: anthropic.NewClient(append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)...), model: model}
}

const systemPrompt = `Ты читаешь фото накладной поставщика ресторана (ТОРГ-12, УПД, счёт, рукописная накладная) и переносишь позиции в поставку.

Правила:
- Каждая товарная строка документа — одна позиция. Итоги, НДС, подписи, реквизиты — не позиции.
- raw_name — наименование как в документе.
- product_id — id продукта из справочника, если позиция — это он (учитывай бренды, жирность, фасовку: «Молоко 3,2% 1л» → молоко). Если подходящего нет — null. Не выдумывай id.
- quantity и unit — общее количество в одной из единиц g, kg, ml, l, pcs, той же размерности, что у продукта справочника. Фасовку пересчитай: «10 уп × 1 л» → "10" и "l"; «5 шт по 0,5 кг» для продукта в граммах → "2.5" и "kg". quantity — строка с числом, разделитель точка, без пробелов.
- Срок: expires_on (YYYY-MM-DD), только если у позиции напечатан «годен до» / «срок годности до»; shelf_life_hours — если напечатан срок хранения (сутки × 24). Дата документа — НЕ срок годности. Нет срока — оба null.
- confidence = "low", если строка плохо читается, количество или единица под сомнением или product_id выбран неуверенно; в note — коротко, что под сомнением.
- number — номер документа; date — дата документа (YYYY-MM-DD); supplier — поставщик. Нет — пустая строка.
- Если на фото нет накладной — lines пустой.`

func nullable(t string) map[string]any {
	return map[string]any{"anyOf": []any{map[string]any{"type": t}, map[string]any{"type": "null"}}}
}

var resultSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"number":   map[string]any{"type": "string"},
		"date":     map[string]any{"type": "string"},
		"supplier": map[string]any{"type": "string"},
		"lines": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"raw_name":         map[string]any{"type": "string"},
					"product_id":       nullable("string"),
					"quantity":         map[string]any{"type": "string"},
					"unit":             map[string]any{"type": "string", "enum": []string{"g", "kg", "ml", "l", "pcs"}},
					"expires_on":       nullable("string"),
					"shelf_life_hours": nullable("integer"),
					"confidence":       map[string]any{"type": "string", "enum": []string{"high", "low"}},
					"note":             map[string]any{"type": "string"},
				},
				"required":             []string{"raw_name", "product_id", "quantity", "unit", "expires_on", "shelf_life_hours", "confidence", "note"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"number", "date", "supplier", "lines"},
	"additionalProperties": false,
}

// Recognize отправляет фото и справочник в Claude и возвращает позиции; product_id не из справочника обнуляется.
func (c *Claude) Recognize(ctx context.Context, img Image, catalog []Product) (Result, error) {
	var cat strings.Builder
	cat.WriteString("Справочник продуктов (id — название — базовая единица):\n")
	for _, p := range catalog {
		fmt.Fprintf(&cat, "%s — %s — %s\n", p.ID, p.Name, p.Unit)
	}
	msg, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 8000,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(
			anthropic.NewImageBlockBase64(img.MediaType, base64.StdEncoding.EncodeToString(img.Data)),
			anthropic.NewTextBlock(cat.String()+"\nПеренеси позиции накладной с фото."),
		)},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffortLow,
			Format: anthropic.JSONOutputFormatParam{Schema: resultSchema},
		},
	})
	if err != nil {
		return Result{}, fmt.Errorf("invoice: claude: %w", err)
	}
	if msg.StopReason == anthropic.StopReasonRefusal {
		return Result{}, ErrUnreadable
	}
	var text string
	for _, b := range msg.Content {
		if t, ok := b.AsAny().(anthropic.TextBlock); ok {
			text += t.Text
		}
	}
	var res Result
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		return Result{}, fmt.Errorf("invoice: parse model output (stop %s): %w", msg.StopReason, err)
	}
	res.Model = c.model
	return Clean(res, catalog)
}

// Clean обнуляет product_id не из справочника и отбрасывает строки без названия; пустой результат — ErrUnreadable.
func Clean(res Result, catalog []Product) (Result, error) {
	known := make(map[string]bool, len(catalog))
	for _, p := range catalog {
		known[p.ID] = true
	}
	lines := res.Lines[:0]
	for _, l := range res.Lines {
		if strings.TrimSpace(l.RawName) == "" {
			continue
		}
		if l.ProductID != nil && !known[*l.ProductID] {
			l.ProductID = nil
			l.Confidence = "low"
		}
		if l.Confidence != "high" {
			l.Confidence = "low"
		}
		lines = append(lines, l)
	}
	res.Lines = lines
	if len(res.Lines) == 0 {
		return Result{}, ErrUnreadable
	}
	return res, nil
}
