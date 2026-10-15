import { portionsWord } from "../hot-offers/view";

/** Строка публичной витрины: GET /api/public/rescue (FS-168). Внутренних данных склада в ней нет. */
export interface RescueItem {
  dish_id: string;
  dish_name: string;
  servings_left: number;
  discount_percent: number;
  valid_until: string;
  note: string;
}

export const RESCUE_HASH = "#/rescue";

export function isRescueHash(hash: string): boolean {
  return hash.split("?")[0] === RESCUE_HASH;
}

/** «до 16:00» — часы ресторана берём из самой строки времени, а не из пояса браузера гостя. */
export function validUntilLabel(iso: string): string {
  const m = /T(\d{2}:\d{2})/.exec(iso);
  return m ? `до ${m[1]}` : "";
}

export function leftLabel(n: number): string {
  const one = n % 10 === 1 && n % 100 !== 11;
  return `${one ? "осталась" : "осталось"} ${n} ${portionsWord(n)}`;
}

export type RescueState =
  | { kind: "loading" }
  | { kind: "ready"; items: RescueItem[] }
  | { kind: "off" }
  | { kind: "busy" }
  | { kind: "error" };

/** Ответ витрины → состояние экрана: 404 — витрина выключена, 429 — слишком часто. */
export async function loadRescue(fetcher: typeof fetch = fetch): Promise<RescueState> {
  let res: Response;
  try {
    res = await fetcher("/api/public/rescue", { headers: { Accept: "application/json" } });
  } catch {
    return { kind: "error" };
  }
  if (res.status === 404) return { kind: "off" };
  if (res.status === 429) return { kind: "busy" };
  if (!res.ok) return { kind: "error" };
  const body: unknown = await res.json().catch(() => null);
  if (!Array.isArray(body)) return { kind: "error" };
  return { kind: "ready", items: (body as RescueItem[]).filter((i) => i && typeof i.dish_name === "string") };
}

// --- Публичное меню гостя (#/guest-menu): GET /api/public/menu ---

export const GUEST_MENU_HASH = "#/guest-menu";

export function isGuestMenuHash(hash: string): boolean {
  return hash.split("?")[0] === GUEST_MENU_HASH;
}

export type MenuStatus = "available" | "few" | "stopped";

export interface GuestDish {
  dish_id: string;
  dish_name: string;
  status: MenuStatus;
}

export const MENU_STATUS_LABEL: Record<MenuStatus, string> = {
  available: "Можно заказать",
  few: "Осталось мало",
  stopped: "Нет в наличии",
};

/** Доступные сверху, «нет в наличии» — внизу; внутри группы порядок меню сохраняется. */
export function sortGuestMenu(items: readonly GuestDish[]): GuestDish[] {
  const rank: Record<MenuStatus, number> = { available: 0, few: 0, stopped: 1 };
  return items.map((d, i) => ({ d, i })).sort((a, b) => rank[a.d.status] - rank[b.d.status] || a.i - b.i).map((x) => x.d);
}

export type GuestMenuState =
  | { kind: "loading" }
  | { kind: "ready"; items: GuestDish[] }
  | { kind: "off" }
  | { kind: "busy" }
  | { kind: "error" };

export async function loadGuestMenu(fetcher: typeof fetch = fetch): Promise<GuestMenuState> {
  let res: Response;
  try {
    res = await fetcher("/api/public/menu", { headers: { Accept: "application/json" } });
  } catch {
    return { kind: "error" };
  }
  if (res.status === 404) return { kind: "off" };
  if (res.status === 429) return { kind: "busy" };
  if (!res.ok) return { kind: "error" };
  const body: unknown = await res.json().catch(() => null);
  if (!Array.isArray(body)) return { kind: "error" };
  const known = new Set<string>(["available", "few", "stopped"]);
  return {
    kind: "ready",
    items: (body as GuestDish[]).filter((d) => d && typeof d.dish_name === "string" && known.has(d.status)),
  };
}
