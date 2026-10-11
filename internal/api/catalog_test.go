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
		if id == "salt" {
			m[id] = domain.UnitG
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
