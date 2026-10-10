import { statusView } from "./statusView";

// Бейдж статуса партии. Смысл передаётся текстом, а не только цветом.
export function BatchStatus({ status }: { status: string }) {
  const v = statusView(status);
  return (
    <span
      data-status={status}
      style={{ color: v.color, background: v.background, padding: "2px 8px", borderRadius: 8, fontSize: "0.85em", whiteSpace: "nowrap" }}
    >
      {v.label}
    </span>
  );
}
