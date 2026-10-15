package invariants

import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"strings"
	"testing"

	"umirhack/internal/domain/units"
)

var qtyFormat = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// Инвариант 12. Арифметика количеств: таблица из спецификация, раздел 7.3.
func TestInv12ParseTable(t *testing.T) {
	const bad = -1
	for _, tc := range []struct {
		in, unit string
		base     units.Unit
		want     int64 // bad — ошибка
	}{
		{"1500", "g", units.Gram, 1500},
		{"1.5", "kg", units.Gram, 1500},
		{"0.001", "kg", units.Gram, 1},
		{"0.0005", "kg", units.Gram, bad},
		{"2", "l", units.Milli, 2000},
		{"007", "pcs", units.Piece, 7},
		{"1.50", "kg", units.Gram, 1500},
		{"1.5", "pcs", units.Piece, bad},
		{"1", "kg", units.Milli, bad},
		{"1", "pcs", units.Gram, bad},
		{"0", "g", units.Gram, bad},
		{"0.0", "g", units.Gram, bad},
		{"-1", "g", units.Gram, bad},
		{"+1", "g", units.Gram, bad},
		{"1e3", "g", units.Gram, bad},
		{"0x10", "g", units.Gram, bad},
		{"1_0", "g", units.Gram, bad},
		{"1,5", "kg", units.Gram, bad},
		{".5", "kg", units.Gram, bad},
		{"1.", "kg", units.Gram, bad},
		{"", "g", units.Gram, bad},
		{" 1", "g", units.Gram, bad},
		{"NaN", "g", units.Gram, bad},
		{"Inf", "g", units.Gram, bad},
		{"1/3", "g", units.Gram, bad},
		{"9223372036854775808", "g", units.Gram, bad},
		{"1000000000001", "g", units.Gram, bad},
		{"1000000000", "kg", units.Gram, 1_000_000_000_000},
		{"1000000001", "kg", units.Gram, bad},
	} {
		got, err := units.Parse(tc.in, tc.unit, tc.base)
		if tc.want == bad {
			if err == nil {
				t.Errorf("Parse(%q, %s, %s) = %d, ожидалась ошибка", tc.in, tc.unit, tc.base, got)
			}
			continue
		}
		if err != nil || int64(got) != tc.want {
			t.Errorf("Parse(%q, %s, %s) = %d, %v; ожидалось %d", tc.in, tc.unit, tc.base, got, err, tc.want)
		}
	}
}

func TestInv12AddMulBounds(t *testing.T) {
	if _, err := units.Add(units.Qty(math.MaxInt64), 1); err == nil {
		t.Error("Add(MaxInt64, 1) без ошибки")
	}
	if _, err := units.Add(units.Limit, 1); err == nil {
		t.Error("Add(Limit, 1) без ошибки")
	}
	if r, err := units.Add(units.Limit-1, 1); err != nil || r != units.Limit {
		t.Errorf("Add(Limit−1, 1) = %d, %v", r, err)
	}
	if _, err := units.Mul(units.Qty(math.MaxInt64), 2); err == nil {
		t.Error("Mul(MaxInt64, 2) без ошибки")
	}
	if _, err := units.Mul(units.Qty(1<<32), 1<<32); err == nil {
		t.Error("Mul(2^32, 2^32) без ошибки")
	}
	if _, err := units.Mul(units.Limit, 2); err == nil {
		t.Error("Mul(Limit, 2) без ошибки")
	}
	if r, err := units.Mul(2, 3); err != nil || r != 6 {
		t.Errorf("Mul(2, 3) = %d, %v", r, err)
	}
	if _, err := units.Add(-1, 1); !errors.Is(err, units.ErrNegative) {
		t.Errorf("Add(-1, 1): %v", err)
	}
}

var multOf = map[string]int64{"g": 1, "kg": 1000, "ml": 1, "l": 1000, "pcs": 1}
var baseOf = map[string]units.Unit{"g": units.Gram, "kg": units.Gram, "ml": units.Milli, "l": units.Milli, "pcs": units.Piece}

func checkParse(t *testing.T, in, unit string) {
	t.Helper()
	got, err := units.Parse(in, unit, baseOf[unit])
	if !qtyFormat.MatchString(in) {
		if err == nil {
			t.Fatalf("Parse(%q, %s) принял строку не по формату: %d", in, unit, got)
		}
		return
	}
	if err != nil {
		return // отказ допустим: ноль, предел, дробь мельче единицы
	}
	// Независимый эталон на big.Rat: результат — ровно in × множитель, целый, в (0, 10^12].
	r, ok := new(big.Rat).SetString(in)
	if !ok {
		t.Fatalf("эталон не разобрал %q", in)
	}
	r.Mul(r, big.NewRat(multOf[unit], 1))
	if !r.IsInt() || r.Cmp(big.NewRat(int64(units.Limit), 1)) > 0 || r.Sign() <= 0 {
		t.Fatalf("Parse(%q, %s) = %d, а эталон %s не допускает числа", in, unit, got, r)
	}
	if r.Num().Int64() != int64(got) {
		t.Fatalf("Parse(%q, %s) = %d, эталон %s", in, unit, got, r)
	}
}

func FuzzInv12Parse(f *testing.F) {
	for _, s := range []string{"1500", "1.5", "0.001", "0.0005", "007", "9223372036854775808", "1000000000001",
		"1e3", "", "-1", " 1", "1.", ".5", "00000000000000000000001", "0.00000000000000000000001", "999999999999.999"} {
		f.Add(s, "kg")
		f.Add(s, "g")
		f.Add(s, "pcs")
	}
	f.Fuzz(func(t *testing.T, in, unit string) {
		if _, ok := multOf[unit]; !ok {
			return
		}
		checkParse(t, in, unit)
	})
}

func TestInv12ParseRoundTripSamples(t *testing.T) {
	for _, u := range []string{"g", "kg", "ml", "l", "pcs"} {
		for _, in := range []string{"1", "12", "0.5", "1.000", strings.Repeat("9", 30), "0." + strings.Repeat("0", 40) + "1"} {
			checkParse(t, in, u)
		}
	}
}
