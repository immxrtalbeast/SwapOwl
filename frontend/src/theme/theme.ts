import { createTheme } from "@mantine/core";
import type { MantineColorsTuple } from "@mantine/core";

// Фирменный зелёный как шкала Mantine: индексы 6–7 — токены --primary / --primary-hover.
// Сами цвета компонентов берутся из CSS-переменных токенов (tokens.ts), а не из этой шкалы.
const brand: MantineColorsTuple = [
  "var(--brand-tint)", "var(--brand-tint)", "var(--brand-grid)", "var(--brand)", "var(--brand)",
  "var(--brand-strong)", "var(--primary)", "var(--primary-hover)", "var(--brand-ink)", "var(--brand-ink)",
];

export const theme = createTheme({
  primaryColor: "brand",
  primaryShade: 6,
  colors: { brand },
  fontFamily: "var(--font-body)",
  fontFamilyMonospace: "var(--font-display)",
  headings: { fontFamily: "var(--font-body)", fontWeight: "700" },
  defaultRadius: "md",
  radius: { xs: "4px", sm: "6px", md: "8px", lg: "12px", xl: "999px" },
  black: "var(--text)",
  breakpoints: { xs: "36em", sm: "48em", md: "62em", lg: "75em", xl: "88em" },
  focusRing: "auto",
  components: {
    Button: { defaultProps: { radius: "md" } },
    Card: { defaultProps: { radius: "lg", withBorder: true } },
  },
});
