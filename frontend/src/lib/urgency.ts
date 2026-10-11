// Единая шкала срочности SwapOwl (.ai/frontend-prompt.md, «Шкала срочности»).
// Статус приходит с бэкенда, фронт только выбирает тон, иконку и метку. Иконка и метка показываются всегда.

export type Tone = "ok" | "warn" | "risk" | "critical" | "neutral" | "info";

/** Имя иконки Tabler (без префикса Icon): components/UrgencyBadge сопоставляет его с компонентом. */
export type UrgencyIcon =
  | "circle-check" | "hourglass-low" | "trash-x" | "circle-x" | "circle-dashed"
  | "bolt" | "clock" | "calendar-event" | "alert-triangle" | "ban";

export interface UrgencyDef {
  tone: Tone;
  icon: UrgencyIcon;
  label: string;
}

export type BatchState = "ok" | "expiring" | "doomed" | "expired" | "spent";
export type SupplyUrgency = "now" | "soon" | "planned";
export type DishState = "available" | "low" | "stopped";

export const BATCH_STATES: Record<BatchState, UrgencyDef> = {
  ok: { tone: "ok", icon: "circle-check", label: "Свежая" },
  expiring: { tone: "warn", icon: "hourglass-low", label: "Истекает" },
  doomed: { tone: "risk", icon: "trash-x", label: "Спишется" },
  expired: { tone: "critical", icon: "circle-x", label: "Просрочена" },
  spent: { tone: "neutral", icon: "circle-dashed", label: "Израсходована" },
};

export const SUPPLY_STATES: Record<SupplyUrgency, UrgencyDef> = {
  now: { tone: "critical", icon: "bolt", label: "Заказать сейчас" },
  soon: { tone: "warn", icon: "clock", label: "Скоро" },
  planned: { tone: "info", icon: "calendar-event", label: "По плану" },
};

export const DISH_STATES: Record<DishState, UrgencyDef> = {
  available: { tone: "ok", icon: "circle-check", label: "Доступно" },
  low: { tone: "warn", icon: "alert-triangle", label: "Мало" },
  stopped: { tone: "critical", icon: "ban", label: "Стоп-лист" },
};

/** Порог «мало» только визуальный: на доступность и стоп-лист не влияет. */
export const LOW_PORTIONS = 10;

/** Порядок проверки: spent → expired → doomed → expiring → ok. */
export function batchState(b: { remaining: number; status: string; doomed?: boolean }): BatchState {
  if (b.remaining <= 0) return "spent";
  if (b.status === "expired") return "expired";
  if (b.status === "expiring") return b.doomed ? "doomed" : "expiring";
  return "ok";
}

const WORST_RANK: Record<BatchState, number> = { doomed: 4, expiring: 3, expired: 2, ok: 1, spent: 0 };

/** Состояние свёрнутой строки склада — по худшему состоянию партий: doomed > expiring > expired > ok > spent. */
export function worstBatchState(states: readonly BatchState[]): BatchState {
  let worst: BatchState = "spent";
  for (const s of states) if (WORST_RANK[s] > WORST_RANK[worst]) worst = s;
  return worst;
}

export function dishState(d: { available: boolean; portions: number }): DishState {
  if (!d.available) return "stopped";
  return d.portions < LOW_PORTIONS ? "low" : "available";
}

export interface ToneVars {
  fg: string;
  bg: string;
  /** Заливка счётчика; у neutral/info нет своей, берётся цвет текста. */
  solid: string;
  /** Текст на заливке. */
  onSolid: string;
  /** Фон строки таблицы; есть только у warn и risk. */
  row?: string;
}

export function toneVars(tone: Tone): ToneVars {
  const v = (s: string) => `var(--urgency-${tone}-${s})`;
  const hasSolid = tone === "ok" || tone === "warn" || tone === "risk" || tone === "critical";
  return {
    fg: v("fg"),
    bg: v("bg"),
    solid: hasSolid ? v("solid") : v("fg"),
    onSolid: tone === "warn" ? "var(--on-urgency-warn-solid)" : "var(--on-urgency-solid)",
    row: tone === "warn" || tone === "risk" ? v("row") : undefined,
  };
}
