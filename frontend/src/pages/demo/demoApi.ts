import { ApiError } from "../../api/types";

export const MAX_SPEED = 3600;
export const MAX_JUMP_HOURS = 720;
export const MAX_BURST = 50;

export interface BurstResult {
  dish_id: string;
  orders: number;
  accepted: number;
  rejected: number;
}

async function post<T>(path: string, body: unknown, token?: string): Promise<T> {
  const res = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    credentials: "same-origin",
    body: JSON.stringify(body),
  });
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    const e = data?.error;
    throw new ApiError(res.status, e?.code ?? "internal", e?.message ?? `Ошибка ${res.status}`, e?.details ?? {});
  }
  return data as T;
}

export const demoApi = {
  // Все действия требуют DEMO_TOKEN (2.8).
  simulator: (running: boolean, token: string) => post<{ running: boolean }>("/api/demo/simulator", { running }, token),
  burst: (dishId: string, orders: number, token: string) =>
    post<BurstResult>("/api/demo/burst", { dish_id: dishId, orders }, token),
  speed: (speed: number, token: string) => post<unknown>("/api/demo/clock/speed", { speed }, token),
  jump: (hours: number, token: string) => post<unknown>("/api/demo/clock/jump", { hours }, token),
  reset: (token: string) => post<unknown>("/api/demo/reset", {}, token),
};

/** Число в заданных границах или null; запятая как десятичный разделитель допустима. */
export function parseBounded(raw: string, min: number, max: number, minExclusive = false): number | null {
  const s = raw.trim().replace(",", ".");
  if (s === "") return null;
  const n = Number(s);
  if (!Number.isFinite(n) || n > max || n < min || (minExclusive && n === min)) return null;
  return n;
}

export function parseSpeed(raw: string): number | null {
  return parseBounded(raw, 0, MAX_SPEED);
}

export function parseJumpHours(raw: string): number | null {
  return parseBounded(raw, 0, MAX_JUMP_HOURS, true);
}

/** Целое число заказов в залпе (1…50) или null. */
export function parseBurstOrders(raw: string): number | null {
  const n = parseBounded(raw, 1, MAX_BURST);
  return n !== null && Number.isInteger(n) ? n : null;
}

export function describeBurst(r: BurstResult): string {
  return `Принято ${r.accepted}, отклонено ${r.rejected} из ${r.orders}`;
}

export const TOKEN_HINT = "нужен токен демо";

/** Действия с токеном видны только при введённом DEMO_TOKEN. */
export const tokenActionsVisible = (token: string): boolean => token.trim() !== "";
