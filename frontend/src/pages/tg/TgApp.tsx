import { useEffect, useState } from "react";
import { IconChefHat, IconClipboardCheck, IconChartBar } from "@tabler/icons-react";
import { useMantineColorScheme } from "@mantine/core";
import { ThemeToggle } from "../../app/ThemeToggle";
import { ConnectionProvider, useConnection } from "../../app/ConnectionContext";
import { events } from "../../api/events";
import { OfflineBanner } from "../../components";
import { HallTab } from "./HallTab";
import { ManagerTab } from "./ManagerTab";
import { ReceivingTab } from "./ReceivingTab";
import { TG_TABS, overlayAllowed, tgHash, tgSchemeToApply, tgTabFromHash, themeToVars } from "./model";
import type { Scheme, TgTab } from "./model";
import { getWebApp, loadTelegramSdk } from "./sdk";
import { useBackButton } from "./ui";

const MANUAL_KEY = "so-tg-theme-manual";

/** Выбор темы кнопкой в мини-приложении помнит браузер; без хранилища работает следование теме Telegram. */
function readManual(): Scheme | null {
  try {
    const v = window.localStorage.getItem(MANUAL_KEY);
    return v === "light" || v === "dark" ? v : null;
  } catch {
    return null;
  }
}
function writeManual(v: Scheme): void {
  try {
    window.localStorage.setItem(MANUAL_KEY, v);
  } catch {
    /* хранилища нет — тема останется до закрытия */
  }
}

const ICONS = { hall: IconChefHat, receiving: IconClipboardCheck, manager: IconChartBar } as const;

function Offline() {
  const conn = useConnection();
  if (conn !== "offline") return null;
  return <OfflineBanner onRetry={() => { events.stop(); events.start(); }} />;
}

function Body() {
  const [tab, setTab] = useState<TgTab>(() => tgTabFromHash(window.location.hash));
  const [theme, setTheme] = useState(() => themeToVars(getWebApp()?.themeParams));
  const { colorScheme, setColorScheme } = useMantineColorScheme();
  const current: Scheme = colorScheme === "dark" ? "dark" : "light";
  const [tgScheme, setTgScheme] = useState<string | undefined>(() => getWebApp()?.colorScheme);
  // SDK грузится динамически: после onload перечитываем getWebApp() (кнопки и тема подхватываются перерисовкой).
  const [sdkReady, setSdkReady] = useState(() => getWebApp() !== null);
  useEffect(() => {
    let live = true;
    void loadTelegramSdk().then(() => { if (live) setSdkReady(getWebApp() !== null); });
    return () => { live = false; };
  }, []);

  useEffect(() => {
    const on = () => setTab(tgTabFromHash(window.location.hash));
    window.addEventListener("hashchange", on);
    return () => window.removeEventListener("hashchange", on);
  }, []);

  useEffect(() => {
    const app = getWebApp();
    if (!app) return;
    app.ready();
    app.expand();
    const onTheme = () => {
      setTheme(themeToVars(app.themeParams));
      setTgScheme(app.colorScheme);
      // Схема Telegram — по умолчанию; выбор в мини-приложении главнее.
      const next = tgSchemeToApply(app.colorScheme, readManual());
      if (next) setColorScheme(next);
    };
    app.onEvent?.("themeChanged", onTheme);
    onTheme();
    return () => app.offEvent?.("themeChanged", onTheme);
    // setColorScheme стабилен для провайдера; перечитываем только при готовности SDK.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sdkReady]);

  // «Назад» Telegram возвращает на первую вкладку; вложенные экраны вкладок перехватывают её раньше (useBackButton в них).
  useBackButton(tab !== "hall" ? () => { window.location.hash = tgHash("hall"); } : null, 0);

  return (
    <div style={{ ...(overlayAllowed(tgScheme, current) ? theme : {}), minHeight: "100vh", display: "flex", flexDirection: "column", background: "var(--paper)", color: "var(--text)", overflowX: "hidden" }}>
      <main style={{ flex: 1, minWidth: 0, boxSizing: "border-box", padding: "16px 12px 88px", width: "100%", maxWidth: 480, margin: "0 auto" }}>
        <div style={{ display: "flex", justifyContent: "flex-end", marginBottom: 8 }}>
          <ThemeToggle onChange={writeManual} />
        </div>
        <Offline />
        {tab === "hall" && <HallTab />}
        {tab === "receiving" && <ReceivingTab />}
        {tab === "manager" && <ManagerTab />}
      </main>
      <nav
        aria-label="Разделы"
        style={{ position: "fixed", left: 0, right: 0, bottom: 0, zIndex: 100, display: "grid", gridTemplateColumns: "repeat(3, 1fr)", background: "var(--surface)", borderTop: "1px solid var(--border)", paddingBottom: "env(safe-area-inset-bottom)" }}
      >
        {TG_TABS.map((t) => {
          const Icon = ICONS[t.id];
          const on = t.id === tab;
          return (
            <a
              key={t.id}
              href={tgHash(t.id)}
              aria-current={on ? "page" : undefined}
              style={{ display: "grid", justifyItems: "center", gap: 2, padding: "8px 4px", minHeight: 52, textDecoration: "none", font: "var(--type-small)", color: on ? "var(--primary)" : "var(--text-muted)", fontWeight: on ? 700 : 400 }}
            >
              <Icon size={22} aria-hidden="true" />
              {t.title}
            </a>
          );
        })}
      </nav>
    </div>
  );
}

/** Мини-приложение Telegram (#/tg): без боковой панели и шапки, нижняя панель из трёх вкладок. Вне Telegram работает так же. */
export function TgApp() {
  return (
    <ConnectionProvider>
      <Body />
    </ConnectionProvider>
  );
}
