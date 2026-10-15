import type { CSSProperties } from "react";
import type { Alert, DishServings } from "../../api/types";
import { atRiskFromAlerts } from "../../api/today";
import type { UnitPrices } from "../../api/today";

export type TgTab = "hall" | "receiving" | "manager";

export const TG_TABS: readonly { id: TgTab; title: string }[] = [
  { id: "hall", title: "Зал" },
  { id: "receiving", title: "Приёмка" },
  { id: "manager", title: "Менеджер" },
];

/** Хэш мини-приложения: «#/tg» и «#/tg/<вкладка>». */
export function isTgHash(hash: string): boolean {
  return /^#\/?tg(\/|\?|$)/.test(hash);
}

/** Вкладка по хэшу; пустой или неизвестный — «Зал». */
export function tgTabFromHash(hash: string): TgTab {
  const m = /^#\/?tg\/([^/?]+)/.exec(hash);
  const id = m?.[1];
  return TG_TABS.find((t) => t.id === id)?.id ?? "hall";
}

export function tgHash(tab: TgTab): string {
  return tab === "hall" ? "#/tg" : `#/tg/${tab}`;
}

/** Схема оформления мини-приложения. */
export type Scheme = "light" | "dark";

const asScheme = (v: unknown): Scheme | null => (v === "light" || v === "dark" ? v : null);

/**
 * Какую схему включить: выбор пользователя в самом мини-приложении главнее, иначе схема Telegram; нет ни того ни другого
 * (страница открыта не в Telegram) — null, остаётся то, что есть.
 */
export function tgSchemeToApply(telegram: unknown, manual: unknown): Scheme | null {
  return asScheme(manual) ?? asScheme(telegram);
}

/** Цвета Telegram (themeParams) накладываем только когда текущая схема совпала со схемой Telegram: иначе тёмные цвета Telegram попали бы на светлые токены и наоборот. */
export function overlayAllowed(telegram: unknown, current: Scheme): boolean {
  return asScheme(telegram) === current;
}

/** Параметры темы Telegram (themeParams): ключи snake_case, значения — цвета #rrggbb. */
export type ThemeParams = Partial<Record<string, string>>;

const THEME_VARS: readonly [string, string][] = [
  ["secondary_bg_color", "--paper"],
  ["bg_color", "--surface"],
  ["text_color", "--text"],
  ["hint_color", "--text-muted"],
  ["button_color", "--primary"],
  ["button_text_color", "--on-primary"],
];

/** Цвета Telegram поверх наших токенов: только то, что клиент прислал; без темы — пустой объект (остаются токены приложения). */
export function themeToVars(p: ThemeParams | undefined): CSSProperties {
  const out: Record<string, string> = {};
  for (const [key, cssVar] of THEME_VARS) {
    const v = p?.[key];
    if (typeof v === "string" && /^#[0-9a-f]{3,8}$/i.test(v)) out[cssVar] = v;
  }
  return out as CSSProperties;
}

/** Сводка «Сегодня» для менеджера: под риском ₽, сколько партий истекает, сколько блюд в стопе. */
export interface TodaySummary {
  atRiskMinor: number;
  unpriced: number;
  expiring: number;
  stopped: number;
}

export function todaySummary(alerts: readonly Alert[], prices: UnitPrices, menu: readonly DishServings[]): TodaySummary {
  const risk = atRiskFromAlerts(alerts, prices);
  return {
    atRiskMinor: risk.totalMinor,
    unpriced: risk.unpriced,
    expiring: alerts.filter((a) => a.status === "expiring").length,
    stopped: menu.filter((d) => !d.available).length,
  };
}

/** Стоп-лист для зала: только недоступные блюда по алфавиту. */
export function stoppedDishes(menu: readonly DishServings[]): DishServings[] {
  return menu.filter((d) => !d.available).sort((a, b) => a.name.localeCompare(b.name, "ru"));
}

/** «3 партии», «1 партия», «5 партий». */
export function batchesWord(n: number): string {
  const m10 = n % 10;
  const m100 = n % 100;
  const w = m100 >= 11 && m100 <= 14 ? "партий" : m10 === 1 ? "партия" : m10 >= 2 && m10 <= 4 ? "партии" : "партий";
  return `${n} ${w}`;
}
