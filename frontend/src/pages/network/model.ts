import type { HubPoint, HubSummary } from "../../api/network";

/** Период опроса hub экраном, мс. */
export const POLL_MS = 60_000;

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
