import type { ReactNode } from "react";
import { Skeleton } from "@mantine/core";
import { IconAlertCircle, IconInbox, IconLock, IconWifiOff } from "@tabler/icons-react";
import { PrimaryButton, SecondaryButton } from "./Buttons";

// Пять состояний экрана (.ai/frontend-prompt.md, «Состояния экранов»): загрузка, пусто, ошибка, нет связи, нет прав.

/** Загрузка: скелетоны в форме будущего контента. Спиннера на весь экран нет. */
export function LoadingState({ rows = 5, label = "Загрузка" }: { rows?: number; label?: string }) {
  return (
    <div aria-busy="true" aria-label={label} role="status" style={{ display: "grid", gap: 8 }}>
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} height={44} radius={6} style={{ background: "var(--skeleton)" }} />
      ))}
    </div>
  );
}

const centered = { display: "grid", justifyItems: "center", gap: 8, textAlign: "center", padding: "48px 16px" } as const;

interface EmptyProps {
  title: string;
  text?: string;
  /** Действие. Если у роли нет права — не передавать, кнопка не покажется. */
  action?: { label: string; onClick: () => void };
}

/** Пусто: иконка, заголовок, одна фраза и действие. */
export function EmptyState({ title, text, action }: EmptyProps) {
  return (
    <div style={centered}>
      <IconInbox size={40} color="var(--text-subtle)" aria-hidden="true" />
      <h2 style={{ margin: 0, font: "var(--type-section)", textTransform: "uppercase" }}>{title}</h2>
      {text && <p style={{ margin: 0, color: "var(--text-muted)" }}>{text}</p>}
      {action && <PrimaryButton onClick={action.onClick}>{action.label}</PrimaryButton>}
    </div>
  );
}

const OFFLINE_TEXT = "Нет связи с сервером — данные обновятся сами";

/** Сырые сообщения браузера при обрыве связи («Failed to fetch») → человеческий текст. */
export function humanizeReason(reason: string | undefined): string | undefined {
  if (reason && /failed to fetch|networkerror|load failed|fetch failed|network request failed/i.test(reason)) return OFFLINE_TEXT;
  return reason;
}

interface ErrorProps {
  /** Что не загрузилось: «поставки», «склад». */
  what: string;
  /** Причина человеческим языком. */
  reason?: string;
  requestId?: string | null;
  onRetry?: () => void;
}

/** Ошибка: «Не удалось загрузить …», причина, request_id мелким моноширинным и «Повторить». */
export function ErrorState({ what, reason: rawReason, requestId, onRetry }: ErrorProps) {
  const reason = humanizeReason(rawReason);
  return (
    <div role="alert" style={centered}>
      <IconAlertCircle size={40} color="var(--urgency-critical-fg)" aria-hidden="true" />
      <h2 style={{ margin: 0, font: "var(--type-h2)" }}>Не удалось загрузить {what}</h2>
      {reason && <p style={{ margin: 0, color: "var(--text-muted)" }}>{reason}</p>}
      {requestId && <code style={{ font: "var(--type-num)", color: "var(--text-subtle)" }}>request_id: {requestId}</code>}
      {onRetry && <SecondaryButton onClick={onRetry}>Повторить</SecondaryButton>}
    </div>
  );
}

/** Нет связи: плашка над контентом, данные остаются на экране. */
export function OfflineBanner({ onRetry }: { onRetry?: () => void }) {
  return (
    <div
      role="alert"
      style={{
        display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap", padding: "8px 16px", marginBottom: 16,
        borderRadius: 8, background: "var(--urgency-critical-bg)", color: "var(--urgency-critical-fg)", font: "var(--type-body-strong)",
      }}
    >
      <IconWifiOff size={18} aria-hidden="true" />
      <span>{OFFLINE_TEXT}</span>
      {onRetry && <SecondaryButton size="compact-sm" onClick={onRetry}>Повторить сейчас</SecondaryButton>}
    </div>
  );
}

/** Метка рядом с заголовком, пока нет связи: «Могли устареть · 19:20». */
export function StaleMark({ time }: { time: string }) {
  return (
    <span
      title={`Данные могли устареть — последнее обновление в ${time} по времени стенда`}
      style={{ padding: "2px 8px", borderRadius: 4, background: "var(--urgency-warn-bg)", color: "var(--urgency-warn-fg)", font: "var(--type-caption)", textTransform: "uppercase" }}
    >
      Могли устареть · {time}
    </span>
  );
}

interface ForbiddenProps {
  roleLabel: string;
  needLabel: string;
  onHome?: () => void;
}

/** Нет прав (403). 401 — не здесь: переход на экран входа делает AuthGate. */
export function ForbiddenState({ roleLabel, needLabel, onHome }: ForbiddenProps) {
  return (
    <div role="alert" style={centered}>
      <IconLock size={40} color="var(--text-subtle)" aria-hidden="true" />
      <h2 style={{ margin: 0, font: "var(--type-section)", textTransform: "uppercase" }}>Недостаточно прав</h2>
      <p style={{ margin: 0, color: "var(--text-muted)" }}>
        Ваша роль — {roleLabel}. Этот раздел доступен роли «{needLabel}» и выше.
      </p>
      {onHome && <PrimaryButton onClick={onHome}>На «Склад»</PrimaryButton>}
    </div>
  );
}

/** Рамка экрана: заголовок и метка устаревших данных. */
export function ScreenTitle({ children, stale }: { children: ReactNode; stale?: string | null }) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap", marginBottom: 16 }}>
      <h1 style={{ margin: 0, font: "var(--type-h1)" }}>{children}</h1>
      {stale && <StaleMark time={stale} />}
    </div>
  );
}

export const ScreenStates = { Loading: LoadingState, Empty: EmptyState, Error: ErrorState, Offline: OfflineBanner, Forbidden: ForbiddenState };
