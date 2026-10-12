import { createHash } from "node:crypto";
import { describe, expect, it } from "vitest";
import type { ProductStock } from "../../api/types";
import { buildDelivery, decodeCsv, invoiceFromFileName, parseCsv, parseDate, resolveRow, sha256Hex } from "./csvImport";

const product = (id: string, name: string, unit: ProductStock["unit"]): ProductStock => ({ id, name, unit, available: { qty: 0, unit }, batches_count: 0, nearest_expires_at: null });
const PRODUCTS = [product("salmon", "Лосось", "g"), product("cream", "Сливки", "ml"), product("egg", "Яйцо", "pcs"), product("beef", "Говяжья вырезка", "g")];

/** Windows-1251: кириллица А–я = 0xC0–0xFF, Ё/ё — отдельно. */
function cp1251(s: string): Uint8Array {
  return Uint8Array.from(s, (ch) => {
    const c = ch.charCodeAt(0);
    if (c < 0x80) return c;
    if (ch === "Ё") return 0xa8;
    if (ch === "ё") return 0xb8;
    return c - 0x410 + 0xc0;
  });
}

const row = (o: Partial<{ product: string; quantity: string; unit: string; expires: string; hours: string }>) => ({ line: 2, product: "Лосось", quantity: "1", unit: "", expires: "14.10.2026", hours: "", ...o });

describe("кодировка и разделитель", () => {
  const csv = "Товар;Количество;Единица;Годен до\nЛосось;1,5;кг;14.10.2026\n";
  it("UTF-8, «;»", () => {
    const p = parseCsv(decodeCsv(new TextEncoder().encode(csv)));
    expect(p.delimiter).toBe(";");
    expect(p.rows[0]).toMatchObject({ product: "Лосось", quantity: "1,5", unit: "кг", expires: "14.10.2026" });
  });
  it("Windows-1251, «;»", () => {
    expect(decodeCsv(cp1251(csv))).toBe(csv);
  });
  it("UTF-8 с BOM, «,»", () => {
    const p = parseCsv(decodeCsv(new TextEncoder().encode("\uFEFFтовар,количество,срок ч\nЛосось,2,72\n")));
    expect(p.delimiter).toBe(",");
    expect(p.errors).toEqual([]);
    expect(p.rows[0]).toMatchObject({ product: "Лосось", quantity: "2", hours: "72" });
  });
  it("Windows-1251, «,» и кавычки с запятой внутри", () => {
    const p = parseCsv(decodeCsv(cp1251('Товар,Количество,"Срок, ч"\n"Лосось, охл.","1,5",48\n')));
    expect(p.delimiter).toBe(",");
    expect(p.rows[0]).toMatchObject({ product: "Лосось, охл.", quantity: "1,5", hours: "48" });
  });
  it("синонимы и регистр заголовков, накладная из колонки", () => {
    const p = parseCsv("НАКЛАДНАЯ;Продукт;Кол-во;Ед.;Срок годности\nА-7;Яйцо;3;шт;2026-10-20\n");
    expect(p.errors).toEqual([]);
    expect(p.invoice).toBe("А-7");
  });
  it("ошибки файла: нет колонок, разные накладные", () => {
    expect(parseCsv("a;b\n1;2\n").errors.length).toBeGreaterThan(0);
    expect(parseCsv("накладная;товар;количество;срок, ч\n1;Яйцо;1;5\n2;Яйцо;1;5\n").errors[0]).toMatch(/несколько номеров/);
  });
  it("номер накладной из имени файла", () => {
    expect(invoiceFromFileName("C:\\x\\nakl-42.csv")).toBe("nakl-42");
  });
});

describe("даты", () => {
  it("оба формата и невалидные", () => {
    expect(parseDate("14.10.2026")).toBe("2026-10-14");
    expect(parseDate("2026-10-14")).toBe("2026-10-14");
    expect(parseDate("31.02.2026")).toBeNull();
    expect(parseDate("14/10/2026")).toBeNull();
  });
});

describe("resolveRow", () => {
  it("корректная строка: ё/е и регистр, запятая в числе, дата → expires_on", () => {
    const r = resolveRow(row({ product: "  говяжья ВЫРЕЗКА ", quantity: "1,5", unit: "кг" }), PRODUCTS);
    expect(r.errors).toEqual([]);
    expect(r.item).toEqual({ product_id: "beef", quantity: "1.5", unit: "kg", expires_on: "2026-10-14" });
    expect(r.quantityLabel).toBe("1,5 кг");
    expect(r.expiryLabel).toBe("годен до 14 окт.");
  });
  it("1,5 и 1.5 равны; срок в часах; ё/е", () => {
    const a = resolveRow(row({ quantity: "1,5", expires: "", hours: "72" }), PRODUCTS);
    const b = resolveRow(row({ quantity: "1.5", expires: "", hours: "72" }), PRODUCTS);
    expect(a.item).toEqual(b.item);
    expect(a.item).toMatchObject({ shelf_life_hours: 72 });
    expect(resolveRow(row({ product: "Говяжья вырезка" }), [product("b", "Говяжья вырезка", "g")]).errors).toEqual([]);
    expect(resolveRow(row({ product: "ёж" }), [product("h", "Еж", "g")]).productId).toBe("h");
  });
  it("по id продукта и выбор из списка", () => {
    expect(resolveRow(row({ product: "SALMON" }), PRODUCTS).productId).toBe("salmon");
    expect(resolveRow(row({ product: "Неведомо" }), PRODUCTS, "cream").productId).toBe("cream");
  });
  it("неизвестный продукт", () => {
    const r = resolveRow(row({ product: "Единорог" }), PRODUCTS);
    expect(r.item).toBeNull();
    expect(r.errors[0]).toMatch(/Неизвестный продукт/);
  });
  it("нет срока и оба срока", () => {
    expect(resolveRow(row({ expires: "" }), PRODUCTS).errors).toContain("Нет срока годности");
    expect(resolveRow(row({ hours: "5" }), PRODUCTS).item).toBeNull();
  });
  it("неверная дата и срок", () => {
    expect(resolveRow(row({ expires: "32.01.2026" }), PRODUCTS).item).toBeNull();
    expect(resolveRow(row({ expires: "", hours: "0" }), PRODUCTS).item).toBeNull();
  });
  it("дробные штуки", () => {
    expect(resolveRow(row({ product: "Яйцо", quantity: "1,5", unit: "шт" }), PRODUCTS).errors[0]).toMatch(/целыми/);
    expect(resolveRow(row({ product: "Яйцо", quantity: "1.5" }), PRODUCTS).item).toBeNull();
    expect(resolveRow(row({ product: "Яйцо", quantity: "12" }), PRODUCTS).item).not.toBeNull();
  });
  it("единица не той размерности", () => {
    const r = resolveRow(row({ product: "Сливки", quantity: "1", unit: "кг" }), PRODUCTS);
    expect(r.item).toBeNull();
    expect(r.errors[0]).toMatch(/не подходит/);
  });
  it("не число и ноль", () => {
    expect(resolveRow(row({ quantity: "много" }), PRODUCTS).item).toBeNull();
    expect(resolveRow(row({ quantity: "0" }), PRODUCTS).item).toBeNull();
  });
});

describe("id поставки", () => {
  it("sha256 совпадает с node:crypto", () => {
    for (const s of ["", "abc", "Лосось ".repeat(40)]) expect(sha256Hex(s)).toBe(createHash("sha256").update(s).digest("hex"));
  });
  it("детерминирован от содержимого и номера", () => {
    const items = [resolveRow(row({}), PRODUCTS).item!];
    const a = buildDelivery("НК-1", items, new Date(0));
    expect(buildDelivery("НК-1", items, new Date(1000)).id).toBe(a.id);
    expect(buildDelivery("НК-2", items).id).not.toBe(a.id);
    expect(buildDelivery("НК-1", [{ ...items[0], quantity: "2" }]).id).not.toBe(a.id);
  });
});
