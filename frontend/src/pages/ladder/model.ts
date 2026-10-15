import type { LadderBatch, LadderStep } from "../../api/types";
import { formatRub } from "../money/format";

/** Порядок ступеней на экране: продать → скидка → заготовка → передать. */
export const LADDER_ORDER = ["sell", "discount", "prep", "transfer"] as const;

const STATUS_TEXT: Record<string, string> = {
  roadmap: "в разработке",
  network: "считает «Сеть»",
  unavailable: "нет варианта",
  no_price: "нет цены",
};

/** Правая колонка ступени: деньги, если ступень посчитана, иначе честная причина. */
export function stepValue(s: LadderStep): string {
  if (s.status) return STATUS_TEXT[s.status] ?? s.status;
  return `${s.net_minor > 0 ? "+" : ""}${formatRub(s.net_minor)}`;
}

/** Ступени по порядку лестницы; незнакомые виды (новая версия API) — в конец, без потери. */
export function orderedSteps(b: LadderBatch): LadderStep[] {
  const rank = (k: string) => {
    const i = (LADDER_ORDER as readonly string[]).indexOf(k);
    return i < 0 ? LADDER_ORDER.length : i;
  };
  return [...b.steps].sort((a, c) => rank(a.kind) - rank(c.kind));
}

/** Итог по партии: сколько удастся не списать, пройдя ступени по порядку. */
export function ladderSummary(b: LadderBatch): string {
  if (b.combined_minor <= 0) return "Ступени пока не дают выигрыша — решите по месту или посмотрите «Сеть».";
  return `Пройдя ступени по порядку, не спишем ≈ ${formatRub(b.combined_minor)} из ${formatRub(b.at_risk_minor)}.`;
}

/** Куда вести по ступени; у ступеней без перехода — null. */
export function stepLink(kind: string, productId: string): { href: string; label: string } | null {
  switch (kind) {
    case "sell":
      return { href: `#/hot-offers?product_id=${encodeURIComponent(productId)}`, label: "Открыть горячие" };
    case "discount":
      return { href: "#/rescue", label: "Витрина «Спаси продукты»" };
    case "transfer":
      return { href: "#/network", label: "Открыть «Сеть»" };
    default:
      return null;
  }
}
