import { expect, type APIRequestContext, type PlaywrightWorkerArgs } from "@playwright/test";

export interface Qty { qty: number; unit: string }
export interface Batch {
  id: number; product_id: string; quantity: Qty; remaining: Qty;
  received_at: string; expires_at: string; time_left_seconds: number; status: "ok" | "expiring" | "expired";
}
export interface Dish {
  id: string; name: string; available: boolean; servings: number;
  recipe: { product_id: string; quantity: Qty }[];
  missing: { product_id: string; product_name: string; missing: Qty }[];
}
export interface Product { id: string; name: string; unit: string; available: Qty }
export interface Clock { now: string; speed: number; timezone: string; simulated: boolean; simulator_running: boolean }

const token = process.env.E2E_DEMO_TOKEN ?? "";

/** Клиент API стенда. Демо-ручки (сброс, скорость, прыжок) — с DEMO_TOKEN. */
export class Stand {
  private constructor(readonly api: APIRequestContext) {}

  static async open(playwright: PlaywrightWorkerArgs["playwright"], baseURL: string): Promise<Stand> {
    expect(token, "задайте E2E_DEMO_TOKEN — DEMO_TOKEN стенда").not.toBe("");
    return new Stand(await playwright.request.newContext({ baseURL }));
  }

  async get<T>(path: string): Promise<T> {
    const res = await this.api.get(path);
    expect(res.status(), `GET ${path}: ${await res.text()}`).toBe(200);
    return (await res.json()) as T;
  }

  async post(path: string, body: unknown, demo = false) {
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (demo) headers.Authorization = `Bearer ${token}`;
    const res = await this.api.post(path, { data: body, headers });
    const text = await res.text();
    let json: any = null;
    try { json = JSON.parse(text); } catch { /* не JSON */ }
    return { status: res.status(), json, text };
  }

  async demo(path: string, body: unknown = {}) {
    const r = await this.post(path, body, true);
    expect(r.status, `POST ${path}: ${r.text}`).toBe(200);
    return r.json;
  }

  reset() { return this.demo("/api/demo/reset"); }
  speed(speed: number) { return this.demo("/api/demo/clock/speed", { speed }); }
  jump(hours: number) { return this.demo("/api/demo/clock/jump", { hours }); }
  simulator(running: boolean) { return this.post("/api/demo/simulator", { running }); }

  clock() { return this.get<Clock>("/api/clock"); }
  dishes() { return this.get<Dish[]>("/api/dishes"); }
  stopList() { return this.get<Dish[]>("/api/stop-list"); }
  products() { return this.get<Product[]>("/api/products"); }
  batches(productId: string) { return this.get<Batch[]>(`/api/batches?product_id=${encodeURIComponent(productId)}`); }

  async productByName(name: string): Promise<Product> {
    const p = (await this.products()).find((x) => x.name === name);
    expect(p, `продукт «${name}» в справочнике`).toBeTruthy();
    return p!;
  }
  async dishByName(name: string): Promise<Dish> {
    const d = (await this.dishes()).find((x) => x.name === name);
    expect(d, `блюдо «${name}» в меню`).toBeTruthy();
    return d!;
  }
}

export function uuid(): string {
  return crypto.randomUUID();
}

/** Время стенда в зоне ресторана, «ЧЧ:ММ». */
export function localHM(iso: string, tz: string): string {
  return new Intl.DateTimeFormat("ru-RU", { timeZone: tz, hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).format(new Date(iso));
}

export const hoursBetween = (a: string, b: string) => (Date.parse(b) - Date.parse(a)) / 3_600_000;
