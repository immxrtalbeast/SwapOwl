import type { Qty, Unit } from "../../api/types";
import { NBSP, formatNumber, formatQty } from "../../lib/format";

const nf1 = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 1 });

/** Приближённое количество для «≈ 1,7 л»: не больше одного знака после запятой в крупной единице. */
function approxQty(qty: number, unit: Unit): string {
  if (unit !== "pcs" && Math.abs(qty) >= 1000) {
    return `${nf1.format(qty / 1000).replace(/\s/g, NBSP)}${NBSP}${unit === "g" ? "кг" : "л"}`;
  }
  return formatQty({ qty, unit });
}

const num = (s: string) => Number(s);

// Текст собирает сервер (domain.explainSupply) в базовых единицах без единиц измерения. Отдельных
// полей с горизонтом, сроком доставки и упаковкой в ответе нет, поэтому числа берём из текста по
// известному шаблону, а единицы — из unit строки. Не совпало с шаблоном — показываем текст как есть.
const RATE_RE =
  /^Расход ~(\d+) в день, пригодный запас (\d+) хватит на ([\d.]+) сут\.; нужно на (\d+) сут\. \(доставка (\d+) \+ покрытие (\d+)\)\. К заказу (\d+) \(упаковка (\d+)\)\.(.*)$/s;
const ZERO_RE =
  /^Расхода нет; минимальный запас — (\d+) порций × норма (\d+) = (\d+), пригодный запас (\d+)\. К заказу (\d+) \(упаковка (\d+)\)\.(.*)$/s;

function order(qty: number, pack: number, unit: Unit): string {
  const total = formatQty({ qty, unit });
  if (pack > 0 && qty % pack === 0) return `${total} (${formatNumber(qty / pack)}${NBSP}уп. по ${formatQty({ qty: pack, unit })})`;
  return `${total} (упаковка ${formatQty({ qty: pack, unit })})`;
}

/** «≈ 1,7 л в день, запас 600 мл хватит на 0,4 сут.; … к заказу 7 л (7 уп. по 1 л)». */
export function explainSupply(text: string, unit: Qty["unit"]): string {
  const q = (n: string) => formatQty({ qty: num(n), unit });
  let m = RATE_RE.exec(text);
  if (m) {
    const [, rate, stock, days, horizon, lead, cover, qty, pack, rest] = m;
    return (
      `≈${NBSP}${approxQty(num(rate), unit)} в день, запас ${q(stock)} хватит на ${formatNumber(num(days))}${NBSP}сут.; ` +
      `нужно на ${horizon}${NBSP}сут. (доставка ${lead} + покрытие ${cover}). К заказу ${order(num(qty), num(pack), unit)}.${rest}`
    );
  }
  m = ZERO_RE.exec(text);
  if (m) {
    const [, portions, norm, need, stock, qty, pack, rest] = m;
    return (
      `Расхода нет; минимальный запас — ${portions} порций × норма ${q(norm)} = ${q(need)}, пригодный запас ${q(stock)}. ` +
      `К заказу ${order(num(qty), num(pack), unit)}.${rest}`
    );
  }
  return text;
}
