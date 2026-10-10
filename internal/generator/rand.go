package generator

// rng — splitmix64: маленький детерминированный генератор от одного seed.
type rng struct{ s uint64 }

func newRNG(seed uint64) *rng { return &rng{s: seed} }

func (r *rng) next() uint64 {
	r.s += 0x9e3779b97f4a7c15
	z := r.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// intn возвращает число в [0, n). n <= 0 даёт 0.
func (r *rng) intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.next() % uint64(n))
}

// float возвращает число в [0, 1).
func (r *rng) float() float64 { return float64(r.next()>>11) / (1 << 53) }

// mix смешивает seed и номер дня в независимое начальное состояние.
func mix(seed int64, day int64) uint64 {
	r := newRNG(uint64(seed) ^ (uint64(day) * 0xd1b54a32d192ed03))
	r.next()
	return r.next()
}
