import type { BenchResult } from "../../api/types";

const fmt = (n: number) => n.toLocaleString("ru-RU", { maximumFractionDigits: 1 });

/** Цифра сравнения алгоритмов; null — результата нет, ничего не утверждаем (solution.md, 2.7 «Граница»). */
export function benchSentence(b: BenchResult | null): string | null {
  if (!b || !b.available) return null;
  if (b.runs == null || b.waste_reduction_vs_random_pct == null || b.waste_reduction_vs_nearest_pct == null) return null;
  return `Основной алгоритм снижает списания на ${fmt(b.waste_reduction_vs_random_pct)} % против случайных и на ${fmt(
    b.waste_reduction_vs_nearest_pct,
  )} % против ближайшего срока, ${b.runs} прогонов`;
}
