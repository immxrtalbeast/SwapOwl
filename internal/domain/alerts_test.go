package domain

import (
	"testing"
	"time"
)

var alertNow = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
var alertCfg = ExpiryConfig{Share: 0.2, MinHours: 12, MaxShare: 0.5}

func alertBatch(id int64, rem Qty, recvAgo, leftIn time.Duration) Batch {
	return Batch{ID: id, ProductID: "p", Quantity: rem, Remaining: rem,
		ReceivedAt: alertNow.Add(-recvAgo), ExpiresAt: alertNow.Add(leftIn)}
}

func TestBuildAlertsSelectsExpiringAndRecentlyExpired(t *testing.T) {
	sch := Schedule{Location: time.UTC, OpenAt: 0, CloseAt: 24 * time.Hour}
	batches := []Batch{
		alertBatch(1, 10, time.Hour, 100*time.Hour),     // ok
		alertBatch(2, 10, 90*time.Hour, 10*time.Hour),   // 10 ч из 100: ниже порога 20 ч → истекает
		alertBatch(3, 10, 50*time.Hour, -time.Hour),     // просрочена час назад
		alertBatch(4, 10, 80*time.Hour, -25*time.Hour),  // просрочена давно
		alertBatch(5, 0, 90*time.Hour, 5*time.Hour),     // пустая
		alertBatch(6, 10, 90*time.Hour, 20*time.Minute), // истекает, обречена
	}
	got := BuildAlerts(batches, alertNow, alertCfg, sch, 0.5)
	var ids []int64
	for _, a := range got {
		ids = append(ids, a.BatchID)
	}
	want := []int64{3, 6, 2}
	if len(ids) != len(want) {
		t.Fatalf("ids %v want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids %v want %v", ids, want)
		}
	}
	byID := map[int64]Alert{}
	for _, a := range got {
		byID[a.BatchID] = a
	}
	if byID[3].Status != StatusExpired || !byID[3].Doomed || byID[3].TimeLeft >= 0 {
		t.Fatalf("expired alert: %+v", byID[3])
	}
	if byID[6].Status != StatusExpiring || !byID[6].Doomed {
		t.Fatalf("doomed alert: %+v", byID[6])
	}
	if byID[2].Status != StatusExpiring || byID[2].Doomed {
		t.Fatalf("expiring alert: %+v", byID[2])
	}
}

func TestBuildAlertsWindowBoundary(t *testing.T) {
	sch := Schedule{Location: time.UTC, OpenAt: 0, CloseAt: 24 * time.Hour}
	edge := alertBatch(1, 5, 50*time.Hour, -AlertExpiredWindow)
	if got := BuildAlerts([]Batch{edge}, alertNow, alertCfg, sch, 0.5); len(got) != 1 {
		t.Fatalf("exactly 24h ago is still shown: %+v", got)
	}
	old := alertBatch(2, 5, 50*time.Hour, -AlertExpiredWindow-time.Nanosecond)
	if got := BuildAlerts([]Batch{old}, alertNow, alertCfg, sch, 0.5); len(got) != 0 {
		t.Fatalf("older than 24h is hidden: %+v", got)
	}
}

func TestShouldNotifyExpiring(t *testing.T) {
	if !ShouldNotifyExpiring(alertBatch(1, 5, 90*time.Hour, 10*time.Hour), alertNow, alertCfg) {
		t.Fatal("expiring batch must notify")
	}
	if ShouldNotifyExpiring(alertBatch(2, 5, 90*time.Hour, -time.Second), alertNow, alertCfg) {
		t.Fatal("expired batch must not notify expiring")
	}
	if ShouldNotifyExpiring(alertBatch(3, 5, time.Hour, 100*time.Hour), alertNow, alertCfg) {
		t.Fatal("ok batch must not notify")
	}
	if ShouldNotifyExpiring(alertBatch(4, 0, 90*time.Hour, 10*time.Hour), alertNow, alertCfg) {
		t.Fatal("empty batch must not notify")
	}
}
