import { describe, expect, it, vi } from "vitest";
import { isGuestMenuHash, isRescueHash, leftLabel, loadGuestMenu, loadRescue, sortGuestMenu, validUntilLabel } from "./model";

const res = (status: number, body: unknown) => vi.fn().mockResolvedValue({ status, ok: status < 400, json: async () => body }) as unknown as typeof fetch;

describe("rescue model", () => {
  it("узнаёт свой хэш", () => {
    expect(isRescueHash("#/rescue")).toBe(true);
    expect(isRescueHash("#/rescue?x=1")).toBe(true);
    expect(isRescueHash("#/stock")).toBe(false);
  });
  it("время — из строки, без пояса браузера", () => {
    expect(validUntilLabel("2026-10-09T16:00:00+03:00")).toBe("до 16:00");
    expect(validUntilLabel("???")).toBe("");
  });
  it("склоняет порции", () => {
    expect(leftLabel(1)).toBe("осталась 1 порция");
    expect(leftLabel(3)).toBe("осталось 3 порции");
    expect(leftLabel(11)).toBe("осталось 11 порций");
  });
  it("404 — витрина выключена, 429 — слишком часто, список — готово", async () => {
    expect((await loadRescue(res(404, null))).kind).toBe("off");
    expect((await loadRescue(res(429, null))).kind).toBe("busy");
    expect((await loadRescue(res(500, null))).kind).toBe("error");
    const ok = await loadRescue(res(200, [{ dish_id: "a", dish_name: "Стейк", servings_left: 4, discount_percent: 20, valid_until: "2026-10-09T16:00:00+03:00", note: "" }]));
    expect(ok.kind === "ready" && ok.items.length).toBe(1);
    expect((await loadRescue(res(200, { not: "array" }))).kind).toBe("error");
  });
});

describe("guest menu model", () => {
  const res2 = (status: number, body: unknown) => vi.fn().mockResolvedValue({ status, ok: status < 400, json: async () => body }) as unknown as typeof fetch;
  it("узнаёт свой хэш и не путает его с экраном «Меню и стоп-лист»", () => {
    expect(isGuestMenuHash("#/guest-menu")).toBe(true);
    expect(isGuestMenuHash("#/menu")).toBe(false);
  });
  it("доступные сверху, стоп внизу, порядок меню внутри групп", () => {
    const items = [
      { dish_id: "a", dish_name: "А", status: "stopped" as const },
      { dish_id: "b", dish_name: "Б", status: "available" as const },
      { dish_id: "c", dish_name: "В", status: "few" as const },
    ];
    expect(sortGuestMenu(items).map((d) => d.dish_id)).toEqual(["b", "c", "a"]);
  });
  it("404 — не включено, 429 — слишком часто, неизвестный статус отбрасывается", async () => {
    expect((await loadGuestMenu(res2(404, null))).kind).toBe("off");
    expect((await loadGuestMenu(res2(429, null))).kind).toBe("busy");
    const ok = await loadGuestMenu(res2(200, [{ dish_id: "a", dish_name: "Суп", status: "available" }, { dish_id: "b", dish_name: "X", status: "weird" }]));
    expect(ok.kind === "ready" && ok.items.length).toBe(1);
  });
});
