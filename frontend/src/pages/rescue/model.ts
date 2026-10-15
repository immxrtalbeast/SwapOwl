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
