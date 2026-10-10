import type { Qty } from "../../api/types";

const nf = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 3 });

// Только форматирование: 1200 g → «1,2 кг». Бизнес-логики здесь нет.
export function formatQty({ qty, unit }: Qty): string {
  switch (unit) {
    case "g":
      return qty >= 1000 ? `${nf.format(qty / 1000)} кг` : `${nf.format(qty)} г`;
    case "ml":
      return qty >= 1000 ? `${nf.format(qty / 1000)} л` : `${nf.format(qty)} мл`;
    default:
      return `${nf.format(qty)} шт`;
  }
}

// Оставшееся время относительно: «через 14 ч», «просрочено 3 ч назад».
export function formatTimeLeft(seconds: number): string {
  const abs = Math.abs(seconds);
  const text =
    abs < 3600 ? `${Math.max(1, Math.round(abs / 60))} мин` : abs < 172800 ? `${Math.round(abs / 3600)} ч` : `${Math.round(abs / 86400)} дн`;
  return seconds >= 0 ? `через ${text}` : `просрочено ${text} назад`;
}
