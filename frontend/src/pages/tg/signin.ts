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
