const NBSP = " ";

// Копейки → «12 340 ₽» (с копейками только если они есть: «12 340,50 ₽»). Только форматирование.
export function formatRub(minor: number): string {
  const sign = minor < 0 ? "−" : "";
  const abs = Math.abs(Math.trunc(minor));
  const rub = Math.floor(abs / 100).toString().replace(/\B(?=(\d{3})+(?!\d))/g, NBSP);
  const kop = abs % 100;
  return `${sign}${rub}${kop ? `,${kop.toString().padStart(2, "0")}` : ""}${NBSP}₽`;
}

const REASONS: Record<string, string> = { expired: "истёк срок", other: "без причины" };

export function reasonLabel(reason: string | undefined): string {
  return reason ? (REASONS[reason] ?? reason) : "";
}
