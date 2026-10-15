package domain

import (
	"testing"
	"time"
)

// Якорь К3 (критерии кейса, 9 баллов): после прыжка времени риск продуктов с далёким сроком
// не растёт скачком — он меняется не быстрее, чем спрос за пропущенные рабочие часы.
func TestRiskFarExpiryNoJumpAfterTimeJump(t *testing.T) {
	far := at(11).AddDate(0, 0, 10)
	const rate = 10.0
	batches := []Batch{{ID: 1, Remaining: 5000, ExpiresAt: far}}
	riskAt := func(now time.Time) Qty { return RiskByBatch(batches, rate, now, schedule)[0].Risk }

	for _, jump := range []time.Duration{time.Hour, 6 * time.Hour, 24 * time.Hour, 72 * time.Hour, 7 * 24 * time.Hour} {
		a, b := at(11), at(11).Add(jump)
		lost := WorkingHours(a, b, schedule)
		d := riskAt(b) - riskAt(a)
		if d < 0 || float64(d) > rate*lost+1 {
			t.Errorf("jump %v: risk changed by %d, working hours skipped %v", jump, d, lost)
		}
	}
}

// Продукт, который успеет разойтись при обычном спросе, не создаёт риска.
func TestRiskZeroWhenSellsOut(t *testing.T) {
	batches := []Batch{{ID: 1, Remaining: 1000, ExpiresAt: at(20)}}
	if r := RiskByBatch(batches, 200, at(11), schedule)[0].Risk; r != 0 {
		t.Fatalf("risk %d, want 0", r)
	}
}
