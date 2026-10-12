package fake

import (
	"bytes"
	"crypto/sha1" //nolint:gosec
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"umirhack/internal/iiko"
)

func newTS(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	loc, _ := time.LoadLocation("Europe/Moscow")
	s, err := New(Options{Seed: 7, Origin: time.Date(2026, 1, 1, 0, 0, 0, 0, loc), Login: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	sum := sha1.Sum([]byte("p")) //nolint:gosec
	resp, err := http.Post(ts.URL+iiko.PathAuth+"?login=u&pass="+hex.EncodeToString(sum[:]), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || len(b) == 0 {
		t.Fatalf("auth: %d %s", resp.StatusCode, b)
	}
	return ts, string(b)
}

func get(t *testing.T, u string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestAuth(t *testing.T) {
	ts, tok := newTS(t)
	if c, _ := get(t, ts.URL+iiko.PathProducts); c != 401 {
		t.Fatalf("без токена: %d", c)
	}
	if c, _ := get(t, ts.URL+iiko.PathProducts+"?key=bad"); c != 401 {
		t.Fatalf("плохой токен: %d", c)
	}
	resp, _ := http.Post(ts.URL+iiko.PathAuth+"?login=u&pass=00", "", nil)
	if resp.StatusCode != 401 {
		t.Fatalf("плохой пароль: %d", resp.StatusCode)
	}
	if c, _ := get(t, ts.URL+iiko.PathProducts+"?key="+tok); c != 200 {
		t.Fatalf("products: %d", c)
	}
}

func TestProductsAndCharts(t *testing.T) {
	ts, tok := newTS(t)
	_, b := get(t, ts.URL+iiko.PathProducts+"?key="+tok)
	var ps []iiko.Product
	if err := json.Unmarshal(b, &ps); err != nil || len(ps) != 20 {
		t.Fatalf("products: %v n=%d", err, len(ps))
	}
	_, b = get(t, ts.URL+iiko.PathAssemblyCharts+"?key="+tok)
	var cr iiko.AssemblyChartsResponse
	if err := json.Unmarshal(b, &cr); err != nil || len(cr.RawAssemblyCharts) != 8 {
		t.Fatalf("charts: %v", err)
	}
	// паста с лососем: 150 г лосося = 0.15 кг
	if got := cr.RawAssemblyCharts[0].Items[0].AmountIn; got != 0.15 {
		t.Fatalf("amountIn = %v", got)
	}
}

func TestInvoicesDeterministic(t *testing.T) {
	ts, tok := newTS(t)
	u := ts.URL + iiko.PathIncomingInvoice + "?key=" + tok + "&from=2026-03-01&to=2026-03-03"
	_, a := get(t, u)
	_, b := get(t, u)
	if !bytes.Equal(a, b) {
		t.Fatal("ответы различаются")
	}
	var inv iiko.IncomingInvoices
	if err := xml.Unmarshal(a, &inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Documents) != 3 || len(inv.Documents[0].Items) == 0 {
		t.Fatalf("документов %d", len(inv.Documents))
	}
	if c, _ := get(t, ts.URL+iiko.PathIncomingInvoice+"?key="+tok+"&from=x&to=y"); c != 400 {
		t.Fatalf("плохие даты: %d", c)
	}
}

func olap(t *testing.T, ts *httptest.Server, tok string, req iiko.OLAPRequest) iiko.OLAPResponse {
	t.Helper()
	body, _ := json.Marshal(req)
	resp, err := http.Post(ts.URL+iiko.PathOLAP+"?key="+tok, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("olap: %d %s", resp.StatusCode, b)
	}
	var out iiko.OLAPResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOLAPSales(t *testing.T) {
	ts, tok := newTS(t)
	req := iiko.OLAPRequest{
		ReportType:       "SALES",
		GroupByRowFields: []string{iiko.OLAPDishID},
		AggregateFields:  []string{iiko.OLAPAmount, iiko.OLAPSum},
		Filters:          map[string]iiko.OLAPFilter{iiko.OLAPOpenDate: {FilterType: "DateRange", From: "2026-03-01", To: "2026-03-08"}},
	}
	r := olap(t, ts, tok, req)
	if len(r.Data) == 0 || r.Data[0][iiko.OLAPAmount].(float64) <= 0 {
		t.Fatalf("нет продаж: %v", r.Data)
	}
	// разбивка по дням даёт ту же сумму
	req.GroupByRowFields = []string{iiko.OLAPOpenDate}
	var total, byDay float64
	for _, row := range r.Data {
		total += row[iiko.OLAPAmount].(float64)
	}
	for _, row := range olap(t, ts, tok, req).Data {
		byDay += row[iiko.OLAPAmount].(float64)
	}
	if total != byDay {
		t.Fatalf("%v != %v", total, byDay)
	}
	body, _ := json.Marshal(iiko.OLAPRequest{ReportType: "TRANSACTIONS"})
	resp, _ := http.Post(ts.URL+iiko.PathOLAP+"?key="+tok, "application/json", bytes.NewReader(body))
	if resp.StatusCode != 400 {
		t.Fatalf("тип отчёта: %d", resp.StatusCode)
	}
}

func TestBalance(t *testing.T) {
	ts, tok := newTS(t)
	q := url.Values{"key": {tok}, "timestamp": {"2026-01-03T12:00:00"}}
	_, b := get(t, ts.URL+iiko.PathBalanceStores+"?"+q.Encode())
	var items []iiko.BalanceItem
	if err := json.Unmarshal(b, &items); err != nil || len(items) != 12 {
		t.Fatalf("balance: %v n=%d", err, len(items))
	}
	for _, it := range items {
		if it.Amount < 0 {
			t.Fatalf("минус: %+v", it)
		}
	}
	q.Set("timestamp", "2025-01-01T00:00:00")
	_, b = get(t, ts.URL+iiko.PathBalanceStores+"?"+q.Encode())
	if string(bytes.TrimSpace(b)) != "[]" {
		t.Fatalf("до origin: %s", b)
	}
}

func TestStopList(t *testing.T) {
	ts, tok := newTS(t)
	u := ts.URL + iiko.PathStopLists + "?key=" + tok
	body, _ := json.Marshal(iiko.StopListResponse{Items: []iiko.StopItem{{ProductID: "caesar"}}})
	resp, err := http.Post(u, "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("post: %v %v", err, resp)
	}
	_, b := get(t, u)
	var got iiko.StopListResponse
	if err := json.Unmarshal(b, &got); err != nil || len(got.Items) != 1 || got.Items[0].ProductID != "caesar" {
		t.Fatalf("get: %v %s", err, b)
	}
	body, _ = json.Marshal(iiko.StopListResponse{Items: []iiko.StopItem{{ProductID: "nope"}}})
	resp, _ = http.Post(u, "application/json", bytes.NewReader(body))
	if resp.StatusCode != 400 {
		t.Fatalf("неизвестный id: %d", resp.StatusCode)
	}
}

func TestPieceProductPrice(t *testing.T) {
	ts, tok := newTS(t)
	_, b := get(t, ts.URL+iiko.PathIncomingInvoice+"?key="+tok+"&from=2026-03-01&to=2026-03-03")
	var inv iiko.IncomingInvoices
	if err := xml.Unmarshal(b, &inv); err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{"bun": 0.015, "egg": 0.012, "salmon": 1800}
	seen := 0
	for _, d := range inv.Documents {
		for _, it := range d.Items {
			if w, ok := want[it.Product]; ok {
				seen++
				if it.Price < w*0.999 || it.Price > w*1.001 {
					t.Errorf("%s: цена %v, ожидалась %v", it.Product, it.Price, w)
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("нет проверяемых позиций")
	}
}

func postXML(t *testing.T, u string, doc iiko.PurchaseOrder) (int, iiko.DocumentValidationResult) {
	t.Helper()
	b, _ := xml.Marshal(doc)
	resp, err := http.Post(u, "application/xml", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var res iiko.DocumentValidationResult
	_ = xml.Unmarshal(body, &res)
	return resp.StatusCode, res
}

// Заказ поставщику: импорт принимается, повтор с тем же id заменяет документ, а не дублирует.
func TestPurchaseOrderImportIdempotent(t *testing.T) {
	ts, tok := newTS(t)
	imp := ts.URL + iiko.PathPurchaseImport + "?key=" + tok
	exp := ts.URL + iiko.PathPurchaseExport + "?key=" + tok
	doc := iiko.PurchaseOrder{ID: "o-1", DocumentNumber: "1", DateIncoming: "2026-01-02T10:00:00", Supplier: SupplierID, DefaultStore: StoreID,
		Items: []iiko.PurchaseOrderItem{{Num: 1, Product: "salmon", Amount: 4}}}
	for i := 0; i < 2; i++ {
		if c, res := postXML(t, imp, doc); c != 200 || !res.Valid {
			t.Fatalf("импорт #%d: %d %+v", i, c, res)
		}
	}
	doc.Items[0].Amount = 6 // тот же id с другим составом — замена
	if c, res := postXML(t, imp, doc); c != 200 || !res.Valid {
		t.Fatalf("замена: %d %+v", c, res)
	}
	c, b := get(t, exp)
	var out iiko.PurchaseOrders
	if c != 200 || xml.Unmarshal(b, &out) != nil || len(out.Documents) != 1 || out.Documents[0].Items[0].Amount != 6 {
		t.Fatalf("чтение: %d %s", c, b)
	}
	if c, b = get(t, exp+"&id=other"); c != 200 || bytes.Contains(b, []byte("<document>")) {
		t.Fatalf("фильтр по id: %d %s", c, b)
	}
	bad := doc
	bad.ID, bad.Items = "o-2", []iiko.PurchaseOrderItem{{Num: 1, Product: "no-such", Amount: 1}}
	if _, res := postXML(t, imp, bad); res.Valid || res.ErrorMessage == "" {
		t.Fatalf("неизвестный продукт принят: %+v", res)
	}
	if c, _ := get(t, ts.URL+iiko.PathPurchaseExport); c != 401 {
		t.Fatalf("без токена: %d", c)
	}
}
