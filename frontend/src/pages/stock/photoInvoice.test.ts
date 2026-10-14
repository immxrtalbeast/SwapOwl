import { describe, expect, it } from "vitest";
import type { InvoiceDraft, ProductStock } from "../../api/types";
import { buildPhotoDelivery, claimText, discrepancies, draftRows, normQty, normSupplier, rowErrors } from "./photoInvoice";

const products = [
  { id: "milk", name: "Молоко", unit: "ml" },
  { id: "egg", name: "Яйцо", unit: "pcs" },
  { id: "parmesan", name: "Пармезан", unit: "g" },
] as unknown as ProductStock[];

const draft: InvoiceDraft = {
  number: "4471", supplier: "ООО «Свежие продукты»", date: "2026-10-14", model: "m", demo: false,
  lines: [
    { raw_name: "Молоко 1 л × 12", product_id: "milk", quantity: "12", unit: "l", expires_on: null, shelf_life_hours: 120, confidence: "high" },
    { raw_name: "Пармезан", product_id: "parmesan", quantity: "1.2", unit: "kg", expires_on: "2026-11-01", shelf_life_hours: null, confidence: "low", note: "вес" },
    { raw_name: "Йогурт", product_id: null, quantity: "20", unit: "pcs", expires_on: null, shelf_life_hours: 168, confidence: "low" },
  ],
};

describe("photoInvoice", () => {
  it("нормализует количество", () => {
    expect(normQty("1,5")).toBe("1.5");
    expect(normQty(" 2 ")).toBe("2");
    expect(normQty("0")).toBeNull();
    expect(normQty("-1")).toBeNull();
    expect(normQty("много")).toBeNull();
  });

  it("проверяет строки: продукт, размерность, больше накладной, дробные штуки; брак ошибок не имеет", () => {
    const rows = draftRows(draft);
    expect(rowErrors(rows[0], products)).toEqual([]);
    expect(rowErrors(rows[2], products)).toContain("Выберите продукт");
    expect(rowErrors({ ...rows[2], productId: "milk" }, products)[0]).toMatch(/не подходит/);
    expect(rowErrors({ ...rows[2], productId: "egg", accepted: "2.5" }, products)).toContain("Штуки — только целые");
    expect(rowErrors({ ...rows[0], accepted: "13" }, products)).toContain("Принято больше, чем в накладной");
    expect(rowErrors({ ...rows[2], decision: "reject" }, products)).toEqual([]);
  });

  it("недовоз и брак попадают в акт, принятое — в поставку", () => {
    const rows = draftRows(draft);
    rows[1] = { ...rows[1], accepted: "1,0" };
    rows[2] = { ...rows[2], decision: "reject" };
    const diffs = discrepancies(rows);
    expect(diffs).toEqual([
      { name: "Пармезан", kind: "недовоз", qty: "0.2", unit: "kg" },
      { name: "Йогурт", kind: "брак", qty: "20", unit: "pcs" },
    ]);
    const text = claimText({ number: "4471", supplier: draft.supplier }, diffs, "14.10.2026");
    expect(text).toContain("№ 4471");
    expect(text).toContain("Пармезан — недовоз 0,2 кг");
    expect(text).toContain("Йогурт — брак 20 шт");

    const d = buildPhotoDelivery(draft, "4471", draft.supplier!, rows, new Date("2026-10-13T08:00:00Z"));
    expect(d.items).toEqual([
      { product_id: "milk", name: "Молоко 1 л × 12", quantity: "12", unit: "l", shelf_life_hours: 120 },
      { product_id: "parmesan", name: "Пармезан", quantity: "1.0", unit: "kg", expires_on: "2026-11-01" },
    ]);
    expect(d.id).toMatch(/^photo-[0-9a-f]{32}$/);
    expect(d.created_at).toBe("2026-10-14T00:00:00.000Z");
  });

  it("id не зависит от состава, даты и написания поставщика: повтор того же документа — тот же id", () => {
    const at = new Date("2026-10-13T08:00:00Z");
    const a = buildPhotoDelivery(draft, "4471", "ООО «Свежие продукты»", draftRows(draft), at);
    const b = buildPhotoDelivery({ ...draft, date: undefined }, " 4471 ", 'ООО "Свежие  Продукты"', draftRows(draft).slice(0, 1), at);
    expect(a.id).toBe(b.id);
    expect(normSupplier("ИП Петров А.А.")).toBe("петроваа");
    expect(normSupplier("ООО «Аорта»")).toBe("аорта");
    expect(buildPhotoDelivery(draft, "4472", "ООО «Свежие продукты»", draftRows(draft), at).id).not.toBe(a.id);
    expect(buildPhotoDelivery(draft, "4471", "ООО «Другой»", draftRows(draft), at).id).not.toBe(a.id);
  });

  it("без прочитанной даты created_at — момент распознавания, а не отправки", () => {
    const at = new Date("2026-10-13T08:00:00Z");
    expect(buildPhotoDelivery({ ...draft, date: "" }, "1", "", draftRows(draft), at).created_at).toBe(at.toISOString());
    expect(buildPhotoDelivery({ ...draft, date: "2026-13-45" }, "1", "", draftRows(draft), at).created_at).toBe(at.toISOString());
  });

  it("без расхождений акта нет", () => {
    expect(claimText({ number: "1", supplier: "" }, [], "14.10.2026")).toBe("");
  });
});
