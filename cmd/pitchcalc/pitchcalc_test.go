package main

import "testing"

func TestCalibration(t *testing.T) {
	rep, err := compute(30, 4, 1, []int{0}, defaultProfile())
	if err != nil {
		t.Fatal(err)
	}
	mean := func(i int, f func(result) float64) float64 {
		m, _ := meanSD(column(rep.Res[i], f))
		return m
	}
	pct := func(i int) float64 {
		return mean(i, func(r result) float64 { return 100 * float64(r.Writeoff) / float64(r.COGS) })
	}
	// режимы: 0 — «как придётся», 1 — LIFO, 2 — FIFO, 3 — FEFO
	if p := pct(1); p < 4 || p > 10 {
		t.Errorf("списания в худшем случае (LIFO) %.1f %% себестоимости, нужен коридор 4–10 %%", p)
	}
	// «как придётся» может быть и ниже FIFO (смесь иногда берёт короткую партию раньше), поэтому порядок с ним не фиксируем
	if !(pct(1) > pct(0) && pct(1) > pct(2) && pct(0) > pct(3) && pct(2) > pct(3)) {
		t.Errorf("ожидали LIFO > {как придётся, FIFO} > FEFO, получили %.2f / %.2f / %.2f / %.2f", pct(1), pct(0), pct(2), pct(3))
	}
	for i := range rep.Variants {
		share := mean(i, func(r result) float64 { return 100 * float64(r.COGS) / float64(r.Revenue) })
		if share < 28 || share > 35 {
			t.Errorf("%s: себестоимость %.1f %% выручки, нужно 28–35 %%", rep.Variants[i].Name, share)
		}
	}
}

func TestDeterministic(t *testing.T) {
	a, err := compute(7, 2, 5, []int{30}, defaultProfile())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := compute(7, 2, 5, []int{30}, defaultProfile())
	for i := range a.Res {
		for k := range a.Res[i] {
			if a.Res[i][k] != b.Res[i][k] {
				t.Fatalf("режим %d, прогон %d: результаты различаются", i, k)
			}
		}
	}
}
