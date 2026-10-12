import { renderToStaticMarkup } from "react-dom/server";
import { MantineProvider } from "@mantine/core";
import { describe, expect, it } from "vitest";
import { theme } from "../theme/theme";
import { BATCH_STATES, SUPPLY_STATES } from "../lib/urgency";
import { Card, DataTable, SecondaryButton, EmptyState, ErrorState, ForbiddenState, HandNote, Logo, Pill, UrgencyBadge, UrgencyCounter } from ".";

const html = (node: React.ReactNode) => renderToStaticMarkup(<MantineProvider theme={theme}>{node}</MantineProvider>);

describe("общие компоненты", () => {
  it("бейдж срочности: метка и иконка всегда", () => {
    const out = html(<UrgencyBadge def={BATCH_STATES.doomed} />);
    expect(out).toContain("Спишется");
    expect(out).toContain("<svg");
    expect(out).toContain("--urgency-risk-bg");
    expect(html(<UrgencyBadge def={SUPPLY_STATES.now} />)).toContain("Заказать сейчас");
  });
  it("переключатель: выбранный залит --primary, остальные контурные", () => {
    const on = html(<SecondaryButton aria-pressed>7 дней</SecondaryButton>);
    const off = html(<SecondaryButton aria-pressed={false}>Сутки</SecondaryButton>);
    expect(on).toMatch(/background:\s*var\(--primary\)/);
    expect(on).toContain("var(--on-primary)");
    expect(off).toMatch(/background:\s*transparent/);
    expect(off).not.toContain("var(--on-primary)");
  });
  it("счётчик срочности", () => {
    const out = html(<UrgencyCounter def={BATCH_STATES.expiring} n={3} text="истекают" href="#/alerts" />);
    expect(out).toContain("истекают");
    expect(out).toContain('href="#/alerts"');
    expect(out).toContain("--urgency-warn-solid");
  });
  it("таблица: подсветка изменённой строки и тон строки", () => {
    const out = html(
      <DataTable
        caption="Партии"
        columns={[{ key: "n", header: "Номер", cell: (r: { id: string }) => r.id, numeric: true }]}
        rows={[{ id: "a" }, { id: "b" }]}
        rowKey={(r) => r.id}
        rowTone={(r) => (r.id === "b" ? "risk" : undefined)}
        changed={new Set(["a"])}
      />,
    );
    expect(out).toContain("so-row-changed");
    expect(out).toContain("--urgency-risk-row");
    expect(out).toContain("<caption");
  });
  it("логотип, плашка, карточка, пометка", () => {
    expect(html(<Logo variant="inverse" withWord />)).toContain("SwapOwl");
    expect(html(<Pill>Теневой режим</Pill>)).toContain("Теневой режим");
    expect(html(<Card title="Склад">x</Card>)).toContain("Склад");
    const hand = html(<HandNote>лосось спасаем до 16:00</HandNote>);
    expect(hand).toContain('stroke-width="4"');
    expect(hand).toContain('stroke-linecap="round"');
  });
  it("состояния экрана", () => {
    expect(html(<EmptyState title="Поставок пока нет" text="Примите первую поставку" />)).toContain("Поставок пока нет");
    expect(html(<EmptyState title="x" />)).not.toContain("<button");
    const err = html(<ErrorState what="склад" reason="Сервер не ответил за 10 секунд." requestId="r-1" onRetry={() => {}} />);
    expect(err).toContain("Не удалось загрузить склад");
    expect(err).toContain("r-1");
    expect(err).toContain("Повторить");
    expect(html(<ForbiddenState roleLabel="просмотр" needLabel="оператор" />)).toContain("Ваша роль — просмотр");
  });
});
