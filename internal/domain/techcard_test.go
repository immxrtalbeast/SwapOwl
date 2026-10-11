package domain

import (
	"errors"
	"reflect"
	"testing"
)

func TestExpandTechcard(t *testing.T) {
	preps := map[string]Prep{
		// 1000 г соуса из 600 г томатов и 100 г соли.
		"sauce": {ID: "sauce", Yield: 1000, Recipe: []TechLine{{ProductID: "tomato", Qty: 600}, {ProductID: "salt", Qty: 100}}},
	}
	cases := map[string]struct {
		in   []TechLine
		want []Norm
	}{
		"gross":      {[]TechLine{{ProductID: "a", Qty: 150}}, []Norm{{"a", 150}}},
		"net exact":  {[]TechLine{{ProductID: "a", Qty: 80, LossBP: 2000}}, []Norm{{"a", 100}}},
		"net ceil":   {[]TechLine{{ProductID: "a", Qty: 100, LossBP: 3000}}, []Norm{{"a", 143}}},
		"sum":        {[]TechLine{{ProductID: "a", Qty: 10}, {ProductID: "a", Qty: 5}}, []Norm{{"a", 15}}},
		"prep":       {[]TechLine{{PrepID: "sauce", Qty: 250}}, []Norm{{"salt", 25}, {"tomato", 150}}},
		"prep ceil":  {[]TechLine{{PrepID: "sauce", Qty: 3}}, []Norm{{"salt", 1}, {"tomato", 2}}},
		"prep + raw": {[]TechLine{{PrepID: "sauce", Qty: 100}, {ProductID: "salt", Qty: 5}}, []Norm{{"salt", 15}, {"tomato", 60}}},
	}
	for name, c := range cases {
		got, err := ExpandTechcard(c.in, preps)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v err %v, want %v", name, got, err, c.want)
		}
	}
}

func TestExpandTechcardNestedAndCycle(t *testing.T) {
	preps := map[string]Prep{
		"a": {ID: "a", Yield: 100, Recipe: []TechLine{{PrepID: "b", Qty: 50}}},
		"b": {ID: "b", Yield: 100, Recipe: []TechLine{{ProductID: "x", Qty: 200}}},
	}
	got, err := ExpandTechcard([]TechLine{{PrepID: "a", Qty: 100}}, preps)
	if err != nil || !reflect.DeepEqual(got, []Norm{{"x", 100}}) {
		t.Fatalf("nested: %v %v", got, err)
	}
	preps["b"] = Prep{ID: "b", Yield: 100, Recipe: []TechLine{{PrepID: "a", Qty: 1}}}
	if _, err := ExpandTechcard([]TechLine{{PrepID: "a", Qty: 1}}, preps); !errors.Is(err, ErrTechcardCycle) {
		t.Fatalf("cycle: %v", err)
	}
	// Один полуфабрикат дважды подряд — не цикл.
	ok := map[string]Prep{"s": {ID: "s", Yield: 10, Recipe: []TechLine{{ProductID: "x", Qty: 10}}}}
	if got, err := ExpandTechcard([]TechLine{{PrepID: "s", Qty: 5}, {PrepID: "s", Qty: 5}}, ok); err != nil || got[0].Quantity != 10 {
		t.Fatalf("repeat: %v %v", got, err)
	}
}

func TestExpandTechcardInvalid(t *testing.T) {
	for name, l := range map[string]TechLine{
		"zero":     {ProductID: "a"},
		"loss 100": {ProductID: "a", Qty: 1, LossBP: 10000},
		"no ref":   {Qty: 1},
		"mixed":    {ProductID: "a", PrepID: "p", Qty: 1},
		"unknown":  {PrepID: "nope", Qty: 1},
	} {
		if _, err := ExpandTechcard([]TechLine{l}, nil); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
