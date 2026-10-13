import { formatOpenedStamp } from "../stock/opened";

export interface LabelData {
  product: string;
  /** null — партия не вскрыта: строки «вскрыто» на этикетке нет. */
  openedStamp: string | null;
  untilStamp: string;
  by: string | null;
  batchId: number;
  qrUrl: string;
}

/** Номер партии из адреса вида «#/label/42»; иначе null. */
export function labelBatchIdFromHash(hash: string): number | null {
  const m = /^#\/?label\/(\d+)\/?(?:\?.*)?$/.exec(hash);
  return m ? Number(m[1]) : null;
}

/** Ссылка из QR: партия на складе того же стенда. */
export function batchUrl(origin: string, batchId: number): string {
  return `${origin}/#/stock?batch=${batchId}`;
}

/**
 * Данные этикетки. Вскрытая часть: «вскрыто» = opened_at, «годно до» = её expires_at.
 * Партия без вскрытия: строки «вскрыто» нет (момент печати — не дата вскрытия), «годно до» — срок партии.
 */
export function buildLabel(args: {
  product: string;
  batchId: number;
  expiresAt: string;
  openedAt?: string | null;
  nowMs?: number;
  timeZone: string;
  login: string | null;
  origin: string;
}): LabelData {
  return {
    product: args.product,
    openedStamp: args.openedAt ? formatOpenedStamp(args.openedAt, args.timeZone) : null,
    untilStamp: formatOpenedStamp(args.expiresAt, args.timeZone),
    by: args.login,
    batchId: args.batchId,
    qrUrl: batchUrl(args.origin, args.batchId),
  };
}

/**
 * Этикетку «вскрыто / годно до» нельзя печатать для невскрытой партии продукта со сроком после вскрытия:
 * срок вскрытой части назначает сервер при «Вскрыл», а у запечатанной партии он другой.
 */
export function needsOpenFirst(openedAt: string | null | undefined, shelfAfterOpenHours: number | null | undefined): boolean {
  return !openedAt && !!shelfAfterOpenHours;
}

/** Партия, выделенная вскрытием из `parentId` (исходная с нулевым остатком больше не печатается). */
export function childOfOpened(batches: readonly { id: number; parent_batch_id?: number | null }[], parentId: number): number | null {
  const child = batches.find((b) => b.parent_batch_id === parentId);
  return child ? child.id : null;
}
