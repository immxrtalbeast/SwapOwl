import type { DeliveryInput, DeliveryItemInput, ProductStock, Unit } from "../../api/types";
import { formatQty } from "./format";

// Разбор накладной поставщика из CSV — целиком в браузере. Бизнес-логики сроков здесь нет:
// мы лишь приводим файл к телу POST /api/deliveries, срок считает сервер.

export type DeliveryUnit = NonNullable<DeliveryItemInput["unit"]>;

export interface RawRow {
  line: number; // номер строки файла (с 1, заголовок — строка 1)
  product: string;
  quantity: string;
  unit: string;
  expires: string;
  hours: string;
}

export interface ParsedCsv {
  delimiter: ";" | ",";
  invoice: string; // из колонки «накладная»; пусто, если колонки нет
  rows: RawRow[];
  errors: string[]; // ошибки файла целиком (нет колонок и т.п.)
}

export interface ImportRow {
  line: number;
  productId: string; // пусто, если продукт не опознан
  productName: string;
  item: DeliveryItemInput | null; // null, если у строки есть ошибки
  quantityLabel: string; // «1,5 кг»
  expiryLabel: string; // «годен до 14 окт.» / «срок 72 ч»
  errors: string[];
}

/** Декодирует байты файла: UTF-8, а если это не UTF-8 — Windows-1251 (так сохраняет Excel). */
export function decodeCsv(bytes: ArrayBuffer | Uint8Array): string {
  const view = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
  let text: string;
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(view);
  } catch {
    text = new TextDecoder("windows-1251").decode(view);
  }
  return text.replace(/^﻿/, "");
}

/** Без регистра, ё→е, схлопнутые пробелы. */
export function norm(s: string): string {
  return s.toLowerCase().replace(/ё/g, "е").replace(/[\s ]+/g, " ").trim();
}

/** Ключ заголовка: только буквы и цифры, чтобы «Срок, ч», «срок ч» и «срок_ч» совпали. */
function headerKey(s: string): string {
  return norm(s).replace(/[^a-zа-я0-9]/g, "");
}

type Col = "product" | "quantity" | "unit" | "expires" | "hours" | "invoice";

const SYNONYMS: Record<Col, string[]> = {
  product: ["товар", "продукт", "наименование", "название", "product", "productid", "name"],
  quantity: ["количество", "колво", "кол", "qty", "quantity"],
  unit: ["единица", "ед", "едизм", "единицаизмерения", "unit"],
  expires: ["годендо", "срокгодности", "годен", "expireson", "expires"],
  hours: ["срокч", "срокчасов", "срокчасы", "срокхранения", "срокхраненияч", "shelflifehours"],
  invoice: ["накладная", "номернакладной", "номер", "number", "invoice"],
};

const KEY_TO_COL = new Map<string, Col>();
for (const [col, keys] of Object.entries(SYNONYMS) as [Col, string[]][]) for (const k of keys) KEY_TO_COL.set(k, col);

function detectDelimiter(text: string): ";" | "," {
  let inQuotes = false;
  let semi = 0;
  let comma = 0;
  for (const ch of text) {
    if (ch === '"') inQuotes = !inQuotes;
    else if (ch === "\n" && !inQuotes) break;
    else if (!inQuotes) {
      if (ch === ";") semi++;
      else if (ch === ",") comma++;
    }
  }
  return semi >= comma && semi > 0 ? ";" : comma > 0 ? "," : ";";
}

function splitRecords(text: string, delimiter: string): string[][] {
  const records: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let inQuotes = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (inQuotes) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          cell += '"';
          i++;
        } else inQuotes = false;
      } else cell += ch;
    } else if (ch === '"') inQuotes = true;
    else if (ch === delimiter) {
      row.push(cell);
      cell = "";
    } else if (ch === "\n" || ch === "\r") {
      if (ch === "\r" && text[i + 1] === "\n") i++;
      row.push(cell);
      records.push(row);
      row = [];
      cell = "";
    } else cell += ch;
  }
  row.push(cell);
  records.push(row);
  return records;
}

export function parseCsv(text: string): ParsedCsv {
  const clean = text.replace(/^﻿/, "");
  const delimiter = detectDelimiter(clean);
  const records = splitRecords(clean, delimiter).map((r) => r.map((c) => c.trim()));
  const out: ParsedCsv = { delimiter, invoice: "", rows: [], errors: [] };
  if (records.length === 0 || records[0].every((c) => c === "")) {
    out.errors.push("Файл пуст: в первой строке должны быть заголовки");
    return out;
  }
  const index = new Map<Col, number>();
  records[0].forEach((h, i) => {
    const col = KEY_TO_COL.get(headerKey(h));
    if (col && !index.has(col)) index.set(col, i);
  });
  for (const [col, name] of [["product", "товар"], ["quantity", "количество"]] as const)
    if (!index.has(col)) out.errors.push(`Нет колонки «${name}»`);
  if (!index.has("expires") && !index.has("hours")) out.errors.push("Нет колонки со сроком: «годен до» или «срок, ч»");
  if (out.errors.length) return out;

  const get = (r: string[], c: Col) => (index.has(c) ? (r[index.get(c)!] ?? "") : "");
  const invoices = new Set<string>();
  records.slice(1).forEach((r, i) => {
    if (r.every((c) => c === "")) return;
    const inv = get(r, "invoice");
    if (inv) invoices.add(inv);
    out.rows.push({ line: i + 2, product: get(r, "product"), quantity: get(r, "quantity"), unit: get(r, "unit"), expires: get(r, "expires"), hours: get(r, "hours") });
  });
  if (invoices.size > 1) out.errors.push(`В файле несколько номеров накладной: ${[...invoices].join(", ")}`);
  out.invoice = [...invoices][0] ?? "";
  if (out.rows.length === 0) out.errors.push("В файле нет позиций");
  return out;
}

/** Номер накладной из имени файла: «nakl-42.csv» → «nakl-42». */
export function invoiceFromFileName(name: string): string {
  return name.replace(/^.*[\\/]/, "").replace(/\.[^.]+$/, "").trim();
}

const UNIT_ALIASES: Record<string, DeliveryUnit> = {
  g: "g", г: "g", гр: "g", грамм: "g",
  kg: "kg", кг: "kg", килограмм: "kg",
  ml: "ml", мл: "ml",
  l: "l", л: "l", литр: "l",
  pcs: "pcs", шт: "pcs", штука: "pcs", штук: "pcs",
};
const UNIT_BASE: Record<DeliveryUnit, Unit> = { g: "g", kg: "g", ml: "ml", l: "ml", pcs: "pcs" };
const UNIT_FACTOR: Record<DeliveryUnit, number> = { g: 1, kg: 1000, ml: 1, l: 1000, pcs: 1 };
const MONTHS = ["янв.", "февр.", "мар.", "апр.", "мая", "июн.", "июл.", "авг.", "сент.", "окт.", "нояб.", "дек."];

/** «14.10.2026» или «2026-10-14» → «2026-10-14»; null, если формат или дата неверны. */
export function parseDate(s: string): string | null {
  const m = /^(\d{1,2})\.(\d{1,2})\.(\d{4})$/.exec(s) ?? /^(\d{4})-(\d{1,2})-(\d{1,2})$/.exec(s);
  if (!m) return null;
  const iso = s.includes("-");
  const [y, mo, d] = iso ? [+m[1], +m[2], +m[3]] : [+m[3], +m[2], +m[1]];
  const dt = new Date(Date.UTC(y, mo - 1, d));
  if (dt.getUTCFullYear() !== y || dt.getUTCMonth() !== mo - 1 || dt.getUTCDate() !== d) return null;
  return `${y}-${String(mo).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
}

export function formatExpiry(isoDate: string): string {
  const [, m, d] = isoDate.split("-").map(Number);
  return `годен до ${d} ${MONTHS[m - 1]}`;
}

/** Находит продукт по id или названию (без регистра и ё/е). */
export function matchProduct(text: string, products: readonly ProductStock[]): ProductStock | undefined {
  const n = norm(text);
  if (!n) return undefined;
  return products.find((p) => norm(p.id) === n) ?? products.find((p) => norm(p.name) === n);
}

/** Проверяет строку против справочника. productOverride — выбор менеджера из выпадающего списка. */
export function resolveRow(raw: RawRow, products: readonly ProductStock[], productOverride?: string): ImportRow {
  const errors: string[] = [];
  const product = productOverride ? products.find((p) => p.id === productOverride) : matchProduct(raw.product, products);
  if (!product) errors.push(raw.product ? `Неизвестный продукт «${raw.product}»` : "Не указан продукт");

  // Количество: запятая вместо точки допустима (Excel), пробелы-разделители тысяч убираем.
  const qtyText = raw.quantity.replace(/[\s ]/g, "").replace(",", ".");
  let unit: DeliveryUnit | undefined;
  if (raw.unit) {
    unit = UNIT_ALIASES[norm(raw.unit).replace(/\.$/, "")];
    if (!unit) errors.push(`Неизвестная единица «${raw.unit}»`);
  }
  const effective: DeliveryUnit | undefined = unit ?? product?.unit;
  if (unit && product && UNIT_BASE[unit] !== product.unit)
    errors.push(`Единица «${raw.unit}» не подходит продукту «${product.name}» (он считается в ${formatQty({ qty: 1, unit: product.unit }).replace(/^1 /, "")})`);
  let quantity = "";
  if (!/^\d+(\.\d+)?$/.test(qtyText)) errors.push(raw.quantity ? `Количество «${raw.quantity}» не число` : "Не указано количество");
  else if (Number(qtyText) <= 0) errors.push("Количество должно быть больше нуля");
  else if (effective === "pcs" && !Number.isInteger(Number(qtyText))) errors.push(`Штуки должны быть целыми: «${raw.quantity}»`);
  else quantity = qtyText;

  const item: Partial<DeliveryItemInput> = {};
  let expiryLabel = "";
  if (raw.expires && raw.hours) errors.push("Укажите одно: «годен до» или «срок, ч»");
  else if (raw.expires) {
    const date = parseDate(raw.expires);
    if (!date) errors.push(`Дата «${raw.expires}» не распознана (нужно ДД.ММ.ГГГГ или ГГГГ-ММ-ДД)`);
    else {
      item.expires_on = date;
      expiryLabel = formatExpiry(date);
    }
  } else if (raw.hours) {
    const h = Number(raw.hours.replace(",", "."));
    if (!Number.isInteger(h) || h < 1 || h > 87600) errors.push(`Срок «${raw.hours}» ч: нужно целое от 1 до 87600`);
    else {
      item.shelf_life_hours = h;
      expiryLabel = `срок ${h} ч`;
    }
  } else errors.push("Нет срока годности");

  const ok = errors.length === 0 && product;
  const factor = effective ? UNIT_FACTOR[effective] : 1;
  const quantityLabel = quantity && product ? formatQty({ qty: Math.round(Number(quantity) * factor * 1000) / 1000, unit: product.unit }) : raw.quantity;
  return {
    line: raw.line,
    productId: product?.id ?? "",
    productName: product?.name ?? "",
    item: ok ? { product_id: product.id, quantity, ...(unit ? { unit } : {}), ...item } : null,
    quantityLabel,
    expiryLabel,
    errors,
  };
}

// --- Детерминированный id поставки -------------------------------------------------------------

const K = Array.from({ length: 64 }, (_, i) => {
  const p = [2, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37, 41, 43, 47, 53, 59, 61, 67, 71, 73, 79, 83, 89, 97, 101, 103, 107, 109, 113, 127, 131, 137, 139, 149, 151, 157, 163, 167, 173, 179, 181, 191, 193, 197, 199, 211, 223, 227, 229, 233, 239, 241, 251, 257, 263, 269, 271, 277, 281, 283, 293, 307, 311][i];
  return Math.floor((Math.cbrt(p) % 1) * 2 ** 32) >>> 0;
});

/** SHA-256 синхронно: crypto.subtle недоступен на http-страницах. */
export function sha256Hex(message: string): string {
  const bytes = new TextEncoder().encode(message);
  const h = [2, 3, 5, 7, 11, 13, 17, 19].map((p) => Math.floor((Math.sqrt(p) % 1) * 2 ** 32) >>> 0);
  const len = bytes.length;
  const padded = new Uint8Array(((len + 9 + 63) >> 6) << 6);
  padded.set(bytes);
  padded[len] = 0x80;
  const dv = new DataView(padded.buffer);
  dv.setUint32(padded.length - 8, Math.floor((len * 8) / 2 ** 32));
  dv.setUint32(padded.length - 4, (len * 8) >>> 0);
  const w = new Uint32Array(64);
  const rotr = (x: number, n: number) => (x >>> n) | (x << (32 - n));
  for (let off = 0; off < padded.length; off += 64) {
    for (let i = 0; i < 16; i++) w[i] = dv.getUint32(off + i * 4);
    for (let i = 16; i < 64; i++) {
      const s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >>> 3);
      const s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >>> 10);
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0;
    }
    let [a, b, c, d, e, f, g, hh] = h;
    for (let i = 0; i < 64; i++) {
      const t1 = (hh + (rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)) + ((e & f) ^ (~e & g)) + K[i] + w[i]) >>> 0;
      const t2 = ((rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)) + ((a & b) ^ (a & c) ^ (b & c))) >>> 0;
      hh = g; g = f; f = e; e = (d + t1) >>> 0; d = c; c = b; b = a; a = (t1 + t2) >>> 0;
    }
    [a, b, c, d, e, f, g, hh].forEach((v, i) => (h[i] = (h[i] + v) >>> 0));
  }
  return h.map((v) => v.toString(16).padStart(8, "0")).join("");
}

/** Тело POST /api/deliveries. id зависит от номера накладной и позиций, поэтому тот же файл → тот же id → replayed. */
export function buildDelivery(invoice: string, items: readonly DeliveryItemInput[], createdAt: Date = new Date()): DeliveryInput {
  const canonical = JSON.stringify([invoice, items.map((i) => [i.product_id, i.quantity, i.unit ?? "", i.shelf_life_hours ?? 0, i.expires_on ?? ""])]);
  return { id: `csv-${sha256Hex(canonical).slice(0, 32)}`, number: invoice, created_at: createdAt.toISOString(), items: [...items] };
}
