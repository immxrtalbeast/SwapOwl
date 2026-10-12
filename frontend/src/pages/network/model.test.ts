import { describe, expect, it } from "vitest";
import { pt } from "./fixtures";
import { buildRows, networkTotals, stopListText, updateCache } from "./model";

const NOW = Date.parse("2026-10-12T10:12:00Z");

describe("сортировка по риску", () => {
  it("истекающее в ₽ по убыванию, без суммы — в конец", () => {
    const rows = buildRows(
      [pt({ id: "a", expiring_sum_minor: 1000 }), pt({ id: "b", expiring_sum_minor: 5000 }), pt({ id: "c", expiring_sum_minor: null, expiring_batches: 4 })],
      {}, NOW,
    );
    expect(rows.map((r) => r.point.id)).toEqual(["b", "a", "c"]);
  });
  it("при равной сумме — больше истекающих партий выше", () => {
    const rows = buildRows([pt({ id: "a", expiring_batches: 1 }), pt({ id: "b", expiring_batches: 3 })], {}, NOW);
    expect(rows.map((r) => r.point.id)).toEqual(["b", "a"]);
  });
});

describe("недоступная точка", () => {
  it("показывается данными последнего опроса и минутами без связи", () => {
    const good = pt({ id: "a", expiring_sum_minor: 7000, writeoffs_today_minor: 300 });
    const cache = updateCache({}, [good]);
    const down = pt({ id: "a", status: "unreachable", expiring_sum_minor: null, writeoffs_today_minor: 0 });
    const [row] = buildRows([down], updateCache(cache, [down]), NOW);
    expect(row.online).toBe(false);
    expect(row.data?.expiring_sum_minor).toBe(7000);
    expect(row.offlineMin).toBe(12);
  });
  it("без успешных опросов данных нет", () => {
    const [row] = buildRows([pt({ id: "x", status: "unreachable", last_success_at: null })], {}, NOW);
    expect(row.data).toBeNull();
    expect(row.offlineMin).toBeNull();
  });
});

describe("итог и стоп-лист", () => {
  it("итог суммирует только точки на связи", () => {
    const points = [pt({ id: "a", expiring_sum_minor: 100 }), pt({ id: "b", expiring_sum_minor: 200 }), pt({ id: "c", status: "error", expiring_sum_minor: 900 })];
    const t = networkTotals({ points, totals: { points: 3, ok: 2, unavailable: 1, expiring_batches: 0, stop_list_count: 2, writeoffs_today_minor: 5, writeoffs_7d_minor: 9 } });
    expect(t.expiringSumMinor).toBe(300);
    expect(t.online).toBe(2);
  });
  it("стоп-лист: число и до трёх названий", () => {
    expect(stopListText(pt({ id: "a", stop_list_count: 5, stop_list_names: ["А", "Б", "В", "Г"] }))).toBe("5: А, Б, В и ещё 2");
    expect(stopListText(pt({ id: "a" }))).toBe("нет");
    expect(stopListText(null)).toBe("—");
  });
});
