import { TOKENS } from "./tokens";

/**
 * Тёмная тема: переопределяет только цвета, размеры и шрифты общие. Палитра — тот же зелёный бренд на графитовом
 * фоне с зелёным оттенком. Пары «текст на фоне» держат 4,5 : 1 (проверяет dark.test.ts).
 * Светлая тема остаётся основной и стартовой — демонстрация одинаковая у всех; тёмная включается переключателем в шапке.
 */
export const DARK_TOKENS: Record<string, string> = {
  "--brand": "#2fd08a",
  "--brand-strong": "#3fe09a",
  "--brand-tint": "#14382a",
  "--brand-ink": "#6fe3ac",
  "--brand-grid": "#1a2a22",
  "--paper": "#0e1411",
  "--surface-page": "#0e1411",
  "--surface": "#171f1b",
  "--surface-sunken": "#121916",
  "--surface-hover": "#1f2924",
  "--skeleton": "#26312b",
  "--overlay": "rgba(0,0,0,0.62)",
  "--border": "#2c3832",
  "--border-strong": "#7d8a83",
  "--text": "#e8eeea",
  "--text-muted": "#b2beb7",
  "--text-subtle": "#98a59e",
  "--primary": "#3ccf8e",
  "--primary-hover": "#5adba3",
  "--primary-subtle": "#14382a",
  "--on-primary": "#06281a",
  "--focus-ring": "#6fe3ac",
  "--row-changed": "#1c3a2c",
  "--border-cell": "#25302a",
  "--glass-bg": "rgba(23,31,27,0.62)",
  "--glass-bg-strong": "rgba(23,31,27,0.82)",
  "--glass-edge": "rgba(255,255,255,0.09)",
  "--glass-highlight": "rgba(255,255,255,0.06)",
  "--glass-shadow": "0 10px 30px rgba(0,0,0,0.38),0 1px 2px rgba(0,0,0,0.3)",
  "--glass-nav": "linear-gradient(165deg,#145c3c 0%,#0d4128 55%,#082c1c 100%)",
  "--page-glow": "radial-gradient(55% 45% at 0% 0%,rgba(31,180,110,0.14) 0%,transparent 70%),radial-gradient(45% 40% at 100% 0%,rgba(80,140,220,0.10) 0%,transparent 70%),radial-gradient(60% 50% at 80% 100%,rgba(220,160,60,0.08) 0%,transparent 70%)",
  "--urgency-ok-fg": "#6fd89b",
  "--urgency-ok-bg": "#1f2a25",
  "--urgency-warn-fg": "#f5c26b",
  "--urgency-warn-bg": "#3a2a0c",
  "--urgency-warn-row": "#2b2210",
  "--urgency-risk-fg": "#ff9d78",
  "--urgency-risk-bg": "#3a1c10",
  "--urgency-risk-row": "#2e1a12",
  "--urgency-critical-fg": "#ff8a9a",
  "--urgency-critical-bg": "#3d141b",
  "--urgency-neutral-fg": "#b2beb7",
  "--urgency-neutral-bg": "#232b27",
  "--urgency-info-fg": "#9bbcff",
  "--urgency-info-bg": "#16254a",
  "--shadow-sm": "0 1px 2px rgba(0,0,0,0.4),0 1px 1px rgba(0,0,0,0.3)",
  "--shadow-md": "0 4px 12px rgba(0,0,0,0.42),0 1px 3px rgba(0,0,0,0.3)",
  "--shadow-lg": "0 12px 32px rgba(0,0,0,0.5),0 2px 6px rgba(0,0,0,0.35)",
  "--shadow-top": "0 2px 10px rgba(0,0,0,0.35)",
};

/** Свои переменные Mantine в тёмной теме: поля, выпадающие списки и кнопки в той же палитре, а не в серой Mantine. */
const MANTINE_DARK: Record<string, string> = {
  "--mantine-color-body": DARK_TOKENS["--paper"],
  "--mantine-color-text": DARK_TOKENS["--text"],
  "--mantine-color-dimmed": DARK_TOKENS["--text-muted"],
  "--mantine-color-placeholder": DARK_TOKENS["--text-subtle"],
  "--mantine-color-default": DARK_TOKENS["--surface"],
  "--mantine-color-default-hover": DARK_TOKENS["--surface-hover"],
  "--mantine-color-default-color": DARK_TOKENS["--text"],
  "--mantine-color-default-border": DARK_TOKENS["--border-strong"],
  "--mantine-color-error": DARK_TOKENS["--urgency-critical-fg"],
};

/** Раскладывает токены по :root через Mantine: светлая — основная, тёмная — поверх неё. */
export const cssVariablesResolver = () => ({
  variables: {},
  light: TOKENS,
  dark: { ...TOKENS, ...DARK_TOKENS, ...MANTINE_DARK },
});

/**
 * Печать всегда светлая: отчёт смены и этикетки уходят на белую бумагу, а светлый текст тёмной темы на ней не читается
 * (браузеры не печатают фон). Те же токены, что у светлой темы, но только внутри @media print.
 */
export const PRINT_LIGHT_CSS = `@media print { html:root[data-mantine-color-scheme="dark"] { color-scheme: light; ${Object.keys(DARK_TOKENS)
  .map((k) => `${k}: ${TOKENS[k]};`)
  .join(" ")} } }`;
