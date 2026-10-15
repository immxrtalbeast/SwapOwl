package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RemindBefore — за сколько до срока партии шлём напоминание: первое сообщение о партии приходит раньше
// (batch.expiring), это второе — «осталось меньше часа, решайте сейчас».
const RemindBefore = time.Hour

// Reminder — текст напоминания об истекающей партии: сколько осталось и что делать.
func Reminder(a Alert, left time.Duration, loc *time.Location) string {
	mins := int(left.Round(time.Minute) / time.Minute)
	if mins < 1 {
		mins = 1
	}
	var b strings.Builder
	fmt.Fprintf(&b, "⏰ Через %d мин истекает срок: %s\n", mins, a.ProductName)
	fmt.Fprintf(&b, "Партия №%d, осталось %s, срок до %s\n", a.BatchID, FormatQty(a.Remaining), a.ExpiresAt.In(loc).Format("15:04"))
	if a.Doomed {
		b.WriteString("Продать уже не успеют — подумайте о списании или раздаче персоналу.")
	} else {
		b.WriteString("Продвигайте блюда с ней сейчас, иначе партия будет списана.")
	}
	return b.String()
}

// RemindTick шлёт по одному напоминанию на партию, у которой до срока осталось не больше RemindBefore.
// Тип «Истекающие партии»; тихие часы и пауза откладывают сообщение (оно догонит в FlushQuiet), повтора в тот же час нет.
func (n *Notifier) RemindTick(ctx context.Context, alerts []Alert) {
	now := n.now(ctx)
	loc := n.loc(ctx)
	n.sendMu.Lock()
	defer n.sendMu.Unlock()
	if n.reminded == nil {
		n.reminded = map[string]bool{}
	}
	for _, a := range alerts {
		left := a.ExpiresAt.Sub(now)
		if a.Status != "expiring" || left <= 0 || left > RemindBefore {
			continue
		}
		key := fmt.Sprintf("remind:%d:%d", a.BatchID, a.ExpiresAt.Unix())
		if n.reminded[key] {
			continue
		}
		if err := n.deliver(ctx, KindExpiring, key, Reminder(a, left, loc), n.Out); err != nil {
			n.logf("напоминание о партии %d: %v", a.BatchID, err)
			continue // повторим на следующем проходе
		}
		n.reminded[key] = true
	}
}

// holdEvent — полезная нагрузка stoplist.changed от ручной паузы блюда: пустые added/removed, reason=manual_hold.
type holdEvent struct {
	Reason string `json:"reason"`
	DishID string `json:"dish_id"`
	Held   bool   `json:"held"`
	Note   string `json:"note"`
}

// parseHoldEvent распознаёт событие ручной паузы; обычное изменение стоп-листа возвращает ok=false.
func parseHoldEvent(data string) (holdEvent, bool) {
	var e holdEvent
	if json.Unmarshal([]byte(data), &e) != nil || e.Reason != "manual_hold" || e.DishID == "" {
		return holdEvent{}, false
	}
	return e, true
}

// HoldText — сообщение о ручной паузе блюда или её снятии.
func HoldText(name string, held bool, note string) string {
	if !held {
		return fmt.Sprintf("▶️ Пауза снята: «%s» снова можно заказывать.", name)
	}
	t := fmt.Sprintf("⏸ Блюдо «%s» поставлено менеджером на паузу — заказать его нельзя.", name)
	if note != "" {
		t += "\nПричина: " + note
	}
	return t
}

// notifyHold сообщает о ручной паузе (тип «Стоп-лист»). Название блюда берётся из меню; нет в меню — id.
func (n *Notifier) notifyHold(ctx context.Context, e holdEvent) {
	name := e.DishID
	if ds, err := n.API.Dishes(ctx); err == nil {
		for _, d := range ds {
			if d.ID == e.DishID {
				name = d.Name
				break
			}
		}
	}
	key := fmt.Sprintf("hold:%s:%t:%d", e.DishID, e.Held, n.now(ctx).UnixNano())
	n.sendMu.Lock()
	defer n.sendMu.Unlock()
	if err := n.deliver(ctx, KindStop, key, HoldText(name, e.Held, e.Note), n.Out); err != nil {
		n.logf("пауза блюда %s: %v", e.DishID, err)
	}
}
