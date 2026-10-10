package units

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestParseTable(t *testing.T) {
	cases := []struct {
		in, unit string
		base     Unit
		want     Qty
		err      error
	}{
		{"1500", "g", Gram, 1500, nil},
		{"1.5", "kg", Gram, 1500, nil},
		{"0.001", "kg", Gram, 1, nil},
		{"0.0005", "kg", Gram, 0, ErrFraction},
		{"2", "l", Milli, 2000, nil},
		{"007", "pcs", Piece, 7, nil},
		{"1.50", "kg", Gram, 1500, nil},
		{"1.5", "pcs", Piece, 0, ErrFractionalPcs},
		{"1", "kg", Milli, 0, ErrDimension},
		{"1", "pcs", Gram, 0, ErrDimension},
		{"0", "g", Gram, 0, ErrZero},
		{"0.0", "kg", Gram, 0, ErrZero},
		{"0.0", "pcs", Piece, 0, ErrZero},
		{"-1", "g", Gram, 0, ErrFormat},
		{"+1", "g", Gram, 0, ErrFormat},
		{"1e3", "g", Gram, 0, ErrFormat},
		{"0x10", "g", Gram, 0, ErrFormat},
		{"1_0", "g", Gram, 0, ErrFormat},
		{"1,5", "g", Gram, 0, ErrFormat},
		{".5", "g", Gram, 0, ErrFormat},
		{"1.", "g", Gram, 0, ErrFormat},
		{"", "g", Gram, 0, ErrFormat},
		{" 1", "g", Gram, 0, ErrFormat},
		{"NaN", "g", Gram, 0, ErrFormat},
		{"Inf", "g", Gram, 0, ErrFormat},
		{"1/3", "g", Gram, 0, ErrFormat},
		{"1..5", "kg", Gram, 0, ErrFormat},
		{"9223372036854775808", "g", Gram, 0, ErrLimit},
		{"99999999999999999999999999", "kg", Gram, 0, ErrLimit},
		{"1000000000001", "g", Gram, 0, ErrLimit},
		{"1000000000", "kg", Gram, 1_000_000_000_000, nil},
		{"1000000001", "kg", Gram, 0, ErrLimit},
		{"1", "oz", Gram, 0, ErrUnknownUnit},
	}
	for _, c := range cases {
		got, err := Parse(c.in, c.unit, c.base)
		if !errors.Is(err, c.err) || (c.err == nil && got != c.want) {
			t.Errorf("Parse(%q,%q,%q)=%d,%v; want %d,%v", c.in, c.unit, c.base, got, err, c.want, c.err)
		}
	}
}

func TestAddMulBoundaries(t *testing.T) {
	if r, err := Add(Limit-1, 1); err != nil || r != Limit {
		t.Fatal(r, err)
	}
	if _, err := Add(Limit, 1); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	if _, err := Add(math.MaxInt64, 1); !errors.Is(err, ErrOverflow) {
		t.Fatal(err)
	}
	if _, err := Add(-1, 1); !errors.Is(err, ErrNegative) {
		t.Fatal(err)
	}
	if r, err := Mul(Limit, 1); err != nil || r != Limit {
		t.Fatal(r, err)
	}
	if r, err := Mul(0, math.MaxInt64); err != nil || r != 0 {
		t.Fatal(r, err)
	}
	if _, err := Mul(math.MaxInt64, 2); !errors.Is(err, ErrOverflow) {
		t.Fatal(err)
	}
	if _, err := Mul(1<<32, 1<<32); !errors.Is(err, ErrOverflow) {
		t.Fatal(err)
	}
	if _, err := Mul(Limit, 2); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	if _, err := Mul(1, -1); !errors.Is(err, ErrNegative) {
		t.Fatal(err)
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"1500", "1.5", "0.001", "007", "", "1e3", "9223372036854775808", "1000000000"} {
		f.Add(s, "kg")
		f.Add(s, "g")
		f.Add(s, "pcs")
	}
	f.Fuzz(func(t *testing.T, s, unit string) {
		for _, base := range []Unit{Gram, Milli, Piece} {
			q, err := Parse(s, unit, base)
			if err != nil {
				continue
			}
			if q <= 0 || q > Limit {
				t.Fatalf("out of range %d for %q", q, s)
			}
			out := Format(q, base)
			num, u, _ := strings.Cut(out, " ")
			q2, err := Parse(num, string(base), base)
			if err != nil || q2 != q || u != string(base) {
				t.Fatalf("roundtrip %q %q -> %q -> %d,%v", s, unit, out, q2, err)
			}
		}
	})
}
