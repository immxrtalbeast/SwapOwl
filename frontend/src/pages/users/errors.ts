import { ApiError } from "../../api/types";

/** Человекочитаемая причина отказа при удалении пользователя. */
export function deleteUserError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 409) return "Нельзя удалить последнего администратора: сначала создайте другого.";
    if (e.status === 403) return "Недостаточно прав: экран доступен только администратору.";
    if (e.status === 404) return "Пользователь уже удалён.";
    return e.message;
  }
  return e instanceof Error ? e.message : "Не удалось удалить пользователя";
}

export function createUserError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 409) return "Пользователь с таким логином уже есть.";
    if (e.status === 400) return "Проверьте поля: логин не пустой, пароль от 8 до 72 байт.";
    return e.message;
  }
  return e instanceof Error ? e.message : "Не удалось создать пользователя";
}
