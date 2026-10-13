import { describe, expect, it } from "vitest";
import { formatClock, formatDateTime, formatExact, formatPercent, formatQty, formatRelative, formatRub, formatTime, zoneLabel } from "./format";

const N = " ";
const MSK = "Europe/Moscow";
const now = new Date("2026-10-11T16:20:00Z"); // вс, 11 окт., 19:20 МСК

describe("formatQty", () => {
  it("человеческие единицы", () => {
    expect(formatQty({ qty: 1200, unit: "g" })).toBe(`1,2${N}кг`);
    expect(formatQty({ qty: 350, unit: "g" })).toBe(`350${N}г`);
    expect(formatQty({ qty: 1500, unit: "ml" })).toBe(`1,5${N}л`);
    expect(formatQty({ qty: 500, unit: "ml" })).toBe(`500${N}мл`);
    expect(formatQty({ qty: 12, unit: "pcs" })).toBe(`12${N}шт`);
  });
  it("не больше трёх знаков", () => {
    expect(formatQty({ qty: 1234.5678, unit: "g" })).toBe(`1,235${N}кг`);
    expect(formatQty({ qty: 1000, unit: "g" })).toBe(`1${N}кг`);
  });
});

describe("formatRelative", () => {
  const at = (ms: number) => new Date(now.getTime() + ms);
  const M = 60_000, H = 60 * M, D = 24 * H;
  it("будущее", () => {
    expect(formatRelative(at(20_000), now)).toBe("сейчас");
    expect(formatRelative(at(45 * M), now)).toBe(`через${N}45${N}мин`);
    expect(formatRelative(at(2 * H + 15 * M), now)).toBe(`через${N}2${N}ч${N}15${N}мин`);
    expect(formatRelative(at(14 * H + 40 * M), now)).toBe(`через${N}14${N}ч`);
    expect(formatRelative(at(D + 6 * H), now)).toBe(`через${N}1${N}д${N}6${N}ч`);
    expect(formatRelative(at(9 * D + 3 * H), now)).toBe(`через${N}9${N}д`);
  });
  it("прошлое и истёкший срок", () => {
    expect(formatRelative(at(-2 * H), now)).toBe(`2${N}ч${N}назад`);
    expect(formatRelative(at(-3 * H), now, { expired: true })).toBe(`истекла${N}3${N}ч${N}назад`);
  });
  it("считает от переданного времени стенда, а не от часов браузера", () => {
    const standNow = new Date("2020-01-01T00:00:00Z");
    expect(formatRelative("2020-01-01T00:45:00Z", standNow)).toBe(`через${N}45${N}мин`);
  });
});

describe("время в зоне ресторана", () => {
  it("не зависит от зоны браузера", () => {
    expect(formatTime(now, MSK)).toBe("19:20");
    expect(formatTime(now, "Asia/Vladivostok")).toBe("02:20");
    expect(formatClock(now, MSK)).toEqual({ time: "19:20:00", day: "вс, 11 октября" });
  });
  it("полночь — 00, не 24", () => {
    expect(formatTime("2026-10-11T21:05:00Z", MSK)).toBe("00:05");
  });
  it("дата в таблице, год — только не текущий", () => {
    expect(formatDateTime("2026-10-09T05:00:00Z", MSK, now)).toBe("9 окт., 08:00");
    expect(formatDateTime("2025-10-09T05:00:00Z", MSK, now)).toBe("9 окт. 2025, 08:00");
  });
  it("подсказка с зоной", () => {
    expect(formatExact(now, MSK)).toBe("вс, 11 окт., 19:20 МСК");
    expect(zoneLabel("Asia/Yekaterinburg")).toBe("МСК+2");
  });
  it("неизвестная зона не роняет", () => {
    expect(formatTime(now, "Mars/Olympus")).toBe("16:20");
  });
});

describe("проценты и деньги", () => {
  it("минус U+2212", () => {
    expect(formatPercent(-23)).toBe(`−23${N}%`);
    expect(formatPercent(5)).toBe(`5${N}%`);
  });
  it("рубли из копеек", () => {
    expect(formatRub(124000)).toBe(`1${N}240${N}₽`);
    expect(formatRub(124050)).toBe(`1${N}240,50${N}₽`);
    expect(formatRub(-5000)).toBe(`−50${N}₽`);
    expect(formatRub(0)).toBe(`0${N}₽`);
  });
});

describe("formatExact year", () => {
  it("shows the year only when it is not the current one", () => {
    const now = new Date("2026-10-13T08:00:00Z");
    expect(formatExact("2027-04-07T05:00:00Z", "Europe/Moscow", now)).toContain("2027");
    expect(formatExact("2026-10-11T16:20:00Z", "Europe/Moscow", now)).not.toContain("2026");
  });
});
