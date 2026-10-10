import { ApiError } from "./types";
import type { Batch, DeliveryInput, DeliveryResult, Dish, DishServings, Order, OrderItemInput, ProductStock } from "./types";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
    credentials: "same-origin",
  });
  const body = await res.json().catch(() => null);
  if (!res.ok) {
    const e = body?.error;
    throw new ApiError(res.status, e?.code ?? "internal", e?.message ?? `Ошибка ${res.status}`, e?.details ?? {});
  }
  return body as T;
}

export const api = {
  products: () => request<ProductStock[]>("/api/products"),
  batches: (productId?: string) =>
    request<Batch[]>(`/api/batches${productId ? `?product_id=${encodeURIComponent(productId)}` : ""}`),
  receiveDelivery: (d: DeliveryInput) =>
    request<DeliveryResult>("/api/deliveries", { method: "POST", body: JSON.stringify(d) }),
  servings: () => request<DishServings[]>("/api/servings"),
  dishes: () => request<Dish[]>("/api/dishes"),
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
