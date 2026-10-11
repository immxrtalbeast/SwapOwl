package domain

import (
	"fmt"
	"math/big"
	"sort"
)

// Ошибки развёртывания техкарты. Проверка — через errors.Is; все оборачивают ErrValidation.
var (
	ErrTechcardCycle   = fmt.Errorf("%w: techcard cycle", ErrValidation)
	ErrTechcardUnknown = fmt.Errorf("%w: techcard references unknown prep", ErrValidation)
	ErrTechcardInvalid = fmt.Errorf("%w: techcard line is invalid", ErrValidation)
)

// maxLossBP — потеря строго меньше 100 % (в сотых долях процента).
const maxLossBP = 9999

// TechLine — строка техкарты. Qty — в базовых единицах. Для сырья без потерь (LossBP == 0) Qty —
// брутто; при LossBP > 0 Qty — нетто, а брутто = нетто / (1 − потери). Для полуфабриката (PrepID != "")
// Qty — сколько его нужно в единицах выхода, потери не допускаются.
type TechLine struct {
	ProductID string
	PrepID    string
	Qty       int64
	LossBP    int
}

// Prep — полуфабрикат: из Recipe получается Yield единиц самого полуфабриката.
type Prep struct {
	ID     string
	Yield  int64
	Recipe []TechLine
}

// Norm — развёрнутая норма сырья в базовых единицах.
type Norm struct {
	ProductID string
	Quantity  int64
}

// TechcardExpander разворачивает техкарты над фиксированным набором полуфабрикатов и запоминает
// развёртку каждого полуфабриката на единицу выхода: общие вершины графа считаются один раз, а не
// по числу путей. Не потокобезопасен.
type TechcardExpander struct {
	preps map[string]Prep
	memo  map[string]map[string]*big.Rat
	path  map[string]bool
}

// NewTechcardExpander создаёт разворачиватель над preps.
func NewTechcardExpander(preps map[string]Prep) *TechcardExpander {
	return &TechcardExpander{preps: preps, memo: map[string]map[string]*big.Rat{}, path: map[string]bool{}}
}

// ExpandTechcard — разовое развёртывание; см. TechcardExpander.Expand.
func ExpandTechcard(lines []TechLine, preps map[string]Prep) ([]Norm, error) {
	return NewTechcardExpander(preps).Expand(lines)
}

// Expand раскрывает техкарту до сырья: суммирует по продукту, считает брутто из нетто и
// потерь, пересчитывает полуфабрикаты пропорционально выходу. Арифметика точная (рациональные
// числа), округление вверх одно — на итоговой норме продукта. Цикл между полуфабрикатами —
// ErrTechcardCycle. Результат отсортирован по продукту.
func (e *TechcardExpander) Expand(lines []TechLine) ([]Norm, error) {
	acc := map[string]*big.Rat{}
	if err := e.add(lines, big.NewRat(1, 1), acc); err != nil {
		return nil, err
	}
	out := make([]Norm, 0, len(acc))
	for id, r := range acc {
		q := new(big.Int).Div(new(big.Int).Add(r.Num(), new(big.Int).Sub(r.Denom(), big.NewInt(1))), r.Denom())
		if !q.IsInt64() {
			return nil, ErrOverflow
		}
		out = append(out, Norm{ProductID: id, Quantity: q.Int64()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProductID < out[j].ProductID })
	return out, nil
}

// perUnit — сырьё на одну единицу выхода полуфабриката (с проверкой цикла).
func (e *TechcardExpander) perUnit(id string, p Prep) (map[string]*big.Rat, error) {
	if m, ok := e.memo[id]; ok {
		return m, nil
	}
	if e.path[id] {
		return nil, fmt.Errorf("%w: %q", ErrTechcardCycle, id)
	}
	if p.Yield <= 0 {
		return nil, fmt.Errorf("%w: prep %q has no yield", ErrTechcardInvalid, id)
	}
	e.path[id] = true
	defer delete(e.path, id)
	m := map[string]*big.Rat{}
	if err := e.add(p.Recipe, big.NewRat(1, p.Yield), m); err != nil {
		return nil, err
	}
	e.memo[id] = m
	return m, nil
}

func (e *TechcardExpander) add(lines []TechLine, k *big.Rat, acc map[string]*big.Rat) error {
	put := func(id string, q *big.Rat) {
		if cur, ok := acc[id]; ok {
			cur.Add(cur, q)
		} else {
			acc[id] = q
		}
	}
	for _, l := range lines {
		if l.Qty <= 0 || l.LossBP < 0 || l.LossBP > maxLossBP {
			return fmt.Errorf("%w: quantity %d, loss %d", ErrTechcardInvalid, l.Qty, l.LossBP)
		}
		if l.PrepID == "" {
			if l.ProductID == "" {
				return fmt.Errorf("%w: line without product or prep", ErrTechcardInvalid)
			}
			q := new(big.Rat).SetInt64(l.Qty)
			if l.LossBP > 0 {
				q.Mul(q, big.NewRat(10000, int64(10000-l.LossBP)))
			}
			put(l.ProductID, q.Mul(q, k))
			continue
		}
		if l.ProductID != "" || l.LossBP != 0 {
			return fmt.Errorf("%w: prep line %q takes only a quantity", ErrTechcardInvalid, l.PrepID)
		}
		p, ok := e.preps[l.PrepID]
		if !ok {
			return fmt.Errorf("%w: %q", ErrTechcardUnknown, l.PrepID)
		}
		m, err := e.perUnit(l.PrepID, p)
		if err != nil {
			return err
		}
		f := new(big.Rat).Mul(k, new(big.Rat).SetInt64(l.Qty))
		for id, r := range m {
			put(id, new(big.Rat).Mul(r, f))
		}
	}
	return nil
}
