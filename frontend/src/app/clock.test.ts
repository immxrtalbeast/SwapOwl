import { describe, expect, it } from "vitest";
import type { ClockInfo } from "../api/client";
import { formatStandTime, modeLabel, standNow } from "./clock";

const info = (p: Partial<ClockInfo> = {}): ClockInfo => ({
  now: "2026-10-11T10:00:00Z",
  speed: 1,
  timezone: "Europe/Moscow",
  simulated: false,
  simulator_running: false,
  ...p,
});

describe("clock", () => {
  it("время стенда идёт со скоростью часов", () => {
    expect(standNow(info({ speed: 60 }), 1000, 3000).toISOString()).toBe("2026-10-11T10:02:00.000Z");
    expect(standNow(info({ speed: 0 }), 1000, 9000).toISOString()).toBe("2026-10-11T10:00:00.000Z");
  });

  it("подписывает режим", () => {
    expect(modeLabel(info())).toBe("Реальное время");
    expect(modeLabel(info({ simulated: true, speed: 3600 }))).toBe("Симуляция ×3600");
    expect(modeLabel(info({ simulated: true, speed: 0 }))).toBe("Симуляция · пауза");
  });

  it("форматирует в часовом поясе ресторана", () => {
    expect(formatStandTime(new Date("2026-10-11T10:00:00Z"), "Europe/Moscow").time).toBe("13:00:00");
  });
});
