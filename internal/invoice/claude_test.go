package invoice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
)

// Запрос к Messages API: модель, фото, справочник в тексте и JSON-схема ответа; ответ модели разбирается
// и проходит Clean (id не из справочника обнуляется).
func TestClaudeRequestAndParse(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("X-Api-Key") != "k" {
			t.Errorf("path %s key %q", r.URL.Path, r.Header.Get("X-Api-Key"))
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		out := `{"number":"77","date":"2026-10-14","supplier":"ООО Ромашка","lines":[` +
			`{"raw_name":"Молоко 3,2% 1л","product_id":"milk","quantity":"12","unit":"l","expires_on":null,"shelf_life_hours":120,"confidence":"high","note":""},` +
			`{"raw_name":"Креветки","product_id":"shrimp","quantity":"1","unit":"kg","expires_on":"2026-10-20","shelf_life_hours":null,"confidence":"high","note":""}]}`
		text, _ := json.Marshal(out)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-5-5","stop_reason":"end_turn",`+
			`"content":[{"type":"text","text":`+string(text)+`}],"usage":{"input_tokens":10,"output_tokens":10}}`)
	}))
	defer srv.Close()

	res, err := NewClaude("k", "claude-haiku-5-5", option.WithBaseURL(srv.URL), option.WithMaxRetries(0)).
		Recognize(context.Background(), Image{MediaType: "image/jpeg", Data: []byte("img")}, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if res.Number != "77" || res.Model != "claude-haiku-5-5" || len(res.Lines) != 2 || *res.Lines[0].ProductID != "milk" || *res.Lines[0].ShelfLifeHours != 120 {
		t.Fatalf("res %+v", res)
	}
	if res.Lines[1].ProductID != nil || res.Lines[1].Confidence != "low" || *res.Lines[1].ExpiresOn != "2026-10-20" {
		t.Fatalf("неизвестный id не обнулён: %+v", res.Lines[1])
	}
	if got["model"] != "claude-haiku-5-5" {
		t.Fatalf("model %v", got["model"])
	}
	oc, _ := got["output_config"].(map[string]any)
	format, _ := oc["format"].(map[string]any)
	if format["type"] != "json_schema" || format["schema"] == nil {
		t.Fatalf("output_config %v", got["output_config"])
	}
	msgs := got["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].([]any)
	img := content[0].(map[string]any)
	if img["type"] != "image" || img["source"].(map[string]any)["media_type"] != "image/jpeg" {
		t.Fatalf("image block %v", img)
	}
}
