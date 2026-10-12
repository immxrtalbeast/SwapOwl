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
}

/** Количество: вес из AI 310n (кг), иначе 1 упаковка. pack_size в API продуктов нет — 1 упаковка = 1 единица учёта. */
export function lineFromScan(g: Gs1, product: ProductStock, key: number): ScanLine {
  return {
    key,
    gtin: g.gtin ?? "",
    product_id: product.id,
    expires_on: g.expiresOn,
    quantity: g.weightKg !== undefined ? String(g.weightKg) : "1",
    batch: g.batch,
  };
}

export function toDeliveryItem(l: ScanLine, _products: ProductStock[]): DeliveryItemInput {
  return { product_id: l.product_id, quantity: l.quantity, ...(l.expires_on ? { expires_on: l.expires_on } : {}) };
}

export { parseGs1 };
