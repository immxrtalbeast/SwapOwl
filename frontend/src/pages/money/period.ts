const DAY_MS = 86_400_000;

/** Период [from, to) в RFC 3339: последние `days` суток до момента стенда `nowIso` (+1 с, чтобы включить сам момент). */
export function moneyPeriod(nowIso: string, days: number): { from: string; to: string } {
  const nowMs = Date.parse(nowIso);
  return { from: new Date(nowMs - days * DAY_MS).toISOString(), to: new Date(nowMs + 1000).toISOString() };
}
