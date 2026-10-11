// Только арифметика для подписей кнопок; решение о сроке принимает бэкенд.
const HOUR = 3600;

/** Варианты срока хранения от момента приёмки, в сутках. */
export const SHELF_DAYS = [1, 2, 3, 5, 7, 14] as const;

/** Срок хранения, который сейчас стоит у партии, в часах (от приёмки до expires_at). */
export function currentShelfHours(receivedAt: string, expiresAt: string): number {
  return Math.round((Date.parse(expiresAt) - Date.parse(receivedAt)) / 1000 / HOUR);
}

/** Подпись срока: «3 дн», «36 ч». */
export function formatShelf(hours: number): string {
  return hours >= 24 && hours % 24 === 0 ? `${hours / 24} дн` : `${hours} ч`;
}

/** Дата в формате YYYY-MM-DD → тело запроса; пустая строка — null. */
export function datePatch(value: string): { expires_on: string } | null {
  return /^\d{4}-\d{2}-\d{2}$/.test(value) ? { expires_on: value } : null;
}
