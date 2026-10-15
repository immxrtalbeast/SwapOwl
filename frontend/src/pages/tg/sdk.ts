/** Минимальное описание Telegram WebApp SDK: только то, чем пользуется мини-приложение. SDK необязателен. */
export interface TgButton {
  show(): void;
  hide(): void;
  onClick(cb: () => void): void;
  offClick(cb: () => void): void;
}

export interface TgMainButton extends TgButton {
  setText(text: string): void;
  enable(): void;
  disable(): void;
}

export interface TgWebApp {
  ready(): void;
  expand(): void;
  themeParams?: Record<string, string>;
  onEvent?(name: string, cb: () => void): void;
  offEvent?(name: string, cb: () => void): void;
  BackButton?: TgButton;
  MainButton?: TgMainButton;
  HapticFeedback?: {
    notificationOccurred(type: "success" | "error" | "warning"): void;
    impactOccurred?(style: "light" | "medium" | "heavy"): void;
  };
}

/** WebApp, если страница открыта в Telegram; вне Telegram (или без скрипта) — null. */
export function getWebApp(): TgWebApp | null {
  const w = (window as unknown as { Telegram?: { WebApp?: TgWebApp } }).Telegram?.WebApp;
  return w && typeof w.ready === "function" ? w : null;
}

/** Тактильный отклик на успех или ошибку; вне Telegram ничего не делает. */
export function haptic(kind: "success" | "error"): void {
  try {
    getWebApp()?.HapticFeedback?.notificationOccurred(kind);
  } catch {
    /* отклик не критичен */
  }
}
