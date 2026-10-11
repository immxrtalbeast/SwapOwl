import type { Role } from "../auth/roles";

export interface Screen {
  /** Минимальная роль для пункта навигации; без неё пункт виден всем. */
  minRole?: Role;
  id: string;
  title: string;
  /** Что появится на экране-заглушке; у готовых экранов не используется. */
  soon?: string;
}

export const SCREENS: Screen[] = [
  { id: "stock", title: "Склад" },
  { id: "receiving", title: "Приёмка" },
  { id: "menu", title: "Меню и стоп-лист" },
  { id: "orders", title: "Заказы" },
  { id: "alerts", title: "Предупреждения", soon: "лента истекающих и списанных партий" },
  { id: "supply", title: "Поставки", soon: "предложения по закупке" },
  { id: "hot-offers", title: "Горячие предложения", soon: "блюда, которые стоит продвигать, чтобы не списывать продукты" },
  { id: "money", title: "Деньги", soon: "потери от списаний и сэкономленные суммы" },
  { id: "demo", title: "Демо", soon: "панель управления временем стенда" },
  { id: "analytics", title: "Аналитика", soon: "сводки по списаниям и продажам" },
  { id: "account", title: "Аккаунт" },
  { id: "users", title: "Пользователи и ключи", minRole: "admin", soon: "учётные записи и ключи доступа" },
];

export function screenFromHash(hash: string): string {
  const id = hash.replace(/^#\/?/, "").split("?")[0];
  return SCREENS.some((s) => s.id === id) ? id : SCREENS[0].id;
}
