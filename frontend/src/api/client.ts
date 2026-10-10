import { ApiError } from "./types";
import type { Batch, DeliveryInput, DeliveryResult, ProductStock } from "./types";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
    credentials: "same-origin",
  });
  const body = await res.json().catch(() => null);
  if (!res.ok) {
    const e = body?.error;
    throw new ApiError(res.status, e?.code ?? "internal", e?.message ?? `Ошибка ${res.status}`);
  }
  return body as T;
}

export const api = {
  products: () => request<ProductStock[]>("/api/products"),
  batches: (productId?: string) =>
    request<Batch[]>(`/api/batches${productId ? `?product_id=${encodeURIComponent(productId)}` : ""}`),
  receiveDelivery: (d: DeliveryInput) =>
    request<DeliveryResult>("/api/deliveries", { method: "POST", body: JSON.stringify(d) }),
};
