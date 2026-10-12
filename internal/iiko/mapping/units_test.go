package mapping

import (
	"testing"

	"umirhack/internal/iiko"
)

func TestPieceWeightOf(t *testing.T) {
	pcs, _ := UnitOf("шт")
	kg, _ := UnitOf("кг")
	if g, ok := PieceWeightOf(pcs, 0.055); !ok || g != 55 {
		t.Errorf("0.055 kg: %d %v", g, ok)
	}
	for _, w := range []float64{0, -1, 0.0001} {
		if _, ok := PieceWeightOf(pcs, w); ok {
			t.Errorf("weight %v must be ignored", w)
		}
	}
	if _, ok := PieceWeightOf(kg, 0.055); ok {
		t.Error("weight on a kg product must be ignored")
	}
}

func TestPackOf(t *testing.T) {
	l, _ := UnitOf("л")
	pcs, _ := UnitOf("шт")
	cs := []iiko.Container{{Name: "старая", Count: 1, Deleted: true}, {Name: " ", Count: 1}, {Name: "уп", Count: 0.5}}
	if size, label, ok := PackOf(l, cs); !ok || size != 500 || label != "уп" {
		t.Errorf("0.5 l: %d %q %v", size, label, ok)
	}
	if _, _, ok := PackOf(pcs, []iiko.Container{{Name: "уп", Count: 2.5}}); ok {
		t.Error("fractional pieces per pack must be ignored")
	}
	if size, _, ok := PackOf(pcs, []iiko.Container{{Name: "лоток", Count: 30}}); !ok || size != 30 {
		t.Errorf("30 pcs: %d %v", size, ok)
	}
	if _, _, ok := PackOf(l, nil); ok {
		t.Error("no containers")
	}
}

func TestBuildCatalogFillsPiecesAndPacks(t *testing.T) {
	products := []iiko.Product{
		{ID: "egg", Name: "Яйцо", Type: "GOODS", MainUnit: "шт", UnitWeight: 0.055, Containers: []iiko.Container{{Name: "лоток", Count: 30}}},
		{ID: "cream", Name: "Сливки", Type: "GOODS", MainUnit: "л", Containers: []iiko.Container{{Name: "пачка", Count: 0.5}}},
		{ID: "salt", Name: "Соль", Type: "GOODS", MainUnit: "кг"},
	}
	cat, _, _ := BuildCatalog(products, nil, nil, Defaults{PriceMinorPer1000: 1, LeadDays: 1, PackSize: 1000, PriorDaily: 1})
	byID := map[string]CatalogProduct{}
	for _, p := range cat.Products {
		byID[p.ID] = p
	}
	if e := byID["egg"]; e.PieceWeight != 55 || e.PieceWeightUnit != "g" || e.PackSize != 30 || e.PackUnit != "лоток" {
		t.Errorf("egg: %+v", e)
	}
	if c := byID["cream"]; c.PackSize != 500 || c.PackUnit != "пачка" || c.PieceWeight != 0 {
		t.Errorf("cream: %+v", c)
	}
	if s := byID["salt"]; s.PackSize != 1000 || s.PackUnit != "" || s.PieceWeight != 0 {
		t.Errorf("salt: %+v", s)
	}
}
