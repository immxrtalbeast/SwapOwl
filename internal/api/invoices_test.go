package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"umirhack/internal/invoice"
)

type fakeRecognizer struct {
	res     invoice.Result
	err     error
	catalog []invoice.Product
	img     invoice.Image
}

func (f *fakeRecognizer) Recognize(_ context.Context, img invoice.Image, catalog []invoice.Product) (invoice.Result, error) {
	f.img, f.catalog = img, catalog
	return f.res, f.err
}

func postInvoice(t *testing.T, inv *Invoices, body string) *httptest.ResponseRecorder {
	t.Helper()
	rt := NewRouter(Deps{Now: func() time.Time { return standTime }, Stock: &fakeStock{}, Invoices: inv})
	req := httptest.NewRequest(http.MethodPost, "/api/invoices/recognize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, req)
	return rec
}

var jpeg = `{"media_type":"image/jpeg","image_base64":"` + base64.StdEncoding.EncodeToString([]byte("\xff\xd8\xff photo")) + `"}`

func TestRecognizeInvoiceReturnsDraft(t *testing.T) {
	milk := "milk"
	f := &fakeRecognizer{res: invoice.Result{Number: "4471", Model: "m", Lines: []invoice.Line{{RawName: "Молоко", ProductID: &milk, Quantity: "12", Unit: "l", Confidence: "high"}}}}
	rec := postInvoice(t, &Invoices{Recognizer: f, Wall: time.Now}, jpeg)
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body)
	}
	var got invoice.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Number != "4471" || len(got.Lines) != 1 || *got.Lines[0].ProductID != "milk" {
		t.Fatalf("body %s", rec.Body)
	}
	if f.img.MediaType != "image/jpeg" || string(f.img.Data) != "\xff\xd8\xff photo" {
		t.Fatalf("image not passed: %+v", f.img)
	}
}

func TestRecognizeInvoiceErrors(t *testing.T) {
	f := &fakeRecognizer{}
	for name, c := range map[string]struct {
		inv  *Invoices
		body string
		code int
		want string
	}{
		"нет ключа":          {&Invoices{Wall: time.Now}, jpeg, 503, CodeNotConfigured},
		"не тот тип":         {&Invoices{Recognizer: f, Wall: time.Now}, `{"media_type":"image/gif","image_base64":"AA=="}`, 400, CodeValidation},
		"не base64":          {&Invoices{Recognizer: f, Wall: time.Now}, `{"media_type":"image/png","image_base64":"data:image/png;base64,AA"}`, 400, CodeValidation},
		"пусто":              {&Invoices{Recognizer: f, Wall: time.Now}, `{"media_type":"image/png","image_base64":""}`, 400, CodeValidation},
		"лишнее поле":        {&Invoices{Recognizer: f, Wall: time.Now}, `{"media_type":"image/png","image_base64":"AA==","x":1}`, 400, CodeValidation},
		"не накладная":       {&Invoices{Recognizer: &fakeRecognizer{err: invoice.ErrUnreadable}, Wall: time.Now}, jpeg, 422, CodeUnreadable},
		"модель не ответила": {&Invoices{Recognizer: &fakeRecognizer{err: context.DeadlineExceeded}, Wall: time.Now}, jpeg, 502, CodeInternal},
	} {
		rec := postInvoice(t, c.inv, c.body)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), `"code":"`+c.want+`"`) {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}

func TestRecognizeInvoiceRateLimited(t *testing.T) {
	inv := &Invoices{Recognizer: &fakeRecognizer{res: invoice.Result{Lines: []invoice.Line{{RawName: "x"}}}}, Limiter: invoice.NewLimiter(1), Wall: time.Now}
	if rec := postInvoice(t, inv, jpeg); rec.Code != 200 {
		t.Fatalf("first: %d %s", rec.Code, rec.Body)
	}
	rec := postInvoice(t, inv, jpeg)
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("second: %d %s", rec.Code, rec.Body)
	}
}
