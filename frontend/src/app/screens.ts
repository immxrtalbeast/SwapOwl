import type { Role } from "../auth/roles";

export interface Screen {
  /** Минимальная роль для пункта навигации; без неё пункт виден всем. */
  minRole?: Role;
  id: string;
  title: string;
  /** Что появится на экране-заглушке; у готовых экранов не используется. */
  soon?: string;
  /** Пункт внизу боковой панели (аккаунт, пользователи). */
  bottom?: boolean;
  /** Пункт виден, только если задан VITE_HUB_URL или hub отвечает (экран «Сеть»). */
  needsHub?: boolean;
}

// Порядок — по .ai/frontend-prompt.md, «Боковая панель».
export const SCREENS: Screen[] = [
  { id: "today", title: "Сегодня" },
  { id: "stock", title: "Склад" },
  { id: "receiving", title: "Приёмка" },
  { id: "menu", title: "Меню и стоп-лист" },
  { id: "hot-offers", title: "Горячие предложения", soon: "блюда, которые стоит продвигать, чтобы не списывать продукты" },
  { id: "ladder", title: "Лестница спасения", soon: "что делать с каждой партией под риском: продать, скидка, заготовка, передать" },
  { id: "what-if", title: "Что если", soon: "прикинуть эффект скидки или меньшего заказа на списания" },
  { id: "hall", title: "Для зала" },
  { id: "supply", title: "Поставки", soon: "предложения по закупке" },
  { id: "orders", title: "Заказы" },
  { id: "alerts", title: "Предупреждения", soon: "лента истекающих и списанных партий" },
  { id: "money", title: "Деньги", soon: "потери от списаний и сэкономленные суммы" },
  { id: "shift-report", title: "Отчёт смены" },
  { id: "analytics", title: "Аналитика", soon: "сводки по списаниям и продажам" },
  { id: "network", title: "Сеть", needsHub: true },
  { id: "demo", title: "Демо-панель", soon: "панель управления временем стенда" },
  { id: "account", title: "Аккаунт", bottom: true },
  { id: "users", title: "Пользователи и ключи", minRole: "admin", soon: "учётные записи и ключи доступа", bottom: true },
];

/** Стартовый экран после входа и при пустом хэше — «Сегодня»: показ начинается с него (e2e открывают экраны явными хэшами). */
export const DEFAULT_SCREEN = "today";

export function screenFromHash(hash: string): string {
  const id = hash.replace(/^#\/?/, "").split("?")[0];
  return SCREENS.some((s) => s.id === id) ? id : DEFAULT_SCREEN;
}
