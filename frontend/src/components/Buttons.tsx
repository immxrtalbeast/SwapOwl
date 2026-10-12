import { Button } from "@mantine/core";
import type { ButtonProps } from "@mantine/core";
import type { ComponentPropsWithoutRef } from "react";

type Props = ButtonProps & Omit<ComponentPropsWithoutRef<"button">, keyof ButtonProps>;

const base = { font: "var(--type-button)", borderRadius: "var(--radius-control)", height: "var(--control-h)" } as const;

/** Основная: заливка --primary, белый текст. */
export function PrimaryButton(props: Props) {
  return (
    <Button
      {...props}
      variant="filled"
      styles={{ root: { ...base, background: "var(--primary)", color: "var(--on-primary)" }, label: { font: "var(--type-button)" } }}
    />
  );
}

/**
 * Вторичная: контур 2 px --brand-ink, прозрачный фон.
 * Как переключатель (aria-pressed="true", «Сутки / 7 дней / 30 дней», «Алгоритм A / B») выбранная
 * заливается --primary с белым текстом (design-system.md); остальные остаются контурными.
 */
export function SecondaryButton(props: Props) {
  const pressed = props["aria-pressed"] === true || props["aria-pressed"] === "true";
  const look = pressed
    ? { border: "2px solid var(--primary)", color: "var(--on-primary)", background: "var(--primary)" }
    : { border: "2px solid var(--brand-ink)", color: "var(--brand-ink)", background: "transparent" };
  return (
    <Button
      {...props}
      variant="outline"
      styles={{ root: { ...base, ...look }, label: { font: "var(--type-button)" } }}
    />
  );
}

/** Текстовая: --type-button цвета --brand-ink (на --paper --primary даёт 4,2 : 1), без фона. */
export function TextButton(props: Props) {
  return (
    <Button
      {...props}
      variant="subtle"
      styles={{ root: { ...base, color: "var(--brand-ink)", background: "transparent", paddingInline: 8 }, label: { font: "var(--type-button)" } }}
    />
  );
}
