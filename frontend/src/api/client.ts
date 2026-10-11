import type { Role } from "../auth/roles";
import { ApiError } from "./types";
import type { Alert, ApiKeyInfo, Batch, CreatedApiKey, UserAccount, DeliveryInput, DeliveryResult, Dish, DishServings, HotAlgo, HotOffersResponse, Order, SupplySuggestion, MoneyReport, OrderItemInput, ProductStock } from "./types";

let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: (() => void) | null) {
  onUnauthorized = fn;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
    credentials: "same-origin",
  });
  const body = await res.json().catch(() => null);
  if (!res.ok) {
    // Любой 401 ведёт на экран входа; ошибку неверного пароля при входе обрабатывает сама форма.
    if (res.status === 401 && path !== "/api/auth/login") onUnauthorized?.();
    const e = body?.error;
    throw new ApiError(res.status, e?.code ?? "internal", e?.message ?? `Ошибка ${res.status}`, e?.details ?? {});
  }
  return body as T;
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
  alerts: () => request<Alert[]>("/api/alerts"),
  hotOffers: (algo?: HotAlgo) => request<HotOffersResponse>(`/api/hot-offers${algo ? `?algo=${algo}` : ""}`),
  supplySuggestions: () => request<SupplySuggestion[]>("/api/supply-suggestions"),
  writeoffs: (from: string, to: string) => request<MoneyReport>(`/api/analytics/writeoffs?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`),
  shortfalls: (from: string, to: string) => request<MoneyReport>(`/api/analytics/shortfalls?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`),
  products: () => request<ProductStock[]>("/api/products"),
  batches: (productId?: string) =>
    request<Batch[]>(`/api/batches${productId ? `?product_id=${encodeURIComponent(productId)}` : ""}`),
  receiveDelivery: (d: DeliveryInput) =>
    request<DeliveryResult>("/api/deliveries", { method: "POST", body: JSON.stringify(d) }),
  users: () => request<{ items: UserAccount[] }>("/api/users").then((r) => r.items),
  createUser: (u: { login: string; password: string; role: Role }) =>
    request<UserAccount>("/api/users", { method: "POST", body: JSON.stringify(u) }),
  deleteUser: (id: number) => request<null>(`/api/users/${id}`, { method: "DELETE" }),
  apiKeys: () => request<{ items: ApiKeyInfo[] }>("/api/api-keys").then((r) => r.items),
  createApiKey: (k: { name: string; role: ApiKeyInfo["role"] }) =>
    request<CreatedApiKey>("/api/api-keys", { method: "POST", body: JSON.stringify(k) }),
  deleteApiKey: (id: number) => request<null>(`/api/api-keys/${id}`, { method: "DELETE" }),
  dishes: () => request<Dish[]>("/api/dishes"),
  menu: () => request<DishServings[]>("/api/dishes"),
  orders: (status?: Order["status"]) => request<Order[]>(`/api/orders${status ? `?status=${status}` : ""}`),
  // Заказ, отклонённый по нехватке (409 insufficient_stock), приходит как обычный результат со статусом rejected.
  placeOrder: async (id: string, items: OrderItemInput[]): Promise<Order> => {
    try {
      return await request<Order>("/api/orders", { method: "POST", body: JSON.stringify({ id, items }) });
    } catch (e) {
      if (e instanceof ApiError && e.code === "insufficient_stock") {
        const d = e.details as unknown as Pick<Order, "status" | "at" | "replayed" | "missing"> & { order_id: string };
        return { id: d.order_id, status: d.status, at: d.at, replayed: d.replayed, items, writeoffs: [], missing: d.missing };
      }
      throw e;
    }
  },
};
