import { renderToStaticMarkup } from "react-dom/server";
import { MantineProvider } from "@mantine/core";
import { describe, expect, it } from "vitest";
import { theme } from "../../theme/theme";
import type { HubPoint, HubSummary } from "../../api/network";
import { formatRub } from "../../lib/format";
import { NetworkView } from ".";
import { AbCompare } from "./AbCompare";
import { abPair, abRunning, compareAb, diffText } from "./model";
import { pt } from "./fixtures";

const html = (node: React.ReactNode) => renderToStaticMarkup(<MantineProvider theme={theme}>{node}</MantineProvider>);

const run = (total: number, running = true): HubPoint["run"] => ({
  running, started_at: "2026-10-12T10:00:00Z", stand_time: "2026-10-12T13:30:00Z", total_minor: total,
  series: [{ hour: 0, cost_minor: 0 }, { hour: 1, cost_minor: total / 4 }, { hour: 2, cost_minor: total / 2 }, { hour: 3.5, cost_minor: total }],
});
// Конфиг hub: «без подсказок» стоит первой — роли определяются подписью, а не порядком.
const without = pt({ id: "b", name: "B", label: "Без подсказок", group: "ab", run: run(1_000_000) });
const withOwl = pt({ id: "a", name: "A", label: "Со SwapOwl", group: "ab", run: run(820_000) });

describe("сравнение A/B", () => {
  it("подпись сценария — только если обе точки на профиле ab", () => {
    const ab = (id: string, label: string) => pt({ id, name: id, label, group: "ab", run: { ...run(1000)!, seed_profile: "ab" } });
    const text = "Сценарий: скоропорт в трёх блюдах. Гости берут рекомендованное блюдо в 1,5 раза чаще — допущение, как в bench";
    expect(html(<AbCompare pair={[ab("a", "Со SwapOwl"), ab("b", "Без подсказок")]} />)).toContain(text);
    expect(html(<AbCompare pair={[without, withOwl]} />)).not.toContain("Сценарий:");
  });
  it("пара — только группа ab ровно из двух точек", () => {
    expect(abPair([without, withOwl, pt({ id: "c" })])).toEqual([without, withOwl]);
    expect(abPair([without, pt({ id: "c" })])).toBeNull();
    expect(abPair([without, withOwl, pt({ id: "d", group: "ab" })])).toBeNull();
  });
  it("разница считается от точки «без подсказок»", () => {
    const c = compareAb([without, withOwl]);
    expect(c.ours.point.id).toBe("a");
    expect(c.diffMinor).toBe(-180_000);
    expect(c.diffPct).toBeCloseTo(-18, 5);
    expect(diffText(c)).toBe(`${formatRub(-180_000)} (−18,0\u00a0%)`);
  });
  it("без данных — прочерк, у нулевой базы нет процентов", () => {
    expect(diffText(compareAb([pt({ id: "a", group: "ab" }), pt({ id: "b", group: "ab" })]))).toBe("—");
    const z = compareAb([pt({ id: "a", group: "ab", run: run(500) }), pt({ id: "b", group: "ab", run: run(0) })]);
    expect(z.diffPct).toBeNull();
    expect(diffText(z)).toBe(`+${formatRub(500)}`);
  });
  it("частое обновление, пока идёт прогон", () => {
    expect(abRunning([without, withOwl])).toBe(true);
    expect(abRunning([{ ...without, run: run(1, false) }, { ...withOwl, run: run(1, false) }])).toBe(false);
    expect(abRunning([pt({ id: "x" })])).toBe(false);
  });

  const out = html(<AbCompare pair={[without, withOwl]} />);
  it("две крупные цифры с подписями, разница, пометка и график", () => {
    expect(out).toContain("Со SwapOwl");
    expect(out).toContain("Без подсказок");
    expect(out).toContain(formatRub(820_000));
    expect(out).toContain(formatRub(1_000_000));
    expect(out).toContain("списано, ₽");
    expect(out).toContain("−18,0");
    expect(out).toContain("столько не ушло в мусор");
    expect(out).toContain('data-testid="ab-chart"');
    expect((out.match(/<path d="M/g) ?? []).length).toBeGreaterThanOrEqual(2); // два графика на одной оси
  });
  it("время стенда рядом с каждой цифрой", () => {
    expect((out.match(/время стенда:/g) ?? []).length).toBe(2);
  });
  it("пометки нет, если SwapOwl не сэкономил", () => {
    const o = html(<AbCompare pair={[pt({ id: "b", label: "Без подсказок", group: "ab", run: run(100) }), pt({ id: "a", label: "Со SwapOwl", group: "ab", run: run(300) })]} />);
    expect(o).not.toContain("столько не ушло");
  });
  it("до запуска — ожидание, без падения", () => {
    const o = html(<AbCompare pair={[pt({ id: "a", group: "ab" }), pt({ id: "b", group: "ab" })]} />);
    expect(o).toContain("Ждём запуска");
  });
  it("экран «Сеть» показывает блок только при группе ab", () => {
    const sum = (points: HubPoint[]): HubSummary => ({ points, totals: { points: points.length, ok: points.length, unavailable: 0, expiring_batches: 0, stop_list_count: 0, writeoffs_today_minor: 0, writeoffs_7d_minor: 0 } });
    const view = (s: HubSummary) => html(<NetworkView summary={s} cache={{}} nowMs={0} selected={null} onSelect={() => {}} detail={null} />);
    expect(view(sum([without, withOwl]))).toContain("Живое сравнение");
    expect(view(sum([pt({ id: "x" }), pt({ id: "y" })]))).not.toContain("Живое сравнение");
  });
});
