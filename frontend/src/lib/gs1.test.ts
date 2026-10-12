import { describe, expect, it } from "vitest";
import { SAMPLE_MILK, parseGs1, parseGs1Date } from "./gs1";

const GS = "\x1d";

describe("parseGs1Date", () => {
  it("день 00 — последний день месяца", () => {
    expect(parseGs1Date("260200")).toBe("2026-02-28");
    expect(parseGs1Date("280200")).toBe("2028-02-29");
    expect(parseGs1Date("261200")).toBe("2026-12-31");
  });
  it("обычная дата и мусор", () => {
    expect(parseGs1Date("261020")).toBe("2026-10-20");
    expect(parseGs1Date("261320")).toBeUndefined();
    expect(parseGs1Date("260230")).toBeUndefined();
  });
});

describe("parseGs1", () => {
  it("DataMatrix «Честного знака»: 01+21+91+92", () => {
    const r = parseGs1(`]d2010460700123456721Ab3dE9x${GS}91EE10${GS}92dGVzdA==`)!;
    expect(r.gtin).toBe("04607001234567");
    expect(r.serial).toBe("Ab3dE9x");
    expect(r.ai["91"]).toBe("EE10");
    expect(r.ai["92"]).toBe("dGVzdA==");
    expect(r.expiresOn).toBeUndefined();
  });
  it("GS1-128 с 17 и 10", () => {
    const r = parseGs1(`01046070012345671726102010LOT42`)!;
    expect(r.gtin).toBe("04607001234567");
    expect(r.expiresOn).toBe("2026-10-20");
    expect(r.batch).toBe("LOT42");
  });
  it("партия переменной длины перед следующим AI через GS", () => {
    const r = parseGs1(`0104607001234567` + `10L24${GS}17261020`)!;
    expect(r.batch).toBe("L24");
    expect(r.expiresOn).toBe("2026-10-20");
  });
  it("вес 3103 = 1,250 кг", () => {
    const r = parseGs1(`010460700123456731030012501126100117261000`)!;
    expect(r.weightKg).toBeCloseTo(1.25);
    expect(r.producedOn).toBe("2026-10-01");
    expect(r.expiresOn).toBe("2026-10-31");
  });
  it("скобочная запись", () => {
    const r = parseGs1("(01)04607001234567(17)261020(10)A1")!;
    expect(r.expiresOn).toBe("2026-10-20");
    expect(r.batch).toBe("A1");
  });
  it("не GS1", () => {
    expect(parseGs1("hello")).toBeNull();
    expect(parseGs1("")).toBeNull();
  });
  it("пример для демо разбирается", () => {
    const r = parseGs1(SAMPLE_MILK)!;
    expect(r.gtin).toBe("04607001234567");
    expect(r.expiresOn).toBe("2026-10-20");
  });
});
