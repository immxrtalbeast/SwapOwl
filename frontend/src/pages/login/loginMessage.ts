import { ApiError } from "../../api/types";

export function loginErrorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 401) return "Неверный логин или пароль.";
    if (e.code === "too_many_attempts" || e.status === 429) {
      return "Слишком много неудачных попыток. Попробуйте через 15 минут.";
    }
    if (e.status === 400) return "Введите логин и пароль.";
  }
  return "Не удалось войти: сервер не ответил. Повторите попытку.";
}
