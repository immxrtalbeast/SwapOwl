import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { useAuth } from "../../auth/AuthContext";
import { LoadingState } from "../../components/ScreenStates";
import { PrimaryButton } from "../../components/Buttons";
import { classifyTgSignIn } from "./signin";
import type { TgSignInOutcome } from "./signin";
import { getWebApp, loadTelegramSdk } from "./sdk";

type Phase = { kind: "working" } | { kind: "fallback" } | TgSignInOutcome;

const wrap = { minHeight: "100vh", display: "grid", placeItems: "center", padding: 16, boxSizing: "border-box", background: "var(--paper)" } as const;

/**
 * Вход в mini-app без пароля: подпись Telegram (`initData`) → сессия. Показывается вместо формы пароля, когда сессии нет
 * и страница открыта как #/tg. Нет подписи (не Telegram) или вход не включён — обычная форма пароля (`fallback`).
 */
export function TgSignIn({ fallback }: { fallback: ReactNode }) {
  const { telegramLogin } = useAuth();
  const [phase, setPhase] = useState<Phase>({ kind: "working" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    setPhase({ kind: "working" });
    void (async () => {
      await loadTelegramSdk();
      const initData = getWebApp()?.initData;
      if (!initData) {
        if (live) setPhase({ kind: "fallback" });
        return;
      }
      try {
        await telegramLogin(initData);
      } catch (e) {
        if (live) {
          const o = classifyTgSignIn(e);
          setPhase(o.kind === "unavailable" ? { kind: "fallback" } : o);
        }
      }
    })();
    return () => { live = false; };
  }, [attempt, telegramLogin]);

  if (phase.kind === "fallback") return <>{fallback}</>;
  if (phase.kind === "working") return <div style={{ padding: 24 }}><LoadingState label="Входим через Telegram" /></div>;

  return (
    <main style={wrap}>
      <div role="alert" style={{ maxWidth: 360, display: "grid", gap: 12, textAlign: "center" }}>
        {phase.kind === "not_linked" ? (
          <>
            <h1 style={{ margin: 0, font: "var(--type-h2)" }}>Аккаунт не привязан</h1>
            <p style={{ margin: 0, color: "var(--text-muted)" }}>
              Передайте администратору ваш Telegram ID — он привяжет вас к учётной записи SwapOwl.
            </p>
            {phase.telegramUserId && (
              <code className="so-num" style={{ font: "700 22px/28px var(--font-display)", userSelect: "all" }}>{phase.telegramUserId}</code>
            )}
          </>
        ) : (
          <>
            <h1 style={{ margin: 0, font: "var(--type-h2)" }}>Не удалось войти</h1>
            <p style={{ margin: 0, color: "var(--text-muted)" }}>Telegram не подтвердил вход. Закройте приложение и откройте его из бота заново.</p>
          </>
        )}
        <PrimaryButton onClick={() => setAttempt((n) => n + 1)}>Проверить снова</PrimaryButton>
      </div>
    </main>
  );
}
