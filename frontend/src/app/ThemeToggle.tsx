import { IconMoon, IconSun } from "@tabler/icons-react";
import { useMantineColorScheme } from "@mantine/core";

/** Светлая ↔ тёмная тема. Выбор помнит браузер (Mantine хранит его в localStorage); по умолчанию светлая. */
export function ThemeToggle() {
  const { colorScheme, setColorScheme } = useMantineColorScheme();
  const dark = colorScheme === "dark";
  const Icon = dark ? IconSun : IconMoon;
  return (
    <button
      type="button"
      data-testid="theme-toggle"
      aria-label={dark ? "Включить светлую тему" : "Включить тёмную тему"}
      aria-pressed={dark}
      title={dark ? "Светлая тема" : "Тёмная тема"}
      onClick={() => setColorScheme(dark ? "light" : "dark")}
      style={{ display: "inline-flex", alignItems: "center", justifyContent: "center", width: 36, height: 36, borderRadius: "var(--radius-control)", border: "1px solid var(--border-strong)", background: "var(--surface)", color: "var(--text)", cursor: "pointer" }}
    >
      <Icon size={18} aria-hidden="true" />
    </button>
  );
}
