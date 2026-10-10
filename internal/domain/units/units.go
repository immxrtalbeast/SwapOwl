package units

import (
	"errors"
	"math"
	"strconv"
)

// Unit — базовая единица продукта.
type Unit string

const (
	Gram  Unit = "g"
	Milli Unit = "ml"
	Piece Unit = "pcs"
)

// Qty — количество в базовых единицах.
type Qty int64

// Limit — предел количества в базовых единицах.
const Limit Qty = 1_000_000_000_000

var (
	ErrFormat        = errors.New("units: invalid quantity format")
	ErrUnknownUnit   = errors.New("units: unknown unit")
	ErrDimension     = errors.New("units: dimension mismatch")
	ErrFraction      = errors.New("units: fraction finer than base unit")
	ErrFractionalPcs = errors.New("units: fractional pieces")
	ErrZero          = errors.New("units: zero quantity")
	ErrLimit         = errors.New("units: quantity limit exceeded")
	ErrOverflow      = errors.New("units: int64 overflow")
	ErrNegative      = errors.New("units: negative quantity")
)

type unitInfo struct {
	base Unit
	mult int64 // base units per one input unit
	zero int   // decimal zeros in mult
}

var table = map[string]unitInfo{
	"g":   {Gram, 1, 0},
	"kg":  {Gram, 1000, 3},
	"ml":  {Milli, 1, 0},
	"l":   {Milli, 1000, 3},
	"pcs": {Piece, 1, 0},
}

// Parse разбирает строку вида ^[0-9]+(\.[0-9]+)?$ в базовые единицы base.
func Parse(quantity string, unit string, base Unit) (Qty, error) {
	// формат
	n := len(quantity)
	dot := -1
	for i := 0; i < n; i++ {
		c := quantity[i]
		switch {
		case c >= '0' && c <= '9':
		case c == '.' && dot < 0 && i > 0 && i < n-1:
			dot = i
		default:
			return 0, ErrFormat
		}
	}
	if n == 0 {
		return 0, ErrFormat
	}
	info, ok := table[unit]
	if !ok {
		return 0, ErrUnknownUnit
	}
	if info.base != base {
		return 0, ErrDimension
	}
	intPart, fracPart := quantity, ""
	if dot >= 0 {
		intPart, fracPart = quantity[:dot], quantity[dot+1:]
	}
	for len(fracPart) > 0 && fracPart[len(fracPart)-1] == '0' {
		fracPart = fracPart[:len(fracPart)-1]
	}
	if len(fracPart) > info.zero {
		if base == Piece {
			return 0, ErrFractionalPcs
		}
		return 0, ErrFraction
	}
	// целая часть с насыщением выше предела
	var iv int64
	over := false
	for i := 0; i < len(intPart) && !over; i++ {
		iv = iv*10 + int64(intPart[i]-'0')
		if iv > int64(Limit) {
			over = true
		}
	}
	if over {
		return 0, ErrLimit
	}
	total, err := Mul(Qty(iv), info.mult)
	if err != nil {
		return 0, err
	}
	if len(fracPart) > 0 {
		fv, _ := strconv.Atoi(fracPart) // до 3 цифр
		scale := info.mult
		for i := 0; i < len(fracPart); i++ {
			scale /= 10
		}
		part, err := Mul(Qty(fv), scale)
		if err != nil {
			return 0, err
		}
		if total, err = Add(total, part); err != nil {
			return 0, err
		}
	}
	if total == 0 {
		return 0, ErrZero
	}
	return total, nil
}

// Add складывает количества; ошибка при переполнении int64 или выходе за Limit.
func Add(a, b Qty) (Qty, error) {
	if a < 0 || b < 0 {
		return 0, ErrNegative
	}
	if a > math.MaxInt64-b {
		return 0, ErrOverflow
	}
	r := a + b
	if r > Limit {
		return 0, ErrLimit
	}
	return r, nil
}

// Mul умножает количество на целый множитель; ошибка при переполнении или выходе за Limit.
func Mul(a Qty, n int64) (Qty, error) {
	if a < 0 || n < 0 {
		return 0, ErrNegative
	}
	if a != 0 && n > math.MaxInt64/int64(a) {
		return 0, ErrOverflow
	}
	r := Qty(int64(a) * n)
	if r > Limit {
		return 0, ErrLimit
	}
	return r, nil
}

// Format возвращает строку «<число> <единица>» в базовых единицах, например «1500 g».
func Format(q Qty, base Unit) string {
	return strconv.FormatInt(int64(q), 10) + " " + string(base)
}
