import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { useAuth } from "../../auth/AuthContext";
import { LoadingState } from "../../components/ScreenStates";
import { PrimaryButton } from "../../components/Buttons";
import { classifyTgSignIn, linkCodeErrorText, normalizeLinkInput } from "./signin";
import type { TgSignInOutcome } from "./signin";
import { getWebApp, loadTelegramSdk } from "./sdk";

type Phase = { kind: "working" } | { kind: "fallback" } | TgSignInOutcome;

const wrap = { minHeight: "100vh", display: "grid", placeItems: "center", padding: 16, boxSizing: "border-box", background: "var(--paper)" } as const;

/**
 * Вход в mini-app без пароля: подпись Telegram (`initData`) → сессия. Показывается вместо формы пароля, когда сессии нет
 * и страница открыта как #/tg. Нет подписи (не Telegram) или вход не включён — обычная форма пароля (`fallback`).
 */
export function TgSignIn({ fallback }: { fallback: ReactNode }) {
  const { telegramLogin, telegramLinkWithCode } = useAuth();
  const [code, setCode] = useState("");
  const [linkError, setLinkError] = useState<string | null>(null);
  const [linking, setLinking] = useState(false);

  async function linkByCode() {
    const normalized = normalizeLinkInput(code);
    if (!normalized) {
      setLinkError("Введите код из 8 знаков, например ABCD-EFGH.");
      return;
    }
    const initData = getWebApp()?.initData;
    if (!initData) {
      setLinkError("Откройте приложение из бота в Telegram.");
      return;
    }
    setLinking(true);
    setLinkError(null);
    try {
      await telegramLinkWithCode(initData, normalized);
    } catch (e) {
      setLinkError(linkCodeErrorText(e));
    } finally {
      setLinking(false);
    }
  }
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
        // Вход принят, но сессии нет (например, cookie не сохранилась): компонент остался — не вешаем «Входим…» навсегда.
        if (live) setPhase({ kind: "rejected" });
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
              В веб-версии откройте «Аккаунт» → «Привязать Telegram» и введите код сюда — права будут такие же, как в вебе.
            </p>
            <input
              aria-label="Код привязки"
              placeholder="ABCD-EFGH"
              autoCapitalize="characters"
              autoComplete="off"
              maxLength={12}
              value={code}
              onChange={(e) => setCode(e.currentTarget.value)}
              onKeyDown={(e) => { if (e.key === "Enter") void linkByCode(); }}
              style={{ height: 48, textAlign: "center", font: "700 22px/28px var(--font-display)", letterSpacing: 2, border: "2px solid var(--border-strong)", borderRadius: "var(--radius-control)", background: "var(--surface)", color: "var(--text)" }}
            />
            <PrimaryButton loading={linking} onClick={() => void linkByCode()}>Привязать</PrimaryButton>
            {linkError && <p role="alert" style={{ margin: 0, color: "var(--urgency-critical-fg)" }}>{linkError}</p>}
            <p style={{ margin: 0, color: "var(--text-muted)", font: "var(--type-small)" }}>
              Или передайте администратору ваш Telegram ID — он привяжет вас вручную.
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
