// Статус приходит с бэкенда; здесь только отображение. Шкала срочности из дизайн-системы
// ещё не заполнена (docs/design/design-system.md), поэтому цвета — CSS-переменные с запасными значениями.
export type BatchStatusValue = "ok" | "expiring" | "expired";

export interface StatusView {
  label: string;
  color: string;
  background: string;
}

const VIEWS: Record<BatchStatusValue, StatusView> = {
  ok: { label: "Годна", color: "var(--urgency-ok-fg, #1b5e20)", background: "var(--urgency-ok-bg, #e8f5e9)" },
  expiring: { label: "Истекает", color: "var(--urgency-warn-fg, #8a4b00)", background: "var(--urgency-warn-bg, #fff3e0)" },
  expired: { label: "Просрочена", color: "var(--urgency-bad-fg, #b00020)", background: "var(--urgency-bad-bg, #fdecea)" },
};

export function statusView(status: string): StatusView {
  return VIEWS[status as BatchStatusValue] ?? { label: status, color: "inherit", background: "transparent" };
}
