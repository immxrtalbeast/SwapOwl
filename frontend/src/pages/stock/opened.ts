// Только подписи для вскрытой упаковки: срок считает бэкенд (POST /api/batches/{id}/open),
// здесь лишь предварительный текст подтверждения и пометка «вскрыто …».

/** «12.10 14:00» — день, месяц и время в зоне ресторана. */
export function formatOpenedStamp(t: Date | string | number, timeZone: string): string {
  const parts = new Intl.DateTimeFormat("ru-RU", { timeZone, day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).formatToParts(new Date(t));
  const get = (type: string) => parts.find((p) => p.type === type)?.value ?? "";
  return `${get("day")}.${get("month")} ${get("hour")}:${get("minute")}`;
}

/** Пометка дочерней партии: «вскрыто 12.10 14:00». */
export function openedMark(openedAt: string, timeZone: string): string {
  return `вскрыто ${formatOpenedStamp(openedAt, timeZone)}`;
}

/**
 * Момент, до которого годна вскрытая часть: меньший из срока партии и «сейчас + срок после вскрытия».
 * Предпросмотр для подтверждения; итоговый срок назначает сервер.
 */
export function previewOpenedExpiry(nowMs: number, hours: number, batchExpiresAt: string): number {
  return Math.min(nowMs + hours * 3_600_000, Date.parse(batchExpiresAt));
}

/** «молоко годно 48 ч после вскрытия — до 14.10 14:00». */
export function openConfirmText(productName: string, hours: number, nowMs: number, batchExpiresAt: string, timeZone: string): string {
  const until = formatOpenedStamp(previewOpenedExpiry(nowMs, hours, batchExpiresAt), timeZone);
  return `${productName.toLowerCase()} годно ${hours} ч после вскрытия — до ${until}`;
}
