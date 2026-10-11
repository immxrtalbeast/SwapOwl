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

// ExpandTechcard раскрывает техкарту до сырья: суммирует по продукту, считает брутто из нетто и
// потерь, пересчитывает полуфабрикаты пропорционально выходу. Арифметика точная (рациональные
// числа), округление вверх одно — на итоговой норме продукта. Цикл между полуфабрикатами —
// ErrTechcardCycle. Результат отсортирован по продукту.
func ExpandTechcard(lines []TechLine, preps map[string]Prep) ([]Norm, error) {
	acc := map[string]*big.Rat{}
	if err := expandLines(lines, big.NewRat(1, 1), preps, map[string]bool{}, acc); err != nil {
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

func expandLines(lines []TechLine, k *big.Rat, preps map[string]Prep, path map[string]bool, acc map[string]*big.Rat) error {
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
			q.Mul(q, k)
			if cur, ok := acc[l.ProductID]; ok {
				cur.Add(cur, q)
			} else {
				acc[l.ProductID] = q
			}
			continue
		}
		if l.ProductID != "" || l.LossBP != 0 {
			return fmt.Errorf("%w: prep line %q takes only a quantity", ErrTechcardInvalid, l.PrepID)
		}
		p, ok := preps[l.PrepID]
		if !ok {
			return fmt.Errorf("%w: %q", ErrTechcardUnknown, l.PrepID)
		}
		if path[l.PrepID] {
			return fmt.Errorf("%w: %q", ErrTechcardCycle, l.PrepID)
		}
		if p.Yield <= 0 {
			return fmt.Errorf("%w: prep %q has no yield", ErrTechcardInvalid, l.PrepID)
		}
		sub := new(big.Rat).Mul(k, big.NewRat(l.Qty, p.Yield))
		path[l.PrepID] = true
		err := expandLines(p.Recipe, sub, preps, path, acc)
		delete(path, l.PrepID)
		if err != nil {
			return err
		}
	}
	return nil
}
