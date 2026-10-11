import type { Qty } from "../api/types";

// Форматы SwapOwl (.ai/frontend-prompt.md, «Форматы»): по одной функции на формат.
// Время и даты — в зоне ресторана (поле timezone из GET /api/clock), а не в зоне браузера.
// Относительное время считается от времени стенда, а не от часов браузера.

export const NBSP = " ";
const MINUS = "−";

const nf = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 3 });
const nfInt = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 0 });

/** Число по-русски: запятая, не больше трёх знаков, неразрывный пробел в разрядах. */
export function formatNumber(n: number): string {
  return nf.format(n).replace(/[ \s]/g, NBSP).replace("-", MINUS);
}

/** «1,2 кг», «350 г», «1,5 л», «500 мл», «12 шт». От 1000 г или мл — в кг или л. */
export function formatQty({ qty, unit }: Qty): string {
  switch (unit) {
    case "g":
      return Math.abs(qty) >= 1000 ? `${formatNumber(qty / 1000)}${NBSP}кг` : `${formatNumber(qty)}${NBSP}г`;
    case "ml":
      return Math.abs(qty) >= 1000 ? `${formatNumber(qty / 1000)}${NBSP}л` : `${formatNumber(qty)}${NBSP}мл`;
    default:
      return `${formatNumber(qty)}${NBSP}шт`;
  }
}

/** «−23 %» (минус U+2212, неразрывный пробел). Знак «+» не добавляется. */
export function formatPercent(value: number): string {
  return `${formatNumber(Math.round(value))}${NBSP}%`;
}

/** Деньги из копеек: «1 240 ₽», с копейками только если они есть: «1 240,50 ₽». */
export function formatRub(minor: number): string {
  const sign = minor < 0 ? MINUS : "";
  const abs = Math.abs(Math.trunc(minor));
  const rub = nfInt.format(Math.floor(abs / 100)).replace(/[ \s]/g, NBSP);
  const kop = abs % 100;
  return `${sign}${rub}${kop ? `,${String(kop).padStart(2, "0")}` : ""}${NBSP}₽`;
}

/** Длительность целым числом единиц: минуты → «45 мин», дальше «2 ч 15 мин», «14 ч», «1 д 6 ч», «9 д». */
function formatSpan(ms: number): string {
  const min = Math.floor(ms / 60_000);
  if (min < 60) return `${min}${NBSP}мин`;
  const h = Math.floor(min / 60);
  if (h < 6) {
    const m = min % 60;
    return m ? `${h}${NBSP}ч${NBSP}${m}${NBSP}мин` : `${h}${NBSP}ч`;
  }
  if (h < 24) return `${h}${NBSP}ч`;
  const d = Math.floor(h / 24);
  const rest = h % 24;
  return d < 3 && rest ? `${d}${NBSP}д${NBSP}${rest}${NBSP}ч` : `${d}${NBSP}д`;
}

/**
 * Относительное время от `now` (время стенда): «сейчас», «через 45 мин», «2 ч назад».
 * С `expired: true` прошедший срок годности пишется «истекла 3 ч назад».
 */
export function formatRelative(target: Date | string | number, now: Date | number, opts: { expired?: boolean } = {}): string {
  const diff = new Date(target).getTime() - new Date(now).getTime();
  const span = formatSpan(Math.abs(diff));
  if (Math.abs(diff) < 60_000) return opts.expired && diff < 0 ? `истекла${NBSP}только что` : "сейчас";
  if (diff > 0) return `через${NBSP}${span}`;
  return opts.expired ? `истекла${NBSP}${span}${NBSP}назад` : `${span}${NBSP}назад`;
}

const ZONE_ABBR: Record<string, string> = {
  "Europe/Moscow": "МСК",
  "Europe/Kaliningrad": "МСК−1",
  "Europe/Samara": "МСК+1",
  "Asia/Yekaterinburg": "МСК+2",
  "Asia/Omsk": "МСК+3",
  "Asia/Novosibirsk": "МСК+4",
  "Asia/Krasnoyarsk": "МСК+4",
  "Asia/Irkutsk": "МСК+5",
  "Asia/Yakutsk": "МСК+6",
  "Asia/Vladivostok": "МСК+7",
  "Asia/Magadan": "МСК+8",
  "Asia/Kamchatka": "МСК+9",
};

/** Сокращённое имя зоны: «МСК» для российских зон, иначе то, что отдаёт Intl («GMT+3»). */
export function zoneLabel(timezone: string, at: Date = new Date()): string {
  if (ZONE_ABBR[timezone]) return ZONE_ABBR[timezone];
  try {
    return new Intl.DateTimeFormat("ru-RU", { timeZone: timezone, timeZoneName: "short" })
      .formatToParts(at)
      .find((p) => p.type === "timeZoneName")?.value ?? timezone;
  } catch {
    return timezone;
  }
}

function parts(d: Date, timezone: string, opts: Intl.DateTimeFormatOptions): Record<string, string> {
  const out: Record<string, string> = {};
  for (const p of new Intl.DateTimeFormat("ru-RU", { timeZone: timezone, ...opts }).formatToParts(d)) out[p.type] = p.value;
  return out;
}

function safeParts(d: Date, timezone: string, opts: Intl.DateTimeFormatOptions) {
  try {
    return parts(d, timezone, opts);
  } catch {
    return parts(d, "UTC", opts); // неизвестная зона — не падаем, показываем UTC
  }
}

/** Время стенда для шапки: «19:20:05» и «вс, 11 октября». */
export function formatClock(d: Date, timezone: string): { time: string; day: string } {
  const t = safeParts(d, timezone, { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" });
  const date = safeParts(d, timezone, { weekday: "short", day: "numeric", month: "long" });
  return { time: `${t.hour}:${t.minute}:${t.second}`, day: `${date.weekday}, ${date.day} ${date.month}` };
}

/** «HH:MM» в зоне ресторана. */
export function formatTime(d: Date | string | number, timezone: string): string {
  const t = safeParts(new Date(d), timezone, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
  return `${t.hour}:${t.minute}`;
}

/** Дата в таблице: «9 окт., 08:00»; год — только если он не текущий (текущий считается по времени стенда). */
export function formatDateTime(d: Date | string | number, timezone: string, now: Date | number = new Date()): string {
  const date = new Date(d);
  const o = safeParts(date, timezone, { day: "numeric", month: "short", year: "numeric" });
  const cur = safeParts(new Date(now), timezone, { year: "numeric" });
  const day = `${o.day} ${o.month.replace(/\.$/, "")}.`;
  return `${day}${o.year === cur.year ? "" : ` ${o.year}`}, ${formatTime(date, timezone)}`;
}

/** Точное время для подсказки: «вс, 11 окт., 19:20 МСК». */
export function formatExact(d: Date | string | number, timezone: string): string {
  const date = new Date(d);
  const o = safeParts(date, timezone, { weekday: "short", day: "numeric", month: "short" });
  return `${o.weekday}, ${o.day} ${o.month.replace(/\.$/, "")}., ${formatTime(date, timezone)} ${zoneLabel(timezone, date)}`;
}
