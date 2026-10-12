import type { HubPoint, HubSeriesPoint, HubSummary } from "../../api/network";
import { formatRub } from "../../lib/format";

/** Период опроса hub экраном, мс. */
export const POLL_MS = 60_000;
/** Период опроса, пока идёт A/B-прогон, мс. */
export const AB_POLL_MS = 2_500;

/** Последние успешные данные по точкам: недоступная точка показывается ими, а не пустыми цифрами. */
export type Cache = Record<string, HubPoint>;

export interface PointRow {
  point: HubPoint;
  /** Данные для цифр: свежие, у недоступной — последнего опроса; null, если успешных опросов не было. */
  data: HubPoint | null;
  online: boolean;
  /** Минут без связи (с последнего успешного опроса); null — успеха не было вовсе. */
  offlineMin: number | null;
  /** Время последнего успешного опроса (ISO) — для подписи у серой строки. */
  lastSuccessAt: string | null;
}

export function updateCache(cache: Cache, points: readonly HubPoint[]): Cache {
  const next: Cache = {};
  for (const p of points) {
    if (p.status === "ok") next[p.id] = p;
    else if (cache[p.id]) next[p.id] = cache[p.id];
  }
  return next;
}

export function minutesSince(iso: string | null, nowMs: number): number | null {
  if (!iso) return null;
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return null;
  return Math.max(0, Math.floor((nowMs - t) / 60_000));
}

/** Сортировка по риску: истекающее в ₽ по убыванию (null — в конец), затем число истекающих партий, затем стоп-лист, затем имя. */
export function riskCompare(a: PointRow, b: PointRow): number {
  const sum = (r: PointRow) => r.data?.expiring_sum_minor ?? -1;
  const d = sum(b) - sum(a);
  if (d !== 0) return d;
  const batches = (r: PointRow) => (r.data ? r.data.expiring_batches + r.data.expired_batches : -1);
  const e = batches(b) - batches(a);
  if (e !== 0) return e;
  const stop = (r: PointRow) => r.data?.stop_list_count ?? -1;
  const f = stop(b) - stop(a);
  if (f !== 0) return f;
  return a.point.name.localeCompare(b.point.name, "ru");
}

export function buildRows(points: readonly HubPoint[], cache: Cache, nowMs: number): PointRow[] {
  const rows = points.map<PointRow>((point) => {
    const online = point.status === "ok";
    const data = online ? point : cache[point.id] ?? null;
    const lastSuccessAt = point.last_success_at ?? data?.last_success_at ?? null;
    return { point, data, online, lastSuccessAt, offlineMin: online ? null : minutesSince(lastSuccessAt, nowMs) };
  });
  return rows.sort(riskCompare);
}

export interface NetworkTotals {
  points: number;
  online: number;
  expiringSumMinor: number | null;
  stopListCount: number;
  writeoffsTodayMinor: number;
  writeoffs7dMinor: number;
}

/** Итог по сети — только по точкам на связи (как в hub): устаревшие цифры в сумму не идут. */
export function networkTotals(summary: Pick<HubSummary, "points" | "totals">): NetworkTotals {
  const ok = summary.points.filter((p) => p.status === "ok");
  const sums = ok.map((p) => p.expiring_sum_minor).filter((v): v is number => v != null);
  return {
    points: summary.totals.points,
    online: summary.totals.ok,
    expiringSumMinor: sums.length ? sums.reduce((a, b) => a + b, 0) : null,
    stopListCount: summary.totals.stop_list_count,
    writeoffsTodayMinor: summary.totals.writeoffs_today_minor,
    writeoffs7dMinor: summary.totals.writeoffs_7d_minor,
  };
}

/** «3: Паста, Салат, Суп» — число и до трёх названий. */
export function stopListText(data: Pick<HubPoint, "stop_list_count" | "stop_list_names"> | null): string {
  if (!data) return "—";
  if (data.stop_list_count === 0) return "нет";
  const names = data.stop_list_names.slice(0, 3).join(", ");
  const more = data.stop_list_count - Math.min(3, data.stop_list_names.length);
  return `${data.stop_list_count}: ${names}${more > 0 ? ` и ещё ${more}` : ""}`;
}

/** Время для подписи у серой строки: «14:05». */
export function clockText(iso: string | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
}

/** Пара точек группы ab; null, если группы нет или в ней не две точки. */
export function abPair(points: readonly HubPoint[]): [HubPoint, HubPoint] | null {
  const g = points.filter((p) => p.group === "ab");
  return g.length === 2 ? [g[0], g[1]] : null;
}

/** Идёт ли прогон хотя бы на одной точке пары: тогда экран обновляется часто. */
export function abRunning(points: readonly HubPoint[]): boolean {
  const pair = abPair(points);
  return pair !== null && pair.some((p) => p.run?.running === true);
}

export interface AbSide {
  point: HubPoint;
  label: string;
  /** Списано с начала прогона, копейки; null — прогона ещё нет. */
  totalMinor: number | null;
  series: HubSeriesPoint[];
}

export interface AbComparison {
  /** Точка со SwapOwl и точка-сравнение («Без подсказок»). */
  ours: AbSide;
  base: AbSide;
  /** Разница ours − base, копейки; отрицательная — SwapOwl сэкономил. null, пока нет данных у обеих. */
  diffMinor: number | null;
  /** Разница в процентах от списаний базовой точки; null, если база ещё не списала ничего. */
  diffPct: number | null;
  running: boolean;
}

const side = (point: HubPoint, fallback: string): AbSide => ({
  point,
  label: point.label?.trim() || fallback,
  totalMinor: point.run ? point.run.total_minor : null,
  series: point.run?.series ?? [],
});

/** Базовая точка — та, чья подпись начинается с «Без»; иначе вторая в конфиге. */
export function compareAb(pair: readonly [HubPoint, HubPoint]): AbComparison {
  const baseFirst = /^\s*без/i.test(pair[0].label ?? "") && !/^\s*без/i.test(pair[1].label ?? "");
  const [o, b] = baseFirst ? [pair[1], pair[0]] : pair;
  const ours = side(o, "Со SwapOwl");
  const base = side(b, "Без подсказок");
  const diffMinor = ours.totalMinor !== null && base.totalMinor !== null ? ours.totalMinor - base.totalMinor : null;
  const diffPct = diffMinor !== null && base.totalMinor ? (diffMinor / base.totalMinor) * 100 : null;
  return { ours, base, diffMinor, diffPct, running: pair.some((p) => p.run?.running === true) };
}

/** «−12 400 ₽ (−18,3 %)»; «—», пока данных нет. */
export function diffText(c: Pick<AbComparison, "diffMinor" | "diffPct">): string {
  if (c.diffMinor === null) return "—";
  const rub = c.diffMinor > 0 ? `+${formatRub(c.diffMinor)}` : formatRub(c.diffMinor);
  if (c.diffPct === null) return rub;
  const abs = Math.abs(c.diffPct).toFixed(1).replace(".", ",");
  const sign = c.diffPct > 0 ? "+" : c.diffPct < 0 ? "−" : "";
  return `${rub} (${sign}${abs}\u00a0%)`;
}
