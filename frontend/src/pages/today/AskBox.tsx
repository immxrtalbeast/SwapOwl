import { useState } from "react";
import { TextInput } from "@mantine/core";
import { IconSparkles } from "@tabler/icons-react";
import { api } from "../../api/client";
import { PrimaryButton } from "../../components";

/** Подсказки-вопросы: те же, что понимает бот (/ask). */
export const ASK_EXAMPLES = ["Что в стопе?", "Что истекает?", "Что заказать?", "Что предложить гостям?", "Сколько осталось лосося?", "Что запланировано?"] as const;

type State = { kind: "idle" } | { kind: "busy" } | { kind: "answer"; q: string; text: string } | { kind: "error"; message: string };

/** «Помощник»: вопрос своими словами. Ответ считает API по правилам, без модели; здесь — только ввод и показ. */
export function AskBox() {
  const [q, setQ] = useState("");
  const [state, setState] = useState<State>({ kind: "idle" });

  const ask = async (question: string) => {
    const text = question.trim();
    if (!text) return;
    setQ(text);
    setState({ kind: "busy" });
    try {
      const r = await api.ask(text);
      setState({ kind: "answer", q: text, text: r.answer });
    } catch (e) {
      setState({ kind: "error", message: e instanceof Error ? e.message : "Не удалось получить ответ" });
    }
  };

  return (
    <section
      aria-label="Помощник"
      data-testid="ask-box"
      className="so-glass"
      style={{ display: "grid", gap: 12, padding: "16px var(--card-pad)", marginBottom: 16, borderRadius: "var(--radius-card)" }}
    >
      <form
        onSubmit={(e) => { e.preventDefault(); void ask(q); }}
        style={{ display: "flex", gap: 12, flexWrap: "wrap", alignItems: "flex-end" }}
      >
        <TextInput
          aria-label="Вопрос помощнику"
          placeholder="Спросите словами: что в стопе? что истекает? сколько осталось лосося?"
          leftSection={<IconSparkles size={18} aria-hidden="true" />}
          maxLength={300}
          value={q}
          onChange={(e) => setQ(e.currentTarget.value)}
          style={{ flex: 1, minWidth: 240 }}
        />
        <PrimaryButton type="submit" disabled={state.kind === "busy" || !q.trim()}>
          {state.kind === "busy" ? "Думаю…" : "Спросить"}
        </PrimaryButton>
      </form>
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        {ASK_EXAMPLES.map((ex) => (
          <button
            key={ex}
            type="button"
            onClick={() => void ask(ex)}
            style={{ border: "1px solid var(--border-strong)", background: "var(--surface)", color: "var(--text)", borderRadius: "var(--radius-pill)", padding: "6px 12px", font: "var(--type-small)", cursor: "pointer" }}
          >
            {ex}
          </button>
        ))}
      </div>
      {state.kind === "answer" && (
        <div role="status" data-testid="ask-answer" style={{ display: "grid", gap: 4 }}>
          <span style={{ font: "var(--type-small)", color: "var(--text-muted)" }}>{state.q}</span>
          <p style={{ margin: 0, whiteSpace: "pre-line", font: "var(--type-body)" }}>{state.text}</p>
        </div>
      )}
      {state.kind === "error" && <p role="alert" style={{ margin: 0, color: "var(--urgency-critical-fg)" }}>{state.message}</p>}
    </section>
  );
}
