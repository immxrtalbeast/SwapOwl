import type { DeliveryItemInput, ProductStock } from "../../api/types";
import { parseGs1, type Gs1 } from "../../lib/gs1";

const KEY = "gtin-map";

/** GTIN → id продукта: удобство одного устройства, на сервер не уходит. */
export function loadGtinMap(): Record<string, string> {
  try {
    return { ...BUILTIN_GTINS, ...(JSON.parse(localStorage.getItem(KEY) ?? "{}") as Record<string, string>) };
  } catch {
    return { ...BUILTIN_GTINS };
  }
}

export function rememberGtin(gtin: string, productId: string) {
  try {
    localStorage.setItem(KEY, JSON.stringify({ ...loadGtinMap(), [gtin]: productId }));
  } catch {
    /* хранилище недоступно — пара просто не запомнится */
  }
}

/** Привязки, известные без настройки: GTIN примера «молоко» (SAMPLE_MILK) → milk, чтобы на демо не нужен шаг «выберите продукт». */
const BUILTIN_GTINS: Record<string, string> = { "04607001234567": "milk" };

/** Содержимое одной упаковки в базовых единицах: API продуктов pack_size пока не отдаёт, берём из сида (milk — 1 л). */
const BUILTIN_PACK_SIZE: Record<string, number> = { milk: 1000 };

export type ProductWithPack = ProductStock & { pack_size?: number | null };

export function packSizeOf(p: ProductWithPack): number | undefined {
  const n = p.pack_size ?? BUILTIN_PACK_SIZE[p.id];
  return n && n > 1 ? n : undefined;
}

export interface ScanLine {
  key: number;
  gtin: string;
  product_id: string;
  expires_on?: string;
  /** Срок прочитан из кода (AI 17); иначе оператор вводит «годен до» с упаковки — без срока поставку не принять (FS-160). */
  expiryFromCode: boolean;
  /** Пусто — количество обязательно указать вручную (нет ни веса в коде, ни pack_size). */
  quantity: string;
  batch?: string;
  unit: "kg" | "l" | "g" | "ml" | "pcs";
}

/** Количество: вес/объём из GS1 (310n — кг, 315n — л), иначе pack_size × 1 скан в базовой единице продукта, иначе пусто. Единицу «pack» не шлём никогда. */
export function lineFromScan(g: Gs1, product: ProductWithPack, key: number): ScanLine {
  const base = { key, gtin: g.gtin ?? "", product_id: product.id, expires_on: g.expiresOn, expiryFromCode: Boolean(g.expiresOn), batch: g.batch };
  if (g.weightKg !== undefined) return { ...base, quantity: String(g.weightKg), unit: "kg" };
  if (g.volumeL !== undefined) return { ...base, quantity: String(g.volumeL), unit: "l" };
  const pack = packSizeOf(product);
  return { ...base, quantity: pack ? String(pack) : "", unit: product.unit as ScanLine["unit"] };
}

/** Продукт по коду поставщика: серверное сопоставление главнее локального (оно общее для устройств); нет нигде — undefined. */
export function resolveProductId(code: string, server: Readonly<Record<string, string>>, local: Readonly<Record<string, string>>): string | undefined {
  return server[code] ?? local[code];
}

/** Позиция, добавленная руками без кода: продукт выбран из справочника, количество и «годен до» вводит оператор. */
export function manualLine(product: ProductWithPack, key: number): ScanLine {
  return { key, gtin: "", product_id: product.id, expiryFromCode: false, quantity: "", unit: product.unit as ScanLine["unit"] };
}

/** Количество в строке — положительное число. */
export function hasQuantity(l: ScanLine): boolean {
  const n = Number(l.quantity.replace(",", "."));
  return l.quantity.trim() !== "" && Number.isFinite(n) && n > 0;
}

/** Строку можно отправить, только если есть количество и срок годности (из кода или введённый с упаковки). */
export function isLineReady(l: ScanLine): boolean {
  return hasQuantity(l) && /^\d{4}-\d{2}-\d{2}$/.test(l.expires_on ?? "");
}

export function toDeliveryItem(l: ScanLine, _products: ProductStock[]): DeliveryItemInput {
  return { product_id: l.product_id, quantity: l.quantity.trim().replace(",", "."), unit: l.unit,
    ...(l.expires_on ? { expires_on: l.expires_on } : {}) };
}

export { parseGs1 };
