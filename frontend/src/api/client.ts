import type { Role } from "../auth/roles";
import { ApiError, NetworkError } from "./types";
import type { Alert, ApiKeyInfo, Batch, CreatedApiKey, UserAccount, DeliveryInput, DeliveryResult, InvoiceDraft, ExpiryPatch, ReceivingBatch, Dish, DishHold, DishServings, ForecastAccuracy, LadderBatch, HotAlgo, HotOffersResponse, Order, SupplySuggestion, ForecastReport, MoneyReport, ShortagesReport, BenchResult, OrderItemInput, OrdersPage, ProductStock, SupplyOrder, SupplyOrderInput, WhatIfRequest, WhatIfResponse } from "./types";

let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: (() => void) | null) {
  onUnauthorized = fn;
}

async function requestRaw<T>(path: string, init?: RequestInit): Promise<{ body: T; headers: Headers }> {
  let res: Response;
  try {
    res = await fetch(path, {
      ...init,
      headers: { "Content-Type": "application/json", ...init?.headers },
      credentials: "same-origin",
    });
  } catch {
    // Обрыв связи: браузер даёт «Failed to fetch» — показываем человеческий текст.
    throw new NetworkError("Нет связи с сервером — данные обновятся сами");
  }
  const body = await res.json().catch(() => null);
  if (!res.ok) {
    // Любой 401 ведёт на экран входа; ошибку неверного пароля при входе обрабатывает сама форма.
    if (res.status === 401 && path !== "/api/auth/login" && path !== "/api/auth/telegram") onUnauthorized?.();
    const e = body?.error;
    throw new ApiError(res.status, e?.code ?? "internal", e?.message ?? `Ошибка ${res.status}`, e?.details ?? {});
  }
  return { body: body as T, headers: res.headers };
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  return (await requestRaw<T>(path, init)).body;
}

export interface ClockInfo {
  now: string;
  speed: number;
  timezone: string;
  simulated: boolean;
  simulator_running: boolean;
}

export const api = {
  clock: () => request<ClockInfo>("/api/clock"),
  me: () => request<{ login: string | null; role: Role; auth_required: boolean }>("/api/auth/me"),
  login: (login: string, password: string) =>
    request<{ login: string; role: Role }>("/api/auth/login", { method: "POST", body: JSON.stringify({ login, password }) }),
  logout: () => request<null>("/api/auth/logout", { method: "POST" }),
  telegramLogin: (initData: string) =>
    request<{ login: string | null; role: Role }>("/api/auth/telegram", { method: "POST", body: JSON.stringify({ init_data: initData }) }),
  changePassword: (old: string, next: string) =>
    request<null>("/api/auth/password", { method: "POST", body: JSON.stringify({ old, new: next }) }),
  alerts: () => request<Alert[]>("/api/alerts"),
  hotOffers: (algo?: HotAlgo) => request<HotOffersResponse>(`/api/hot-offers${algo ? `?algo=${algo}` : ""}`),
  setHotOverride: (dishId: string, mode: "pin" | "hide", note = "") =>
    request<unknown>(`/api/hot-offers/overrides/${encodeURIComponent(dishId)}`, { method: "PUT", body: JSON.stringify({ mode, note }) }),
  deleteHotOverride: (dishId: string) =>
    request<null>(`/api/hot-offers/overrides/${encodeURIComponent(dishId)}`, { method: "DELETE" }),
  supplySuggestions: () => request<SupplySuggestion[]>("/api/supply-suggestions"),
  createSupplyOrder: (o: SupplyOrderInput) =>
    request<SupplyOrder>("/api/supply-orders", { method: "POST", body: JSON.stringify(o) }),
  supplyOrder: (id: string) => request<SupplyOrder>(`/api/supply-orders/${encodeURIComponent(id)}`),
  writeoffs: (from: string, to: string) => request<MoneyReport>(`/api/analytics/writeoffs?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`),
  forecastWriteoffs: () => request<ForecastReport>("/api/forecast/writeoffs"),
  shortfalls: (from: string, to: string) => request<MoneyReport>(`/api/analytics/shortfalls?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`),
  shortages: (from: string, to: string) => request<ShortagesReport>(`/api/analytics/shortages?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`),
  bench: () => request<BenchResult>("/api/analytics/bench"),
  rescueLadder: () => request<LadderBatch[]>("/api/rescue-ladder"),
  forecastAccuracy: (days: number) => request<ForecastAccuracy>(`/api/analytics/forecast-accuracy?days=${days}`),
  products: () => request<ProductStock[]>("/api/products"),
  batches: (productId?: string) =>
    request<Batch[]>(`/api/batches${productId ? `?product_id=${encodeURIComponent(productId)}` : ""}`),
  receiving: () => request<ReceivingBatch[]>("/api/receiving"),
  confirmExpiry: (batchId: number, patch: ExpiryPatch) =>
    request<{ batch_id: number; expires_at: string; expiry_source: "confirmed" }>(`/api/batches/${batchId}/expiry`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),
  openBatch: (batchId: number, id: string, qty?: string) =>
    request<{ child: { id: number; expires_at: string }; opened_at: string; replayed: boolean }>(`/api/batches/${batchId}/open`, {
      method: "POST",
      body: JSON.stringify(qty ? { id, qty } : { id }),
    }),
  receiveDelivery: (d: DeliveryInput) =>
    request<DeliveryResult>("/api/deliveries", { method: "POST", body: JSON.stringify(d) }),
  recognizeInvoice: (image_base64: string, media_type: string) =>
    request<InvoiceDraft>("/api/invoices/recognize", { method: "POST", body: JSON.stringify({ image_base64, media_type }) }),
  users: () => request<{ items: UserAccount[] }>("/api/users").then((r) => r.items),
  createUser: (u: { login: string; password: string; role: Role }) =>
    request<UserAccount>("/api/users", { method: "POST", body: JSON.stringify(u) }),
  deleteUser: (id: number) => request<null>(`/api/users/${id}`, { method: "DELETE" }),
  apiKeys: () => request<{ items: ApiKeyInfo[] }>("/api/api-keys").then((r) => r.items),
  createApiKey: (k: { name: string; role: ApiKeyInfo["role"] }) =>
    request<CreatedApiKey>("/api/api-keys", { method: "POST", body: JSON.stringify(k) }),
  deleteApiKey: (id: number) => request<null>(`/api/api-keys/${id}`, { method: "DELETE" }),
  whatIf: (body: WhatIfRequest) => request<WhatIfResponse>("/api/what-if", { method: "POST", body: JSON.stringify(body) }),
  ask: (question: string) => request<{ intent: string; answer: string }>("/api/assistant/ask", { method: "POST", body: JSON.stringify({ question }) }),
  dishes: () => request<Dish[]>("/api/dishes"),
  menu: () => request<DishServings[]>("/api/dishes"),
  // Страница истории: total — число заказов с этим статусом целиком (X-Total-Count), before — id последнего заказа страницы.
  ordersPage: async (status: Order["status"], opts: { limit?: number; before?: string } = {}): Promise<OrdersPage> => {
    const q = new URLSearchParams({ status });
    if (opts.limit) q.set("limit", String(opts.limit));
    if (opts.before) q.set("before", opts.before);
    const { body, headers } = await requestRaw<Order[]>(`/api/orders?${q}`);
    const total = Number(headers.get("X-Total-Count"));
    return { orders: body, total: Number.isFinite(total) && headers.has("X-Total-Count") ? total : body.length };
  },
  // Блюда на паузе у менеджера (ручная пауза блюда).
  dishHolds: () => request<DishHold[]>("/api/dish-holds"),
  setDishHold: (dishId: string, note = "") =>
    request<DishHold>(`/api/dish-holds/${encodeURIComponent(dishId)}`, { method: "PUT", body: JSON.stringify({ note }) }),
  deleteDishHold: (dishId: string) =>
    request<null>(`/api/dish-holds/${encodeURIComponent(dishId)}`, { method: "DELETE" }),
  // Заказ, отклонённый по нехватке (409 insufficient_stock) или паузе блюда (409 dish_on_hold), приходит как обычный результат со статусом rejected.
  placeOrder: async (id: string, items: OrderItemInput[]): Promise<Order> => {
    try {
      return await request<Order>("/api/orders", { method: "POST", body: JSON.stringify({ id, items }) });
    } catch (e) {
      if (e instanceof ApiError && e.code === "insufficient_stock") {
        const d = e.details as unknown as Pick<Order, "status" | "at" | "replayed" | "missing"> & { order_id: string };
        return { id: d.order_id, status: d.status, at: d.at, replayed: d.replayed, items, writeoffs: [], missing: d.missing };
      }
      if (e instanceof ApiError && e.code === "dish_on_hold") {
        const d = e.details as unknown as Pick<Order, "status" | "at" | "replayed"> & { order_id: string; dishes: NonNullable<Order["held"]> };
        return { id: d.order_id, status: d.status, at: d.at, replayed: d.replayed, items, writeoffs: [], missing: [], held: d.dishes };
      }
      throw e;
    }
  },
  // Отмена принятого заказа (POST /api/orders/{id}/cancel): проданное возвращается на склад; id — ключ идемпотентности.
  cancelOrder: (orderId: string, id: string) =>
    request<{ id: string; order_id: string; at: string; replayed: boolean }>(`/api/orders/${encodeURIComponent(orderId)}/cancel`, {
      method: "POST",
      body: JSON.stringify({ id }),
    }),
};
