package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"umirhack/internal/auth"
	"umirhack/internal/domain"
	"umirhack/internal/store"
)

type fakeCatalog struct {
	in    store.CatalogInput
	dry   bool
	calls int
	err   error
}

func (f *fakeCatalog) ProductUnits(_ context.Context, ids []string) (map[string]domain.Unit, error) {
	m := map[string]domain.Unit{}
	for _, id := range ids {
		switch id {
		case "salt":
			m[id] = domain.UnitG
		case "egg":
			m[id] = domain.UnitPcs
		}
	}
	return m, nil
}

func (f *fakeCatalog) ProductPacking(_ context.Context, ids []string) (map[string]store.Packing, error) {
	m := map[string]store.Packing{}
	for _, id := range ids {
		if id == "egg" {
			m[id] = store.Packing{Unit: domain.UnitPcs, PieceWeight: 55, PieceWeightUnit: domain.UnitG, PackSize: 1}
		}
	}
	return m, nil
}

func (f *fakeCatalog) ImportCatalog(_ context.Context, in store.CatalogInput, dry bool) (store.CatalogDiff, error) {
	f.in, f.dry = in, dry
	f.calls++
	return store.CatalogDiff{DryRun: dry, Products: []store.CatalogChange{}, Dishes: []store.CatalogChange{}}, f.err
}

const catalogBody = `{"products":[{"id":"rice","name":"Рис","unit":"g","price_minor_per_1000":15000,"lead_days":2,"pack_size":1000,"shelf_life_hours":720}],
"dishes":[{"id":"pilaf","name":"Плов","price_minor":45000,"prior_daily":12.5,"recipe":[{"product_id":"rice","quantity":"0.15","unit":"kg"},{"product_id":"salt","quantity":"5"}]}]}`

// putCatalog отправляет запрос от admin: без входа импортировать нельзя (см. TestPutCatalogAccess).
func putCatalog(t *testing.T, fc *fakeCatalog, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	e := newAuthEnv(t, true)
	e.rt.deps.Catalog = fc
	e.rt.catalogRoutes()
	e.addUser(t, "root", "password1", auth.Admin)
	return e.do(req{method: http.MethodPut, path: path, body: body, cookie: e.login(t, "root", "password1")})
}

func TestPutCatalogNormalizesAndPassesDryRun(t *testing.T) {
	fc := &fakeCatalog{}
	rec := putCatalog(t, fc, "/api/catalog?dry_run=true", catalogBody)
	if rec.Code != 200 || !fc.dry || fc.calls != 1 {
		t.Fatalf("code %d body %s dry=%v", rec.Code, rec.Body, fc.dry)
	}
	p, d := fc.in.Products[0], fc.in.Dishes[0]
	if p.PriceMinorPer1000 != 15000 || p.ShelfLifeHours == nil || *p.ShelfLifeHours != 720 || d.PriorDaily != 12.5 {
		t.Fatalf("parsed: %+v %+v", p, d)
	}
	// 0.15 kg → 150 g (единица из тела), соль — единица продукта из БД.
	if d.Recipe[0].Quantity != 150 || d.Recipe[1].Quantity != 5 {
		t.Fatalf("recipe: %+v", d.Recipe)
	}
	if rec := putCatalog(t, fc, "/api/catalog", catalogBody); rec.Code != 200 || fc.dry {
		t.Fatalf("write: %d dry=%v", rec.Code, fc.dry)
	}
}

func TestPutCatalogValidation(t *testing.T) {
	prod := func(extra string) string {
		return `{"products":[{"id":"rice","name":"Рис","unit":"g","price_minor_per_1000":1,"lead_days":1,"pack_size":1` + extra + `}]}`
	}
	dish := func(recipe string) string {
		return `{"products":[{"id":"rice","name":"Рис","unit":"g","price_minor_per_1000":1,"lead_days":1,"pack_size":1}],
"dishes":[{"id":"d","name":"D","price_minor":1,"prior_daily":1,"recipe":` + recipe + `}]}`
	}
	cases := map[string]struct{ path, body, code string }{
		"empty":           {"/api/catalog", `{}`, CodeValidation},
		"bad dry_run":     {"/api/catalog?dry_run=1", catalogBody, CodeValidation},
		"unit kg":         {"/api/catalog", strings.Replace(prod(""), `"unit":"g"`, `"unit":"kg"`, 1), CodeValidation},
		"missing price":   {"/api/catalog", `{"products":[{"id":"rice","name":"Рис","unit":"g","lead_days":1,"pack_size":1}]}`, CodeValidation},
		"shelf range":     {"/api/catalog", prod(`,"shelf_life_hours":0`), CodeValidation},
		"duplicate":       {"/api/catalog", `{"products":[` + strings.Repeat(`{"id":"r","name":"R","unit":"g","price_minor_per_1000":1,"lead_days":1,"pack_size":1},`, 2)[:len(`{"id":"r","name":"R","unit":"g","price_minor_per_1000":1,"lead_days":1,"pack_size":1},`)*2-1] + `]}`, CodeValidation},
		"empty recipe":    {"/api/catalog", dish(`[]`), CodeValidation},
		"unknown product": {"/api/catalog", dish(`[{"product_id":"ghost","quantity":"1"}]`), CodeUnknownProduct},
		"numeric qty":     {"/api/catalog", dish(`[{"product_id":"rice","quantity":5}]`), CodeValidation},
		"zero qty":        {"/api/catalog", dish(`[{"product_id":"rice","quantity":"0"}]`), CodeValidation},
		"dup recipe item": {"/api/catalog", dish(`[{"product_id":"rice","quantity":"1"},{"product_id":"rice","quantity":"2"}]`), CodeValidation},
		"unknown field":   {"/api/catalog", prod(`,"colour":"red"`), CodeValidation},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fc := &fakeCatalog{}
			rec := putCatalog(t, fc, c.path, c.body)
			if rec.Code != 400 || decodeErr(t, rec).Error.Code != c.code || fc.calls != 0 {
				t.Fatalf("code %d body %s calls %d", rec.Code, rec.Body, fc.calls)
			}
		})
	}
}

func TestPutCatalogErrorMapping(t *testing.T) {
	rec := putCatalog(t, &fakeCatalog{err: &store.UnitChangeError{ProductID: "rice", Have: domain.UnitG, Got: domain.UnitMl}}, "/api/catalog", catalogBody)
	if rec.Code != 409 || decodeErr(t, rec).Error.Code != CodeUnitConflict {
		t.Fatalf("unit change: %d %s", rec.Code, rec.Body)
	}
	rec = putCatalog(t, &fakeCatalog{err: &store.CatalogInvalidError{Problems: []string{"x"}}}, "/api/catalog", catalogBody)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), `"problems"`) {
		t.Fatalf("invalid: %d %s", rec.Code, rec.Body)
	}
}

func TestPutCatalogAccess(t *testing.T) {
	e := newAuthEnv(t, true)
	fc := &fakeCatalog{}
	e.rt.deps.Catalog = fc
	e.rt.catalogRoutes()
	e.addUser(t, "root", "password1", auth.Admin)
	e.addUser(t, "op", "password2", auth.Operator)
	e.addUser(t, "view", "password3", auth.Viewer)
	adm := e.login(t, "root", "password1")

	var key struct {
		Key string `json:"key"`
	}
	rec := e.do(req{method: "POST", path: "/api/api-keys", cookie: adm, body: `{"name":"iiko","role":"operator"}`})
	_ = json.Unmarshal(rec.Body.Bytes(), &key)
	vk := e.do(req{method: "POST", path: "/api/api-keys", cookie: adm, body: `{"name":"ro","role":"viewer"}`})
	var viewerKey struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal(vk.Body.Bytes(), &viewerKey)

	cases := []struct {
		name string
		r    req
		want int
	}{
		{"anonymous", req{}, 401},
		{"admin", req{cookie: adm}, 200},
		{"operator key", req{bearer: key.Key}, 200},
		{"operator user", req{cookie: e.login(t, "op", "password2")}, 403},
		{"viewer user", req{cookie: e.login(t, "view", "password3")}, 403},
		{"viewer key", req{bearer: viewerKey.Key}, 403},
	}
	for _, c := range cases {
		c.r.method, c.r.path, c.r.body = "PUT", "/api/catalog", catalogBody
		if rec := e.do(c.r); rec.Code != c.want {
			t.Errorf("%s: got %d want %d: %s", c.name, rec.Code, c.want, rec.Body)
		}
	}

	// Стенд без входа (AUTH_REQUIRED=false): анонимный operator импортировать не может, admin и ключ — могут.
	open := newAuthEnv(t, false)
	open.rt.deps.Catalog = fc
	open.rt.catalogRoutes()
	open.addUser(t, "root", "password1", auth.Admin)
	oadm := open.login(t, "root", "password1")
	rec = open.do(req{method: "POST", path: "/api/api-keys", cookie: oadm, body: `{"name":"iiko","role":"operator"}`})
	_ = json.Unmarshal(rec.Body.Bytes(), &key)
	fc.calls = 0
	cases = []struct {
		name string
		r    req
		want int
	}{
		{"open stand anonymous", req{}, 403},
		{"open stand admin", req{cookie: oadm}, 200},
		{"open stand operator key", req{bearer: key.Key}, 200},
	}
	e = open

	for _, c := range cases {
		c.r.method, c.r.path, c.r.body = "PUT", "/api/catalog", catalogBody
		if rec := e.do(c.r); rec.Code != c.want {
			t.Errorf("%s: got %d want %d: %s", c.name, rec.Code, c.want, rec.Body)
		}
	}
}

const techcardBody = `{"products":[
{"id":"tomato","name":"Томат","unit":"g","price_minor_per_1000":1,"lead_days":1,"pack_size":1000},
{"id":"salt","name":"Соль","unit":"g","price_minor_per_1000":1,"lead_days":1,"pack_size":1000}],
"preps":[{"id":"sauce","yield":"1","unit":"g","yield_unit":"kg","recipe":[{"product_id":"tomato","net":"480","loss_pct":20},{"product_id":"salt","gross":"100"}]}],
"dishes":[{"id":"d","name":"Блюдо","price_minor":1,"prior_daily":1,"recipe":[{"prep_id":"sauce","quantity":"250"},{"product_id":"salt","quantity":"5"}]}]}`

func TestPutCatalogExpandsTechcard(t *testing.T) {
	fc := &fakeCatalog{}
	rec := putCatalog(t, fc, "/api/catalog", techcardBody)
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body)
	}
	// соус 1 кг = 600 г томата (480/0.8) + 100 г соли; 250 г соуса = 150 + 25; плюс 5 г соли.
	got := fc.in.Dishes[0].Recipe
	want := []store.CatalogRecipeItem{{ProductID: "salt", Quantity: 30}, {ProductID: "tomato", Quantity: 150}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("recipe %+v", got)
	}
}

func TestPutCatalogTechcardValidation(t *testing.T) {
	rep := func(old, new string) string { return strings.Replace(techcardBody, old, new, 1) }
	cases := map[string]struct{ body, code string }{
		"cycle":        {rep(`"recipe":[{"product_id":"tomato","net"`, `"recipe":[{"prep_id":"sauce","quantity":"1"},{"product_id":"tomato","net"`), CodeValidation},
		"unknown prep": {rep(`"prep_id":"sauce"`, `"prep_id":"nope"`), CodeValidation},
		"both":         {rep(`"gross":"100"`, `"gross":"100","net":"1"`), CodeValidation},
		"loss w/o net": {rep(`"gross":"100"`, `"gross":"100","loss_pct":5`), CodeValidation},
		"loss 100":     {rep(`"loss_pct":20`, `"loss_pct":100`), CodeValidation},
		"prep gross":   {rep(`"prep_id":"sauce","quantity":"250"`, `"prep_id":"sauce","gross":"250"`), CodeValidation},
		"no yield":     {rep(`"yield":"1"`, `"yield":"0"`), CodeValidation},
		"unknown prod": {rep(`"product_id":"salt","quantity":"5"`, `"product_id":"zzz","quantity":"5"`), CodeUnknownProduct},
	}
	for name, c := range cases {
		fc := &fakeCatalog{}
		rec := putCatalog(t, fc, "/api/catalog", c.body)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), c.code) || fc.calls != 0 {
			t.Errorf("%s: %d %s calls=%d", name, rec.Code, rec.Body, fc.calls)
		}
	}
}

const eggProduct = `{"id":"egg","name":"Яйцо","unit":"pcs","price_minor_per_1000":1,"lead_days":1,"pack_size":30,"piece_weight":55`

func eggDish(recipe string) string {
	return `{"products":[` + eggProduct + `}],"dishes":[{"id":"omelette","name":"Омлет","price_minor":1,"prior_daily":1,"recipe":` + recipe + `}]}`
}

// Яйцо 55 г: норма 50 г на порцию — 1 шт на порцию, 3 порции — 3 шт; 30 г и 60 г — тоже по штуке и две.
func TestPutCatalogPieceWeightNorms(t *testing.T) {
	cases := map[string]struct {
		line string
		want int64
	}{
		"50 g":           {`{"product_id":"egg","gross":"50","unit":"g"}`, 1},
		"exact 55 g":     {`{"product_id":"egg","gross":"55","unit":"g"}`, 1},
		"56 g rounds up": {`{"product_id":"egg","gross":"56","unit":"g"}`, 2},
		"0.11 kg":        {`{"product_id":"egg","gross":"0.11","unit":"kg"}`, 2},
		"net with loss":  {`{"product_id":"egg","net":"100","unit":"g","loss_pct":20}`, 3}, // брутто 125 г
		"pcs unchanged":  {`{"product_id":"egg","quantity":"2"}`, 2},
		"explicit pcs":   {`{"product_id":"egg","quantity":"2","unit":"pcs"}`, 2},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fc := &fakeCatalog{}
			rec := putCatalog(t, fc, "/api/catalog", eggDish(`[`+c.line+`]`))
			if rec.Code != 200 {
				t.Fatalf("code %d body %s", rec.Code, rec.Body)
			}
			if got := fc.in.Dishes[0].Recipe[0].Quantity; got != c.want {
				t.Fatalf("quantity %d, want %d", got, c.want)
			}
			p := fc.in.Products[0]
			if p.PieceWeight == nil || *p.PieceWeight != 55 || p.PieceWeightUnit != domain.UnitG {
				t.Fatalf("product: %+v", p)
			}
		})
	}
}

// Вес штуки уже в БД (продукта нет в теле) — норма в граммах тоже пересчитывается.
func TestPutCatalogPieceWeightFromStoredProduct(t *testing.T) {
	fc := &fakeCatalog{}
	body := `{"products":[{"id":"rice","name":"Рис","unit":"g","price_minor_per_1000":1,"lead_days":1,"pack_size":1}],
"dishes":[{"id":"d","name":"D","price_minor":1,"prior_daily":1,"recipe":[{"product_id":"egg","gross":"100","unit":"g"}]}]}`
	if rec := putCatalog(t, fc, "/api/catalog", body); rec.Code != 200 || fc.in.Dishes[0].Recipe[0].Quantity != 2 {
		t.Fatalf("code %d body %s in %+v", rec.Code, rec.Body, fc.in.Dishes)
	}
}

func TestPutCatalogPieceWeightValidation(t *testing.T) {
	noWeight := `{"products":[{"id":"egg","name":"Яйцо","unit":"pcs","price_minor_per_1000":1,"lead_days":1,"pack_size":1}],
"dishes":[{"id":"d","name":"D","price_minor":1,"prior_daily":1,"recipe":[{"product_id":"egg","gross":"50","unit":"g"}]}]}`
	cases := map[string]string{
		"grams without weight": noWeight,
		"ml for gram weight":   eggDish(`[{"product_id":"egg","gross":"50","unit":"ml"}]`),
		"weight on grams":      `{"products":[{"id":"r","name":"R","unit":"g","price_minor_per_1000":1,"lead_days":1,"pack_size":1,"piece_weight":5}]}`,
		"zero weight":          `{"products":[{"id":"e","name":"E","unit":"pcs","price_minor_per_1000":1,"lead_days":1,"pack_size":1,"piece_weight":0}]}`,
		"bad weight unit":      `{"products":[{"id":"e","name":"E","unit":"pcs","price_minor_per_1000":1,"lead_days":1,"pack_size":1,"piece_weight":5,"piece_weight_unit":"kg"}]}`,
		"unit without weight":  `{"products":[{"id":"e","name":"E","unit":"pcs","price_minor_per_1000":1,"lead_days":1,"pack_size":1,"piece_weight_unit":"g"}]}`,
		"empty pack_unit":      `{"products":[{"id":"e","name":"E","unit":"pcs","price_minor_per_1000":1,"lead_days":1,"pack_size":1,"pack_unit":""}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			fc := &fakeCatalog{}
			rec := putCatalog(t, fc, "/api/catalog", body)
			if rec.Code != 400 || decodeErr(t, rec).Error.Code != CodeValidation || fc.calls != 0 {
				t.Fatalf("code %d body %s calls %d", rec.Code, rec.Body, fc.calls)
			}
		})
	}
}

// Продукт без новых полей импортируется как раньше: поля nil, нормы не меняются.
func TestPutCatalogWithoutPieceFieldsUnchanged(t *testing.T) {
	fc := &fakeCatalog{}
	if rec := putCatalog(t, fc, "/api/catalog", catalogBody); rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body)
	}
	if p := fc.in.Products[0]; p.PieceWeight != nil || p.PackUnit != nil || p.PieceWeightUnit != "" {
		t.Fatalf("product: %+v", p)
	}
}

func TestPiecesFromWeight(t *testing.T) {
	pw := pieceWeight{55, domain.UnitG}
	for _, c := range []struct {
		q, u   string
		loss   int
		want   int64
		wantOK bool
	}{
		{"150", "g", 0, 3, true}, {"1", "g", 0, 1, true}, {"55", "g", 0, 1, true}, {"110.5", "g", 0, 0, false},
		{"1", "kg", 0, 19, true}, {"100", "g", 2000, 3, true}, {"1", "ml", 0, 0, false},
	} {
		got, err := piecesFromWeight(c.q, c.u, pw, c.loss)
		if (err == nil) != c.wantOK || int64(got) != c.want {
			t.Errorf("%s%s loss %d: got %d err %v, want %d ok=%v", c.q, c.u, c.loss, got, err, c.want, c.wantOK)
		}
	}
}
