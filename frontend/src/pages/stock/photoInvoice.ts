import type { DeliveryInput, DeliveryItemInput, InvoiceDraft, InvoiceLine, ProductStock } from "../../api/types";
import { sha256Hex } from "./csvImport";

/** Решение по строке при приёмке: принять (возможно, меньше, чем в накладной — недовоз) или отказать (брак). */
export type Decision = "accept" | "reject";

export interface DraftRow {
  key: number;
  line: InvoiceLine;
  productId: string; // "" — не выбран
  /** Принятое количество в line.unit; меньше line.quantity — недовоз. */
  accepted: string;
  decision: Decision;
}

export function draftRows(d: InvoiceDraft): DraftRow[] {
  return d.lines.map((line, key) => ({ key, line, productId: line.product_id ?? "", accepted: line.quantity, decision: "accept" }));
}

const UNIT_RU: Record<InvoiceLine["unit"], string> = { g: "г", kg: "кг", ml: "мл", l: "л", pcs: "шт" };
const DIM: Record<InvoiceLine["unit"], "g" | "ml" | "pcs"> = { g: "g", kg: "g", ml: "ml", l: "ml", pcs: "pcs" };

export const unitRu = (u: InvoiceLine["unit"]) => UNIT_RU[u];

/** "1,5" и "1.5" → "1.5"; не число или ≤ 0 → null. */
export function normQty(s: string): string | null {
  const v = s.trim().replace(",", ".");
  return /^\d+(\.\d+)?$/.test(v) && Number(v) > 0 ? v : null;
}

/** Ошибки строки, без которых её нельзя принять. Отказанная строка ошибок не имеет. */
export function rowErrors(r: DraftRow, products: readonly ProductStock[]): string[] {
  if (r.decision === "reject") return [];
  const out: string[] = [];
  const p = products.find((x) => x.id === r.productId);
  if (!p) out.push("Выберите продукт");
  else if (DIM[r.line.unit] !== p.unit) out.push(`Единица ${unitRu(r.line.unit)} не подходит продукту (${p.unit === "pcs" ? "шт" : p.unit === "g" ? "граммы" : "миллилитры"})`);
  const q = normQty(r.accepted);
  if (!q) out.push("Количество — число больше нуля");
  else {
    if (r.line.unit === "pcs" && !Number.isInteger(Number(q))) out.push("Штуки — только целые");
    const inv = normQty(r.line.quantity);
    if (inv && Number(q) > Number(inv)) out.push("Принято больше, чем в накладной");
  }
  return out;
}

/** Расхождения для претензии: брак — вся строка, недовоз — разница. */
export interface Discrepancy { name: string; kind: "брак" | "недовоз"; qty: string; unit: InvoiceLine["unit"] }

export function discrepancies(rows: readonly DraftRow[]): Discrepancy[] {
  const out: Discrepancy[] = [];
  for (const r of rows) {
    const inv = normQty(r.line.quantity);
    if (!inv) continue;
    if (r.decision === "reject") out.push({ name: r.line.raw_name, kind: "брак", qty: inv, unit: r.line.unit });
    else {
      const acc = normQty(r.accepted);
      const diff = acc ? Math.round((Number(inv) - Number(acc)) * 1000) / 1000 : 0;
      if (diff > 0) out.push({ name: r.line.raw_name, kind: "недовоз", qty: String(diff), unit: r.line.unit });
    }
  }
  return out;
}

/** Текст претензии поставщику по расхождениям; пусто — расхождений нет. */
export function claimText(d: Pick<InvoiceDraft, "number" | "supplier">, list: readonly Discrepancy[], date: string): string {
  if (list.length === 0) return "";
  const lines = list.map((x, i) => `${i + 1}. ${x.name} — ${x.kind} ${x.qty.replace(".", ",")} ${unitRu(x.unit)}`);
  return [`${d.supplier || "Поставщику"}`, `Акт расхождений по накладной № ${d.number}, приёмка ${date}.`, "", ...lines, "", "Просим заменить или скорректировать счёт."].join("\n");
}

/**
 * Тело POST /api/deliveries из принятых строк. id — от номера, поставщика и даты документа: тот же документ,
 * снятый повторно и принятый так же, даёт replayed; принятый иначе — delivery_conflict, а не вторую поставку.
 */
export function buildPhotoDelivery(d: InvoiceDraft, number: string, rows: readonly DraftRow[], now: Date = new Date()): DeliveryInput {
  const items: DeliveryItemInput[] = rows
    .filter((r) => r.decision === "accept")
    .map((r) => ({
      product_id: r.productId,
      name: r.line.raw_name,
      quantity: normQty(r.accepted)!,
      unit: r.line.unit,
      ...(r.line.expires_on ? { expires_on: r.line.expires_on } : r.line.shelf_life_hours ? { shelf_life_hours: r.line.shelf_life_hours } : {}),
    }));
  const created = d.date && /^\d{4}-\d{2}-\d{2}$/.test(d.date) ? new Date(`${d.date}T00:00:00Z`) : now;
  return { id: `photo-${sha256Hex(JSON.stringify([number, d.supplier ?? "", d.date ?? ""])).slice(0, 32)}`, number, created_at: created.toISOString(), items };
}

/** Снимок → JPEG не больше maxSide по длинной стороне: меньше трафика и токенов модели, текст накладной читается. */
export async function shrinkImage(file: Blob, maxSide = 1600): Promise<{ base64: string; mediaType: string }> {
  const bmp = await createImageBitmap(file);
  const k = Math.min(1, maxSide / Math.max(bmp.width, bmp.height));
  const canvas = document.createElement("canvas");
  canvas.width = Math.round(bmp.width * k);
  canvas.height = Math.round(bmp.height * k);
  canvas.getContext("2d")!.drawImage(bmp, 0, 0, canvas.width, canvas.height);
  const url = canvas.toDataURL("image/jpeg", 0.85);
  return { base64: url.slice(url.indexOf(",") + 1), mediaType: "image/jpeg" };
}
