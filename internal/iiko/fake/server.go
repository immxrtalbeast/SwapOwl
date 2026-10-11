// Package fake — HTTP-сервер, повторяющий подмножество iikoServer API.
// Данные детерминированы по seed: справочники из internal/seed, поток накладных
// и продаж из internal/generator. Сервер не вызывает time.Now(): время — параметр запроса.
package fake

import (
	"crypto/sha1" //nolint:gosec // iikoServer требует SHA-1 пароля
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"umirhack/internal/generator"
	"umirhack/internal/iiko"
	"umirhack/internal/seed"
)

// MaxSpan — наибольший интервал, который сервер генерирует за один запрос.
const MaxSpan = 366 * 24 * time.Hour

// StoreID — единственный склад фейка.
const StoreID = "main-store"

// SupplierID — единственный поставщик фейка.
const SupplierID = "supplier-1"

// Options — настройки сервера.
type Options struct {
	Seed     int64
	Origin   time.Time // начало истории: остатки считаются с этого момента
	Login    string
	Password string
}

// Server — фейковый iikoServer.
type Server struct {
	opt   Options
	loc   *time.Location
	token string
	mu    sync.Mutex
	stop  []iiko.StopItem
}

// New создаёт сервер.
func New(opt Options) (*Server, error) {
	if opt.Login == "" || opt.Password == "" {
		return nil, fmt.Errorf("fake iiko: логин и пароль обязательны")
	}
	loc, err := time.LoadLocation(seed.WorkingHours().TZ)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s", opt.Seed, opt.Login, opt.Password)))
	h := hex.EncodeToString(sum[:16])
	tok := fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
	return &Server{opt: opt, loc: loc, token: tok}, nil
}

// Handler возвращает HTTP-обработчик.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(iiko.PathAuth, s.auth)
	mux.HandleFunc(iiko.PathLogout, s.keyed(s.logout))
	mux.HandleFunc(iiko.PathProducts, s.keyed(s.products))
	mux.HandleFunc(iiko.PathAssemblyCharts, s.keyed(s.charts))
	mux.HandleFunc(iiko.PathIncomingInvoice, s.keyed(s.invoices))
	mux.HandleFunc(iiko.PathOLAP, s.keyed(s.olap))
	mux.HandleFunc(iiko.PathBalanceStores, s.keyed(s.balance))
	mux.HandleFunc(iiko.PathStopLists, s.keyed(s.stopList))
	return mux
}

func (s *Server) keyed(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" || key != s.token {
			http.Error(w, "Token is expired or invalid", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

func (s *Server) auth(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sum := sha1.Sum([]byte(s.opt.Password)) //nolint:gosec
	if q.Get("login") != s.opt.Login || !strings.EqualFold(q.Get("pass"), hex.EncodeToString(sum[:])) {
		http.Error(w, "Wrong login or password", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, s.token)
}

func (s *Server) logout(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, "ok")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func bad(w http.ResponseWriter, msg string) { http.Error(w, msg, http.StatusBadRequest) }

// mainUnit и коэффициент перевода базовой единицы в основную (г→кг, мл→л).
func mainUnit(u string) (string, float64) {
	switch u {
	case "g":
		return "кг", 1000
	case "ml":
		return "л", 1000
	default:
		return "шт", 1
	}
}

// unitPrice — руб за основную единицу продукта (цена в сиде — копейки за 1000 базовых).
func unitPrice(p seed.Product) float64 {
	_, k := mainUnit(string(p.Unit))
	return float64(p.PriceMinorPer1000) / 100 * k / 1000
}

func round(v float64, n int) float64 {
	p := math.Pow10(n)
	return math.Round(v*p) / p
}

func (s *Server) products(w http.ResponseWriter, _ *http.Request) {
	var out []iiko.Product
	for _, p := range seed.Products() {
		u, _ := mainUnit(string(p.Unit))
		out = append(out, iiko.Product{ID: p.ID, Name: p.Name, Num: p.ID, Type: "GOODS", MainUnit: u, ShelfLifeHours: p.ShelfLifeHours})
	}
	for _, d := range seed.Dishes() {
		out = append(out, iiko.Product{ID: d.ID, Name: d.Name, Num: d.ID, Type: "DISH", MainUnit: "порц", Price: float64(d.PriceMinor) / 100})
	}
	writeJSON(w, out)
}

func (s *Server) charts(w http.ResponseWriter, _ *http.Request) {
	units := map[string]float64{}
	for _, p := range seed.Products() {
		_, k := mainUnit(string(p.Unit))
		units[p.ID] = k
	}
	resp := iiko.AssemblyChartsResponse{RawAssemblyCharts: []iiko.AssemblyChart{}}
	for _, d := range seed.Dishes() {
		c := iiko.AssemblyChart{ID: "chart-" + d.ID, AssembledProductID: d.ID, DateFrom: s.opt.Origin.In(s.loc).Format(iiko.DateLayout), AssembledAmount: 1}
		for i, it := range d.Recipe {
			a := float64(it.Quantity) / units[it.ProductID]
			c.Items = append(c.Items, iiko.ChartItem{SortWeight: float64(i), ProductID: it.ProductID, AmountIn: a, AmountMiddle: a, AmountOut: a, PackageCount: 1})
		}
		resp.RawAssemblyCharts = append(resp.RawAssemblyCharts, c)
	}
	writeJSON(w, resp)
}

// parseTime разбирает дату или дату-время в часовом поясе ресторана.
func (s *Server) parseTime(v string) (time.Time, error) {
	if t, err := time.ParseInLocation(iiko.DateTimeLayout, v, s.loc); err == nil {
		return t, nil
	}
	return time.ParseInLocation(iiko.DateLayout, v, s.loc)
}

func (s *Server) span(from, to time.Time) ([]generator.Event, error) {
	if !from.Before(to) {
		return nil, fmt.Errorf("from должно быть раньше to")
	}
	if to.Sub(from) > MaxSpan {
		return nil, fmt.Errorf("интервал больше %d суток", int(MaxSpan.Hours()/24))
	}
	return generator.Generate(s.opt.Seed, from, to)
}

func itemAmount(it seed.DeliveryItem) (float64, error) {
	v, err := strconv.ParseFloat(it.Quantity, 64)
	if err != nil {
		return 0, err
	}
	switch it.Unit {
	case "kg", "l":
		return v, nil
	case "g", "ml":
		return v / 1000, nil
	default:
		return v, nil
	}
}

func (s *Server) invoices(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err1 := s.parseTime(q.Get("from"))
	to, err2 := s.parseTime(q.Get("to"))
	if err1 != nil || err2 != nil {
		bad(w, "from и to: yyyy-MM-dd")
		return
	}
	to = to.AddDate(0, 0, 1) // to включительно по дню
	evs, err := s.span(from, to)
	if err != nil {
		bad(w, err.Error())
		return
	}
	prices := map[string]float64{}
	for _, p := range seed.Products() {
		prices[p.ID] = unitPrice(p)
	}
	out := iiko.IncomingInvoices{}
	for _, e := range evs {
		if e.Kind != generator.KindDelivery {
			continue
		}
		d := e.Delivery
		doc := iiko.IncomingInvoice{ID: d.ID, DocumentNumber: d.Number, DateIncoming: e.At.In(s.loc).Format(iiko.DateTimeLayout), Status: "PROCESSED", Supplier: SupplierID, DefaultStore: StoreID}
		for i, it := range d.Items {
			a, err := itemAmount(it)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			pr := prices[it.ProductID]
			doc.Items = append(doc.Items, iiko.InvoiceItem{Num: i + 1, Product: it.ProductID, Amount: a, Price: round(pr, 5), Sum: round(pr*a, 2)})
		}
		out.Documents = append(out.Documents, doc)
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	fmt.Fprint(w, xml.Header)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(out)
}

func (s *Server) olap(w http.ResponseWriter, r *http.Request) {
	var req iiko.OLAPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		bad(w, "некорректный JSON: "+err.Error())
		return
	}
	if req.ReportType != "SALES" {
		bad(w, "поддержан только reportType=SALES")
		return
	}
	f, ok := req.Filters[iiko.OLAPOpenDate]
	if !ok || f.FilterType != "DateRange" {
		bad(w, "нужен фильтр OpenDate.Typed типа DateRange")
		return
	}
	from, err1 := s.parseTime(f.From)
	to, err2 := s.parseTime(f.To)
	if err1 != nil || err2 != nil {
		bad(w, "некорректные from/to фильтра")
		return
	}
	for _, g := range req.GroupByRowFields {
		if g != iiko.OLAPDishID && g != iiko.OLAPDishName && g != iiko.OLAPOpenDate {
			bad(w, "неизвестное поле группировки "+g)
			return
		}
	}
	for _, a := range req.AggregateFields {
		if a != iiko.OLAPAmount && a != iiko.OLAPSum {
			bad(w, "неизвестное поле агрегата "+a)
			return
		}
	}
	evs, err := s.span(from, to)
	if err != nil {
		bad(w, err.Error())
		return
	}
	dishes := map[string]seed.Dish{}
	for _, d := range seed.Dishes() {
		dishes[d.ID] = d
	}
	type acc struct {
		row    map[string]any
		amount float64
		sum    float64
	}
	groups := map[string]*acc{}
	var keys []string
	for _, e := range evs {
		if e.Kind != generator.KindOrder {
			continue
		}
		for _, it := range e.Order.Items {
			d := dishes[it.DishID]
			row := map[string]any{}
			var kp []string
			for _, g := range req.GroupByRowFields {
				var v string
				switch g {
				case iiko.OLAPDishID:
					v = d.ID
				case iiko.OLAPDishName:
					v = d.Name
				case iiko.OLAPOpenDate:
					v = e.At.In(s.loc).Format(iiko.DateLayout)
				}
				row[g] = v
				kp = append(kp, v)
			}
			k := strings.Join(kp, "\x00")
			a := groups[k]
			if a == nil {
				a = &acc{row: row}
				groups[k] = a
				keys = append(keys, k)
			}
			a.amount += float64(it.Portions)
			a.sum += float64(it.Portions) * float64(d.PriceMinor) / 100
		}
	}
	sort.Strings(keys)
	resp := iiko.OLAPResponse{Data: []map[string]any{}}
	for _, k := range keys {
		a := groups[k]
		for _, ag := range req.AggregateFields {
			if ag == iiko.OLAPAmount {
				a.row[ag] = a.amount
			} else {
				a.row[ag] = round(a.sum, 2)
			}
		}
		resp.Data = append(resp.Data, a.row)
	}
	writeJSON(w, resp)
}

// balance: остаток = приход − расход по рецептам с Origin до timestamp, не ниже нуля.
// Сроки годности не учитываются. [ПРЕДПОЛОЖЕНО]
func (s *Server) balance(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ts, err := s.parseTime(q.Get("timestamp"))
	if err != nil {
		bad(w, "timestamp: yyyy-MM-dd'T'HH:mm:ss")
		return
	}
	if !ts.After(s.opt.Origin) {
		writeJSON(w, []iiko.BalanceItem{})
		return
	}
	evs, err := s.span(s.opt.Origin, ts)
	if err != nil {
		bad(w, err.Error())
		return
	}
	recipes := map[string][]seedItem{}
	for _, d := range seed.Dishes() {
		for _, it := range d.Recipe {
			recipes[d.ID] = append(recipes[d.ID], seedItem{it.ProductID, float64(it.Quantity)})
		}
	}
	bal := map[string]float64{} // в базовых единицах
	for _, e := range evs {
		switch e.Kind {
		case generator.KindDelivery:
			for _, it := range e.Delivery.Items {
				a, err := itemAmount(it)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				bal[it.ProductID] += a // основные единицы
			}
		case generator.KindOrder:
			for _, oi := range e.Order.Items {
				for _, ri := range recipes[oi.DishID] {
					bal[ri.product] = math.Max(0, bal[ri.product]-ri.qty*float64(oi.Portions)/mainFactor(ri.product))
				}
			}
		}
	}
	want := q.Get("product")
	out := []iiko.BalanceItem{}
	for _, p := range seed.Products() {
		if want != "" && want != p.ID {
			continue
		}
		a := math.Max(0, bal[p.ID])
		out = append(out, iiko.BalanceItem{Store: StoreID, Product: p.ID, Amount: round(a, 3), Sum: round(a*unitPrice(p), 2)})
	}
	writeJSON(w, out)
}

type seedItem struct {
	product string
	qty     float64
}

func mainFactor(productID string) float64 {
	for _, p := range seed.Products() {
		if p.ID == productID {
			_, k := mainUnit(string(p.Unit))
			return k
		}
	}
	return 1
}

// stopList: GET читает стоп-лист, POST заменяет его целиком. [ПРЕДПОЛОЖЕНО]
func (s *Server) stopList(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		items := append([]iiko.StopItem{}, s.stop...)
		s.mu.Unlock()
		writeJSON(w, iiko.StopListResponse{Items: items})
	case http.MethodPost:
		var req iiko.StopListResponse
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			bad(w, "некорректный JSON: "+err.Error())
			return
		}
		known := map[string]bool{}
		for _, d := range seed.Dishes() {
			known[d.ID] = true
		}
		for _, p := range seed.Products() {
			known[p.ID] = true
		}
		for _, it := range req.Items {
			if !known[it.ProductID] {
				bad(w, "неизвестный productId "+it.ProductID)
				return
			}
		}
		s.mu.Lock()
		s.stop = append([]iiko.StopItem{}, req.Items...)
		s.mu.Unlock()
		writeJSON(w, iiko.StopListResponse{Items: req.Items})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
