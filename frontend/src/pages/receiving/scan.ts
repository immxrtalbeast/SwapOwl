import type { DeliveryItemInput, ProductStock } from "../../api/types";
import { parseGs1, type Gs1 } from "../../lib/gs1";

const KEY = "gtin-map";

/** GTIN → id продукта: удобство одного устройства, на сервер не уходит. */
export function loadGtinMap(): Record<string, string> {
  try {
    return JSON.parse(localStorage.getItem(KEY) ?? "{}") as Record<string, string>;
  } catch {
    return {};
  }
}

export function rememberGtin(gtin: string, productId: string) {
  try {
    localStorage.setItem(KEY, JSON.stringify({ ...loadGtinMap(), [gtin]: productId }));
  } catch {
    /* хранилище недоступно — пара просто не запомнится */
  }
}

export interface ScanLine {
  key: number;
  gtin: string;
  product_id: string;
  expires_on?: string;
  quantity: string;
  batch?: string;
  unit: "kg" | "pack";
}

/** Количество: вес из AI 310n (unit kg) или 1 упаковка (unit pack); пересчёт в базовую единицу делает бэкенд. */
export function lineFromScan(g: Gs1, product: ProductStock, key: number): ScanLine {
  return {
    key,
    gtin: g.gtin ?? "",
    product_id: product.id,
    expires_on: g.expiresOn,
    quantity: g.weightKg !== undefined ? String(g.weightKg) : "1",
    batch: g.batch,
    unit: g.weightKg !== undefined ? "kg" : "pack",
  };
}

export function toDeliveryItem(l: ScanLine, _products: ProductStock[]): DeliveryItemInput {
  return { product_id: l.product_id, quantity: l.quantity, unit: l.unit, ...(l.expires_on ? { expires_on: l.expires_on } : {}) };
}

export { parseGs1 };
