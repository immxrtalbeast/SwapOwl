import { formatOpenedStamp } from "../stock/opened";

export interface LabelData {
  product: string;
  openedStamp: string;
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
 * Партия без вскрытия: «вскрыто» — момент печати (nowMs), «годно до» — срок партии.
 */
export function buildLabel(args: {
  product: string;
  batchId: number;
  expiresAt: string;
  openedAt?: string | null;
  nowMs: number;
  timeZone: string;
  login: string | null;
  origin: string;
}): LabelData {
  const opened = args.openedAt ?? args.nowMs;
  return {
    product: args.product,
    openedStamp: formatOpenedStamp(opened, args.timeZone),
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
