import type { BenchResult } from "../../api/types";

const fmt = (n: number) => n.toLocaleString("ru-RU", { maximumFractionDigits: 1 });

/** Фраза со сравнением алгоритмов; null — цифры показывать нельзя (solution.md, 2.7, «Граница»). */
export function benchSentence(b: BenchResult | null | undefined): string | null {
  if (!b || !b.available) return null;
  const { runs, waste_reduction_vs_random_pct: rnd, waste_reduction_vs_nearest_pct: near } = b;
  if (runs == null || rnd == null || near == null) return null;
  return `Основной алгоритм снижает списания на ${fmt(rnd)} % против случайных и на ${fmt(near)} % против ближайшего срока, ${runs} прогонов.`;
}
