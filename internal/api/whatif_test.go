package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func postWhatIf(t *testing.T, rt *Router, body string) (int, WhatIfResponse, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/what-if", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rt.Handler().ServeHTTP(rec, req)
	var out WhatIfResponse
	if rec.Code == 200 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code, out, rec.Body.String()
}

// Лосось 1400 г, 200 г/ч, 5 рабочих часов до срока → база: риск 400 г × 900 ₽/кг = 360 ₽.
func TestWhatIfThreeScenarios(t *testing.T) {
	rt := hotRouter(hotSnap(), "a")
	body := `{"horizon_days":3,"scenarios":[
		{"type":"discount","dish_id":"steak","percent":20},
		{"type":"order_less","product_id":"salmon","percent":50},
		{"type":"demand","dish_id":"steak","factor":0.5}]}`
	code, out, raw := postWhatIf(t, rt, body)
	if code != 200 || out.Base.TotalMinor != 36000 || len(out.Scenarios) != 3 {
		t.Fatalf("%d %s", code, raw)
	}
	for i, c := range []struct {
		total int64
		pct   float64
	}{
		{9000, -75},  // спрос ×1,3 → 260 г/ч · 5 ч = 1300 г → риск 100 г
		{0, -100},    // остаток 700 г разойдётся за срок
		{81000, 125}, // спрос ×0,5 → 100 г/ч → риск 900 г
	} {
		s := out.Scenarios[i]
		if s.TotalMinor != c.total || s.DeltaMinor != c.total-36000 || s.DeltaPct != c.pct || len(s.Assumptions) == 0 {
			t.Fatalf("scenario %d: %+v", i, s)
		}
	}
	if !strings.Contains(out.Scenarios[0].Assumptions[0], "в 1,3 раза") {
		t.Fatalf("assumption: %v", out.Scenarios[0].Assumptions)
	}
	if p := out.Scenarios[0].Products; len(p) != 1 || p[0].ProductID != "salmon" || p[0].BaseMinor != 36000 || p[0].DeltaMinor != -27000 {
		t.Fatalf("products: %+v", p)
	}
}

func TestWhatIfEmptyScenarioEqualsBase(t *testing.T) {
	rt := hotRouter(hotSnap(), "a")
	_, base, _ := postWhatIf(t, rt, `{"horizon_days":7,"scenarios":[]}`)
	_, same, _ := postWhatIf(t, rt, `{"horizon_days":7,"scenarios":[{"type":"demand","dish_id":"steak","factor":1}]}`)
	if len(base.Scenarios) != 0 || base.Base.TotalMinor != 36000 {
		t.Fatalf("%+v", base)
	}
	if s := same.Scenarios[0]; s.TotalMinor != base.Base.TotalMinor || s.DeltaMinor != 0 || s.DeltaPct != 0 {
		t.Fatalf("%+v", s)
	}
	// База совпадает с GET /api/forecast/writeoffs.
	if _, f := getForecast(t, "/api/forecast/writeoffs?hours=168"); f.TotalMinor != base.Base.TotalMinor {
		t.Fatalf("forecast %d vs base %d", f.TotalMinor, base.Base.TotalMinor)
	}
}

func TestWhatIfDiscountNeverIncreasesWriteoffs(t *testing.T) {
	rt := hotRouter(hotSnap(), "a")
	for pct := 5; pct <= 50; pct += 5 {
		body := `{"horizon_days":3,"scenarios":[{"type":"discount","dish_id":"steak","percent":` + itoa(int64(pct)) + `}]}`
		code, out, raw := postWhatIf(t, rt, body)
		if code != 200 || out.Scenarios[0].DeltaMinor > 0 {
			t.Fatalf("%d%%: %d %s", pct, code, raw)
		}
	}
}

func TestWhatIfDeterministicAndDoesNotChangeSnapshot(t *testing.T) {
	snap := hotSnap()
	before := hotSnap()
	rt := hotRouter(snap, "a")
	body := `{"horizon_days":5,"scenarios":[{"type":"order_less","product_id":"salmon","percent":30},{"type":"discount","dish_id":"steak","percent":10}]}`
	_, _, a := postWhatIf(t, rt, body)
	done := make(chan string, 4)
	for i := 0; i < 4; i++ { // параллельные вызовы
		go func() { _, _, r := postWhatIf(t, rt, body); done <- r }()
	}
	for i := 0; i < 4; i++ {
		if b := <-done; b != a {
			t.Fatalf("ответы различаются:\n%s\n%s", a, b)
		}
	}
	if !reflect.DeepEqual(snap, before) {
		t.Fatal("снимок склада изменился после расчёта")
	}
}

func TestWhatIfValidation(t *testing.T) {
	rt := hotRouter(hotSnap(), "a")
	for _, c := range []struct{ name, body, field string }{
		{"no horizon", `{"scenarios":[]}`, "horizon_days"},
		{"horizon 0", `{"horizon_days":0}`, "horizon_days"},
		{"horizon 8", `{"horizon_days":8}`, "horizon_days"},
		{"six scenarios", `{"horizon_days":1,"scenarios":[` + strings.Repeat(`{"type":"demand","dish_id":"steak","factor":1},`, 5) + `{"type":"demand","dish_id":"steak","factor":1}]}`, "scenarios"},
		{"bad type", `{"horizon_days":1,"scenarios":[{"type":"x"}]}`, "scenarios[0].type"},
		{"discount 4", `{"horizon_days":1,"scenarios":[{"type":"discount","dish_id":"steak","percent":4}]}`, "scenarios[0].percent"},
		{"discount 51", `{"horizon_days":1,"scenarios":[{"type":"discount","dish_id":"steak","percent":51}]}`, "scenarios[0].percent"},
		{"unknown dish", `{"horizon_days":1,"scenarios":[{"type":"discount","dish_id":"nope","percent":10}]}`, "scenarios[0].dish_id"},
		{"order_less 9", `{"horizon_days":1,"scenarios":[{"type":"order_less","product_id":"salmon","percent":9}]}`, "scenarios[0].percent"},
		{"unknown product", `{"horizon_days":1,"scenarios":[{"type":"order_less","product_id":"nope","percent":20}]}`, "scenarios[0].product_id"},
		{"factor 2.1", `{"horizon_days":1,"scenarios":[{"type":"demand","dish_id":"steak","factor":2.1}]}`, "scenarios[0].factor"},
		{"missing factor", `{"horizon_days":1,"scenarios":[{"type":"demand","dish_id":"steak"}]}`, "scenarios[0].factor"},
	} {
		code, _, raw := postWhatIf(t, rt, c.body)
		if code != http.StatusBadRequest || !strings.Contains(raw, `"validation_error"`) || !strings.Contains(raw, `"field":"`+c.field+`"`) {
			t.Errorf("%s: %d %s", c.name, code, raw)
		}
	}
	if code, _, _ := postWhatIf(t, rt, `{"horizon_days":1,"bogus":1}`); code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", code)
	}
}
