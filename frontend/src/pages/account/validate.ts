import { ApiError } from "../../api/types";

const MIN_BYTES = 8;
const MAX_BYTES = 72; // предел bcrypt, как на сервере

const bytes = (s: string) => new TextEncoder().encode(s).length;

/** Проверка формы смены пароля до отправки; возвращает текст ошибки или null. */
export function validatePasswordChange(oldPw: string, newPw: string, repeat: string): string | null {
  if (!oldPw) return "Введите текущий пароль.";
  const n = bytes(newPw);
  if (n < MIN_BYTES || n > MAX_BYTES) return "Новый пароль — от 8 до 72 байт.";
  if (newPw === oldPw) return "Новый пароль должен отличаться от текущего.";
  if (newPw !== repeat) return "Новый пароль и повтор не совпадают.";
  return null;
}

/** Человекочитаемая причина отказа сервера при смене пароля. */
export function changePasswordError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 401) return "Текущий пароль неверен.";
    if (e.status === 429 || e.code === "too_many_attempts") return "Слишком много неудачных попыток. Попробуйте через 15 минут.";
    if (e.status === 403) return "Пароль меняет только вошедший пользователь; на открытом стенде и для ключей интеграции его нет.";
    if (e.status === 400) return "Проверьте поля: новый пароль от 8 до 72 байт и не совпадает с текущим.";
    return e.message;
  }
  return e instanceof Error ? e.message : "Не удалось сменить пароль";
}
