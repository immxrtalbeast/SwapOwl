import type { ClockInfo } from "../api/client";

/** Время стенда сейчас: ответ сервера плюс прошедшее реальное время, умноженное на скорость. */
export function standNow(info: ClockInfo, fetchedAtMs: number, nowMs: number): Date {
  const base = Date.parse(info.now);
  return new Date(base + Math.max(0, nowMs - fetchedAtMs) * info.speed);
}

export function modeLabel(info: ClockInfo): string {
  if (!info.simulated) return "Реальное время";
  if (info.speed === 0) return "Симуляция · пауза";
  return `Симуляция ×${Number.isInteger(info.speed) ? info.speed : info.speed.toFixed(1)}`;
}

export function formatStandTime(d: Date, timezone: string): { time: string; day: string } {
  const opts = { timeZone: timezone } as const;
  try {
    return {
      time: d.toLocaleTimeString("ru-RU", { ...opts, hour: "2-digit", minute: "2-digit", second: "2-digit" }),
      day: d.toLocaleDateString("ru-RU", { ...opts, weekday: "short", day: "numeric", month: "long" }),
    };
  } catch {
    return { time: d.toLocaleTimeString("ru-RU"), day: d.toLocaleDateString("ru-RU") };
  }
}
