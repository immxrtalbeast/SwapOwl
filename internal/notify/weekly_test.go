package notify

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// weeklyAPI — клок стенда настраивается; прогноз отдаётся, если forecast != "".
type weeklyAPI struct {
	mu       sync.Mutex
	now      string
	forecast string
}

func (f *weeklyAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	now, fc := f.now, f.forecast
	f.mu.Unlock()
	switch r.URL.Path {
	case "/api/clock":
		_, _ = w.Write([]byte(`{"now":"` + now + `","timezone":"Europe/Moscow"}`))
	case "/api/analytics/writeoffs":
		_, _ = w.Write([]byte(`{"total_minor":150000,"by_product":[
		{"product_id":"a","product_name":"Креветки","quantity":{"qty":1000,"unit":"g"},"cost_minor":90000},
		{"product_id":"b","product_name":"Лосось","quantity":{"qty":2000,"unit":"g"},"cost_minor":40000},
		{"product_id":"c","product_name":"Сливки","quantity":{"qty":500,"unit":"ml"},"cost_minor":10000},
		{"product_id":"d","product_name":"Укроп","quantity":{"qty":100,"unit":"g"},"cost_minor":10000}]}`))
	case "/api/forecast/writeoffs":
		if fc == "" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(fc))
	default:
		http.NotFound(w, r)
	}
}

type fakeOwner struct {
	mu     sync.Mutex
	texts  []string
	photos [][]byte
	caps   []string
}

func (o *fakeOwner) Send(_ context.Context, t string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.texts = append(o.texts, t)
	return nil
}

func (o *fakeOwner) SendPhoto(_ context.Context, p []byte, c string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.photos, o.caps = append(o.photos, p), append(o.caps, c)
	return nil
}

func weeklyNotifier(t *testing.T, api *weeklyAPI, o *fakeOwner) *Notifier {
	t.Helper()
	s := httptest.NewServer(api)
	t.Cleanup(s.Close)
	return &Notifier{
		API:    &APIClient{Base: s.URL, Key: "k", HC: s.Client()},
		Weekly: &WeeklyConfig{At: "09:00", Text: o, Photo: o, StandURL: "https://stand.example"},
	}
}

// 2026-10-12 — понедельник.
func TestWeeklyTickMondayOnce(t *testing.T) {
	api := &weeklyAPI{now: "2026-10-12T08:59:00+03:00", forecast: `{"total_minor":77700,"by_product":[]}`}
	o := &fakeOwner{}
	n := weeklyNotifier(t, api, o)
	ctx := context.Background()
	n.WeeklyTick(ctx)
	if len(o.photos) != 0 {
		t.Fatal("до 09:00 отчёта быть не должно")
	}
	api.mu.Lock()
	api.now = "2026-10-12T09:00:30+03:00"
	api.mu.Unlock()
	n.WeeklyTick(ctx)
	n.WeeklyTick(ctx)
	if len(o.photos) != 1 {
		t.Fatalf("ожидалась одна картинка, есть %d", len(o.photos))
	}
	if _, err := png.Decode(bytes.NewReader(o.photos[0])); err != nil {
		t.Fatalf("не PNG: %v", err)
	}
	c := o.caps[0]
	for _, want := range []string{"1 500,00 ₽", "1. Креветки — 900,00 ₽", "3. Сливки", "777,00 ₽", "https://stand.example"} {
		if !strings.Contains(c, want) {
			t.Errorf("в подписи нет %q:\n%s", want, c)
		}
	}
	if strings.Contains(c, "Укроп") {
		t.Error("только топ-3")
	}
	// не понедельник — ничего
	api.mu.Lock()
	api.now = "2026-10-13T10:00:00+03:00"
	api.mu.Unlock()
	n.WeeklyTick(ctx)
	if len(o.photos) != 1 {
		t.Fatal("во вторник отчёта быть не должно")
	}
}

func TestWeeklyNoForecastRouteSkipsLine(t *testing.T) {
	api := &weeklyAPI{now: "2026-10-12T12:00:00+03:00"}
	o := &fakeOwner{}
	n := weeklyNotifier(t, api, o)
	if err := n.SendWeekly(context.Background(), o, nil); err != nil {
		t.Fatal(err)
	}
	if len(o.texts) != 1 || strings.Contains(o.texts[0], "Под угрозой") || !strings.Contains(o.texts[0], "▇") {
		t.Fatalf("текст: %v", o.texts)
	}
}

func TestMarkWeeklyStartupSuppresses(t *testing.T) {
	api := &weeklyAPI{now: "2026-10-12T10:00:00+03:00"}
	o := &fakeOwner{}
	n := weeklyNotifier(t, api, o)
	n.MarkWeeklyStartup(context.Background())
	n.WeeklyTick(context.Background())
	if len(o.photos)+len(o.texts) != 0 {
		t.Fatal("после времени отчёта при старте отчёт не шлём")
	}
}

func TestWeeklyCommandAndPhotoUpload(t *testing.T) {
	b, tgf, _ := newBot(t)
	ctx := context.Background()
	b.HandleUpdate(ctx, msgUpdate(botChat, "/week"))
	if len(tgf.sent("sendPhoto")) != 1 {
		t.Fatalf("ожидался sendPhoto, вызовы: %v", tgf.calls)
	}
	if !strings.Contains(tgf.sent("sendPhoto")[0], "Списано за неделю") {
		t.Errorf("подпись: %s", tgf.sent("sendPhoto")[0])
	}
}

func TestRenderWeeklyPNG(t *testing.T) {
	r := &WeeklyReport{TotalMinor: 12345600, DayMinor: [7]int64{0, 100, 200, 300, 0, 50, 1000}, DayLabels: [7]int{6, 7, 8, 9, 10, 11, 12}}
	b, err := RenderWeeklyPNG(r)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil || img.Bounds().Dx() != 800 {
		t.Fatalf("png: %v", err)
	}
}
