package hub

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MaxTransfers — сколько подсказок о перемещении отдаёт hub.
const MaxTransfers = 5

// DefaultTransferHours — запас времени на перевозку между точками, часов (HUB_TRANSFER_HOURS).
const DefaultTransferHours = 2.0

// TransferHours разбирает HUB_TRANSFER_HOURS (число часов, допускается дробное); пусто — 2.
func TransferHours(s string) (float64, error) {
	if strings.TrimSpace(s) == "" {
		return DefaultTransferHours, nil
	}
	h, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || h < 0 || h > 72 {
		return 0, fmt.Errorf("hub: bad HUB_TRANSFER_HOURS %q", s)
	}
	return h, nil
}

// RiskBatch — партия точки, которую не успеют продать (из reasons в /api/hot-offers).
type RiskBatch struct {
	BatchID     int64     `json:"batch_id"`
	ProductID   string    `json:"product_id"`
	ProductName string    `json:"product_name"`
	AtRisk      Qty       `json:"at_risk"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Demand — темп расхода продукта и остаток в точке (из /api/supply-suggestions).
type Demand struct {
	ProductID   string `json:"product_id"`
	RatePerDay  Qty    `json:"rate_per_day"`
	UsableStock Qty    `json:"usable_stock"`
}

// Transfer — подсказка: переместить часть партии из одной точки в другую. Только подсказка: hub ничего не выполняет.
type Transfer struct {
	ProductID   string    `json:"product_id"`
	ProductName string    `json:"product_name"`
	BatchID     int64     `json:"batch_id"`
	Qty         Qty       `json:"qty"`
	FromID      string    `json:"from_id"`
	FromName    string    `json:"from_name"`
	ToID        string    `json:"to_id"`
	ToName      string    `json:"to_name"`
	ExpiresAt   time.Time `json:"expires_at"`
	// SavesMinor — копейки; null, если цена продукта не задана (HUB_PRICES).
	SavesMinor *int64 `json:"saves_minor"`
	Text       string `json:"text"`
	Invoice    string `json:"invoice"`
}

// FormatQty — «1,2 кг», «350 г», «2 л», «12 шт».
func FormatQty(q Qty) string {
	num := func(v float64) string {
		return strings.Replace(strconv.FormatFloat(v, 'f', -1, 64), ".", ",", 1)
	}
	switch q.Unit {
	case "g":
		if q.Qty >= 1000 {
			return num(float64(q.Qty)/1000) + " кг"
		}
		return num(float64(q.Qty)) + " г"
	case "ml":
		if q.Qty >= 1000 {
			return num(float64(q.Qty)/1000) + " л"
		}
		return num(float64(q.Qty)) + " мл"
	case "pcs":
		return num(float64(q.Qty)) + " шт"
	}
	return fmt.Sprintf("%d %s", q.Qty, q.Unit)
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

type candidate struct {
	t        Transfer
	atRisk   int64 // объём партии под риском в A
	capacity int64 // сколько B успеет продать сверх своего остатка
	price    int64 // копейки за 1000 базовых единиц
	hasPrice bool
	value    float64 // для сортировки: копейки, без цены — граммы со знаком «ниже любой цены»
}

// SuggestTransfers считает подсказки по снимкам доступных точек.
//
// Для каждой партии точки A под риском и каждой другой точки B: B расходует продукт быстрее A (rate_per_day из
// supply-suggestions; нет записи — расход 0); до срока партии за вычетом transferHours остаётся окно > 0;
// B успеет продать за него rate_B × окно(мин) / 1440 сверх собственного остатка usable_stock. Объём — меньшее из
// «под риском» и этой ёмкости B. Цена — из каталога точки A (/api/forecast/writeoffs), иначе из prices (HUB_PRICES). Расчёт целочисленный. Одна партия и ёмкость одной точки жадно не делятся дважды. Берутся первые MaxTransfers
// по убыванию ₽ (без цен — по убыванию количества).
func SuggestTransfers(states []PointState, transferHours float64, prices map[string]int64) []Transfer {
	type key struct {
		point, product string
	}
	rate := map[key]int64{} // базовых единиц в сутки
	stock := map[key]int64{}
	for _, s := range states {
		if s.Status != StatusOK {
			continue
		}
		for _, d := range s.Demand {
			rate[key{s.ID, d.ProductID}] = d.RatePerDay.Qty
			stock[key{s.ID, d.ProductID}] = d.UsableStock.Qty
		}
	}
	var cands []candidate
	for _, a := range states {
		if a.Status != StatusOK || a.StandTime == nil {
			continue
		}
		for _, rb := range a.RiskBatches {
			windowMin := int64(rb.ExpiresAt.Sub(*a.StandTime).Minutes() - transferHours*60)
			if windowMin <= 0 || rb.AtRisk.Qty <= 0 {
				continue
			}
			for _, b := range states {
				if b.ID == a.ID || b.Status != StatusOK {
					continue
				}
				kb := key{b.ID, rb.ProductID}
				rb2, ok := rate[kb]
				if !ok || rb2 <= rate[key{a.ID, rb.ProductID}] {
					continue
				}
				capacity := rb2*windowMin/(24*60) - stock[kb]
				q := rb.AtRisk.Qty
				if capacity < q {
					q = capacity
				}
				if q <= 0 {
					continue
				}
				cands = append(cands, candidate{t: Transfer{
					ProductID: rb.ProductID, ProductName: rb.ProductName, BatchID: rb.BatchID,
					Qty:    Qty{Qty: q, Unit: rb.AtRisk.Unit},
					FromID: a.ID, FromName: a.Name, ToID: b.ID, ToName: b.Name, ExpiresAt: rb.ExpiresAt,
				}, atRisk: rb.AtRisk.Qty, capacity: capacity})
				pr, ok := a.Prices[rb.ProductID] // цена каталога точки-источника, иначе запасная из HUB_PRICES
				if !ok {
					pr, ok = prices[rb.ProductID]
				}
				cands[len(cands)-1].price, cands[len(cands)-1].hasPrice = pr, ok
			}
		}
	}
	for i := range cands {
		if cands[i].hasPrice {
			cands[i].value = float64(cands[i].price*cands[i].t.Qty.Qty/1000) + 1e15 // с ценой всегда выше безценовых
		} else {
			cands[i].value = float64(cands[i].t.Qty.Qty)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].value != cands[j].value {
			return cands[i].value > cands[j].value
		}
		ti, tj := cands[i].t, cands[j].t
		if ti.FromID != tj.FromID {
			return ti.FromID < tj.FromID
		}
		if ti.ToID != tj.ToID {
			return ti.ToID < tj.ToID
		}
		return ti.BatchID < tj.BatchID
	})
	usedBatch := map[[2]string]int64{} // (точка-источник, партия) -> уже отдано
	usedCap := map[key]int64{}         // (точка-приёмник, продукт) -> ёмкость, уже занятая
	var out []Transfer
	for _, c := range cands {
		t := c.t
		bk := [2]string{t.FromID, fmt.Sprint(t.BatchID)}
		kb := key{t.ToID, t.ProductID}
		q := t.Qty.Qty
		if left := c.atRisk - usedBatch[bk]; left < q {
			q = left
		}
		if left := c.capacity - usedCap[kb]; left < q {
			q = left
		}
		if q <= 0 {
			continue
		}
		usedBatch[bk] += q
		usedCap[kb] += q
		t.Qty.Qty = q
		if c.hasPrice {
			v := c.price * q / 1000
			t.SavesMinor = &v
		}
		t.Text = fmt.Sprintf("%s %s: %s → %s", t.ProductName, FormatQty(t.Qty), t.FromName, t.ToName)
		if t.SavesMinor != nil {
			t.Text += fmt.Sprintf(", не спишем ≈ %s", formatRubMinor(*t.SavesMinor))
		}
		t.Invoice = invoiceText(t)
		out = append(out, t)
		if len(out) == MaxTransfers {
			break
		}
	}
	// Остаток после урезания мог изменить порядок по ₽.
	sort.SliceStable(out, func(i, j int) bool {
		vi, vj := int64(-1), int64(-1)
		if out[i].SavesMinor != nil {
			vi = *out[i].SavesMinor
		}
		if out[j].SavesMinor != nil {
			vj = *out[j].SavesMinor
		}
		return vi > vj
	})
	return out
}

func formatRubMinor(m int64) string {
	r := (m + 50) / 100
	s := strconv.FormatInt(r, 10)
	var b strings.Builder
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteString(" ")
		}
		b.WriteRune(ch)
	}
	return b.String() + " ₽"
}

func invoiceText(t Transfer) string {
	var b strings.Builder
	b.WriteString("Накладная перемещения (черновик для ручного оформления)\n")
	fmt.Fprintf(&b, "Откуда: %s\nКуда: %s\n", t.FromName, t.ToName)
	fmt.Fprintf(&b, "Товар: %s (%s)\n", t.ProductName, t.ProductID)
	fmt.Fprintf(&b, "Партия: №%d, годна до %s\n", t.BatchID, t.ExpiresAt.Format("02.01.2006 15:04"))
	fmt.Fprintf(&b, "Количество: %s (%d %s)\n", FormatQty(t.Qty), t.Qty.Qty, t.Qty.Unit)
	if t.SavesMinor != nil {
		fmt.Fprintf(&b, "Не спишем: ≈ %s\n", formatRubMinor(*t.SavesMinor))
	}
	b.WriteString("Hub только подсказывает: перемещение в API точек не выполняется.")
	return b.String()
}
