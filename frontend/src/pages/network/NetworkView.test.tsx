import { renderToStaticMarkup } from "react-dom/server";
import { MantineProvider } from "@mantine/core";
import { describe, expect, it } from "vitest";
import { theme } from "../../theme/theme";
import type { HubPoint, HubSummary } from "../../api/network";
import { formatRub } from "../../lib/format";
import { NetworkView } from ".";
import { updateCache } from "./model";
import { pt } from "./fixtures";

const html = (node: React.ReactNode) => renderToStaticMarkup(<MantineProvider theme={theme}>{node}</MantineProvider>);
const NOW = Date.parse("2026-10-12T10:12:00Z");

const points: HubPoint[] = [
  pt({ id: "arbat", name: "Арбат", url: "https://arbat.example.com", expiring_sum_minor: 250000, expiring_batches: 2, stop_list_count: 1, stop_list_names: ["Паста"], writeoffs_today_minor: 150000, writeoffs_7d_minor: 990000, top_hot_offer: { dish_id: "s", dish_name: "Стейк из лосося" } }),
  pt({ id: "tver", name: "Тверская", expiring_sum_minor: 40000, writeoffs_today_minor: 10000, writeoffs_7d_minor: 50000 }),
  pt({ id: "lenin", name: "Ленинский", status: "unreachable", last_success_at: "2026-10-12T10:00:00Z" }),
];
const summary: HubSummary = {
  points,
  totals: { points: 3, ok: 2, unavailable: 1, expiring_batches: 2, stop_list_count: 1, writeoffs_today_minor: 160000, writeoffs_7d_minor: 1040000 },
};
// Ленинский раньше был на связи: у него в кэше последние цифры.
const cache = updateCache({ lenin: pt({ id: "lenin", name: "Ленинский", expiring_sum_minor: 100000, writeoffs_today_minor: 20000 }) }, points);

describe("NetworkView", () => {
  const out = html(<NetworkView summary={summary} cache={cache} nowMs={NOW} selected={null} onSelect={() => {}} detail={null} />);

  it("три точки в порядке риска", () => {
    const order = ["Арбат", "Ленинский", "Тверская"].map((n) => out.indexOf(`>${n}<`));
    expect(order.every((i) => i > 0)).toBe(true);
    expect(order).toEqual([...order].sort((a, b) => a - b));
  });
  it("недоступная точка серым: нет связи N мин и данные последнего опроса", () => {
    expect(out).toContain('data-offline="true"');
    expect(out).toContain("Нет связи 12 мин");
    expect(out).toContain("данные последнего опроса");
    expect(out).toContain(formatRub(100000)); // истекающее из кэша, ₽
  });
  it("стоп-лист, горячее предложение и итог по сети", () => {
    expect(out).toContain("1: Паста");
    expect(out).toContain("Стейк из лосося");
    expect(out).toContain("Итого по сети");
    expect(out).toContain("на связи 2 из 3");
  });
  it("подробности без загруженных лент не выдают пустые ленты", () => {
    const d = html(<NetworkView summary={summary} cache={cache} nowMs={NOW} selected="lenin" onSelect={() => {}} detail={null} />);
    expect(d).toContain("Ленты недоступны");
    expect(d).not.toContain("Стоп-лист пуст");
    expect(d).not.toContain("Нет предупреждений");
  });
  it("подробности: ленты и ссылка «Открыть точку»", () => {
    const detail: HubPoint = {
      ...points[0],
      alerts: [{ batch_id: 1, product_id: "salmon", product_name: "Лосось", remaining: { qty: 1400, unit: "g" }, expires_at: "2026-10-12T14:00:00Z", time_left_seconds: 7200, status: "expiring", doomed: false }],
      stop_list: [{ id: "pasta", name: "Паста с лососем" }],
    };
    const d = html(<NetworkView summary={summary} cache={cache} nowMs={NOW} selected="arbat" onSelect={() => {}} detail={detail} />);
    expect(d).toContain("Лосось");
    expect(d).toContain("Паста с лососем");
    expect(d).toContain('href="https://arbat.example.com"');
    expect(d).toContain("Открыть точку");
  });
});

describe("NetworkView: перемещения", () => {
  const transfers = [{
    product_id: "salmon", product_name: "Лосось", batch_id: 17, qty: { qty: 1200, unit: "g" as const }, from_id: "arbat", from_name: "Арбат",
    to_id: "tver", to_name: "Тверская", expires_at: "2026-10-12T20:00:00Z", saves_minor: 240000, text: "", invoice: "Накладная",
  }];
  it("блок с подсказкой, ₽ и кнопкой накладной", () => {
    const out = html(<NetworkView summary={summary} cache={cache} nowMs={NOW} selected={null} onSelect={() => {}} detail={null} transfers={transfers} />);
    expect(out).toContain("Перемещения");
    expect(out).toContain("Лосось");
    expect(out).toContain("Арбат → Тверская");
    expect(out).toContain(formatRub(240000));
    expect(out).toContain("Скопировать накладную перемещения");
  });
  it("пустой список — пояснение, без transfers блока нет", () => {
    const empty = html(<NetworkView summary={summary} cache={cache} nowMs={NOW} selected={null} onSelect={() => {}} detail={null} transfers={[]} />);
    expect(empty).toContain("Перемещать нечего");
    const none = html(<NetworkView summary={summary} cache={cache} nowMs={NOW} selected={null} onSelect={() => {}} detail={null} />);
    expect(none).not.toContain("Перемещения");
  });
});
