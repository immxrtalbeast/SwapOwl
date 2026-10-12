package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sort"
	"strings"
	"time"
)

// PhotoSender шлёт картинку с подписью.
type PhotoSender interface {
	SendPhoto(ctx context.Context, png []byte, caption string) error
}

// WeeklyReport — данные недельного отчёта собственнику.
type WeeklyReport struct {
	From, To   time.Time
	TotalMinor int64
	DayMinor   [7]int64 // по дням, от старого к новому; последний — день To
	DayLabels  [7]int   // число месяца для подписи столбика
	Top        []MoneyRow
	Forecast   *MoneyReport // nil — маршрута прогноза нет
}

// WeeklyConfig — настройки отчёта собственнику.
type WeeklyConfig struct {
	At       string      // "HH:MM" по понедельникам, время ресторана
	Text     Sender      // чат собственника
	Photo    PhotoSender // чат собственника; необязательно
	StandURL string      // ссылка на стенд
}

// BuildWeekly собирает данные за 7 суток, последние — по текущий момент времени стенда.
func (n *Notifier) BuildWeekly(ctx context.Context) (*WeeklyReport, error) {
	clk, err := n.API.Clock(ctx)
	if err != nil {
		return nil, err
	}
	loc := n.loc(ctx)
	now := clk.Now.In(loc)
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	r := &WeeklyReport{To: now, From: day0.AddDate(0, 0, -6)}
	total, err := n.API.Writeoffs(ctx, r.From, now)
	if err != nil {
		return nil, err
	}
	r.TotalMinor = total.TotalMinor
	r.Top = append(r.Top, total.ByProduct...)
	sort.SliceStable(r.Top, func(i, j int) bool { return r.Top[i].CostMinor > r.Top[j].CostMinor })
	if len(r.Top) > 3 {
		r.Top = r.Top[:3]
	}
	for i := 0; i < 7; i++ {
		from := day0.AddDate(0, 0, i-6)
		to := from.AddDate(0, 0, 1)
		if i == 6 || to.After(now) {
			to = now
		}
		r.DayLabels[i] = from.Day()
		if !to.After(from) {
			continue
		}
		d, err := n.API.Writeoffs(ctx, from, to)
		if err != nil {
			return nil, err
		}
		r.DayMinor[i] = d.TotalMinor
	}
	if f, err := n.API.ForecastWriteoffs(ctx); err == nil {
		r.Forecast = &f
	} else {
		var ae *APIError
		if !errors.As(err, &ae) || ae.Status != 404 {
			n.logf("прогноз списаний: %v", err)
		}
	}
	return r, nil
}

// FormatWeekly — текст недельного отчёта.
func FormatWeekly(r *WeeklyReport, standURL string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📈 Неделя %s — %s\n", r.From.Format("02.01"), r.To.Format("02.01"))
	fmt.Fprintf(&b, "Списано за неделю: %s\n", Rub(r.TotalMinor))
	if len(r.Top) > 0 {
		b.WriteString("Топ-3 по потерям:\n")
		for i, p := range r.Top {
			fmt.Fprintf(&b, "%d. %s — %s (%s)\n", i+1, p.ProductName, Rub(p.CostMinor), FormatQty(p.Quantity))
		}
	}
	b.WriteString("По дням:\n")
	var max int64
	for _, v := range r.DayMinor {
		if v > max {
			max = v
		}
	}
	for i, v := range r.DayMinor {
		n := 0
		if max > 0 {
			n = int((v*8 + max - 1) / max)
		}
		fmt.Fprintf(&b, "%02d %s %s\n", r.DayLabels[i], strings.Repeat("▇", n), Rub(v))
	}
	if r.Forecast != nil {
		fmt.Fprintf(&b, "⏳ Под угрозой на следующей неделе: %s\n", Rub(r.Forecast.TotalMinor))
	}
	if standURL != "" {
		b.WriteString("Стенд: " + standURL)
	}
	return strings.TrimRight(b.String(), "\n")
}

// SendWeekly собирает и отправляет отчёт: картинка с подписью, при сбое картинки — текст.
func (n *Notifier) SendWeekly(ctx context.Context, text Sender, photo PhotoSender) error {
	r, err := n.BuildWeekly(ctx)
	if err != nil {
		return err
	}
	var stand string
	if n.Weekly != nil {
		stand = n.Weekly.StandURL
	}
	caption := FormatWeekly(r, stand)
	if photo != nil {
		img, err := RenderWeeklyPNG(r)
		if err == nil {
			// подпись к фото ограничена 1024 символами: длинный текст идёт отдельным сообщением
			if len([]rune(caption)) <= 1024 {
				if err = photo.SendPhoto(ctx, img, caption); err == nil {
					return nil
				}
			} else if err = photo.SendPhoto(ctx, img, fmt.Sprintf("📈 Списано за неделю: %s", Rub(r.TotalMinor))); err == nil {
				return text.Send(ctx, caption)
			}
		}
		n.logf("картинка недельного отчёта: %v", err)
	}
	return text.Send(ctx, caption)
}

// WeeklyTick шлёт недельный отчёт собственнику по понедельникам в Weekly.At (время ресторана).
func (n *Notifier) WeeklyTick(ctx context.Context) {
	if n.Weekly == nil || n.Weekly.Text == nil {
		return
	}
	c, err := n.API.Clock(ctx)
	if err != nil {
		return
	}
	now := c.Now.In(n.loc(ctx))
	hh, mm, ok := parseHM(n.Weekly.At)
	if !ok || now.Weekday() != time.Monday {
		return
	}
	date := now.Format("2006-01-02")
	due := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())
	n.mu.Lock()
	skip := n.weeklyDate == date || now.Before(due)
	n.mu.Unlock()
	if skip {
		return
	}
	if err := n.SendWeekly(ctx, n.Weekly.Text, n.Weekly.Photo); err != nil {
		n.logf("недельный отчёт: %v", err)
		return
	}
	n.mu.Lock()
	n.weeklyDate = date
	n.mu.Unlock()
}

// MarkWeeklyStartup: если процесс стартовал после времени отчёта, за сегодня его не шлём.
func (n *Notifier) MarkWeeklyStartup(ctx context.Context) {
	if n.Weekly == nil {
		return
	}
	c, err := n.API.Clock(ctx)
	if err != nil {
		return
	}
	now := c.Now.In(n.loc(ctx))
	if hh, mm, ok := parseHM(n.Weekly.At); ok &&
		!now.Before(time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())) {
		n.mu.Lock()
		n.weeklyDate = now.Format("2006-01-02")
		n.mu.Unlock()
	}
}

// ForecastWriteoffs — GET /api/forecast/writeoffs: что окажется под угрозой на следующей неделе
// (ожидаемый формат — как у отчётов о потерях). 404 — маршрута ещё нет.
func (c *APIClient) ForecastWriteoffs(ctx context.Context) (r MoneyReport, err error) {
	err = c.get(ctx, "/api/forecast/writeoffs", nil, &r)
	return
}

// --- картинка ---

var (
	colBG    = color.RGBA{0x14, 0x1b, 0x2d, 0xff}
	colCard  = color.RGBA{0x1f, 0x29, 0x40, 0xff}
	colText  = color.RGBA{0xf5, 0xf1, 0xe8, 0xff}
	colAccnt = color.RGBA{0xf2, 0xa5, 0x41, 0xff}
	colDim   = color.RGBA{0x8a, 0x95, 0xb0, 0xff}
)

// RenderWeeklyPNG рисует сводку: крупная сумма в рублях и столбики по дням. Только стандартная библиотека.
func RenderWeeklyPNG(r *WeeklyReport) ([]byte, error) {
	const w, h = 800, 450
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill(img, 0, 0, w, h, colBG)
	fill(img, 24, 24, w-48, h-48, colCard)

	whole := r.TotalMinor / 100
	drawText(img, 56, 56, fmt.Sprintf("%d", whole), 10, colText)
	drawRuble(img, 56+textWidth(fmt.Sprintf("%d", whole), 10)+16, 56, 10, colAccnt)

	// столбики
	var max int64 = 1
	for _, v := range r.DayMinor {
		if v > max {
			max = v
		}
	}
	const baseY, maxH, bw, gap, x0 = 400, 200, 70, 24, 56
	for i, v := range r.DayMinor {
		bh := int(v * maxH / max)
		if v > 0 && bh < 4 {
			bh = 4
		}
		x := x0 + i*(bw+gap)
		fill(img, x, baseY-bh, bw, bh, colAccnt)
		fill(img, x, baseY, bw, 3, colDim)
		lbl := fmt.Sprintf("%d", r.DayLabels[i])
		drawText(img, x+(bw-textWidth(lbl, 3))/2, baseY+10, lbl, 3, colDim)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fill(img *image.RGBA, x, y, w, h int, c color.Color) {
	b := img.Bounds()
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			if image.Pt(xx, yy).In(b) {
				img.Set(xx, yy, c)
			}
		}
	}
}

// шрифт 3×5 для цифр
var digits = [10][5]string{
	{"###", "# #", "# #", "# #", "###"},
	{" ##", "  #", "  #", "  #", "  #"},
	{"###", "  #", "###", "#  ", "###"},
	{"###", "  #", "###", "  #", "###"},
	{"# #", "# #", "###", "  #", "  #"},
	{"###", "#  ", "###", "  #", "###"},
	{"###", "#  ", "###", "# #", "###"},
	{"###", "  #", "  #", "  #", "  #"},
	{"###", "# #", "###", "# #", "###"},
	{"###", "# #", "###", "  #", "###"},
}

func textWidth(s string, scale int) int { return len(s) * 4 * scale }

func drawText(img *image.RGBA, x, y int, s string, scale int, c color.Color) {
	for i, ch := range s {
		if ch < '0' || ch > '9' {
			continue
		}
		g := digits[ch-'0']
		for row, line := range g {
			for col, p := range line {
				if p == '#' {
					fill(img, x+i*4*scale+col*scale, y+row*scale, scale, scale, c)
				}
			}
		}
	}
}

// знак ₽ в сетке 3×5 (P с чертой)
func drawRuble(img *image.RGBA, x, y, scale int, c color.Color) {
	g := [5]string{"## ", "# #", "## ", "###", "#  "}
	for row, line := range g {
		for col, p := range line {
			if p == '#' {
				fill(img, x+col*scale, y+row*scale, scale, scale, c)
			}
		}
	}
}
