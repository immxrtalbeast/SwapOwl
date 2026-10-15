/** Исход входа по подписи Telegram (POST /api/auth/telegram, FS-163) — что показать человеку. */
export type TgSignInOutcome =
  | { kind: "not_linked"; telegramUserId: string }
  | { kind: "rejected" }
  | { kind: "unavailable" };

interface ErrLike {
  status?: number;
  code?: string;
  details?: Record<string, unknown>;
}

/** Что сказать человеку, когда код привязки не принят: причины неверного кода не различаем — их не различает и сервер. */
export function linkCodeErrorText(err: unknown): string {
  const e = (err ?? {}) as ErrLike;
  if (e.status === 429) return "Слишком много неверных кодов. Подождите 10 минут и попробуйте снова.";
  if (e.status === 409) return "Этот Telegram уже привязан к другому пользователю. Попросите администратора отвязать его.";
  if (e.status === 401) return "Telegram не подтвердил вход. Закройте приложение и откройте его из бота заново.";
  if (e.status === 400) return "Код неверный, просрочен или уже использован. Получите новый в вебе: «Аккаунт» → «Привязать Telegram».";
  return "Не удалось привязать. Проверьте связь и попробуйте снова.";
}

/** Ввод кода как на экране: регистр, пробелы и дефис не важны; пусто или не 8 знаков — не отправляем. */
export function normalizeLinkInput(raw: string): string | null {
  const s = raw.replace(/[\s-]/g, "").toUpperCase();
  return /^[A-Z0-9]{8}$/.test(s) ? `${s.slice(0, 4)}-${s.slice(4)}` : null;
}

/** 403 telegram_not_linked — нужен id для привязки; 401 — подпись не принята; всё остальное (404, сеть) — обычный вход по паролю. */
export function classifyTgSignIn(err: unknown): TgSignInOutcome {
  const e = (err ?? {}) as ErrLike;
  if (e.status === 403 && e.code === "telegram_not_linked") {
    const id = e.details?.telegram_user_id;
    return { kind: "not_linked", telegramUserId: id === undefined || id === null ? "" : String(id) };
  }
  if (e.status === 401) return { kind: "rejected" };
  return { kind: "unavailable" };
}
