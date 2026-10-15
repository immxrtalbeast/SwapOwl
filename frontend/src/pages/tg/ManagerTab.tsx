import { useState } from "react";
import { NativeSelect, TextInput } from "@mantine/core";
import { IconEye, IconEyeOff, IconPin, IconPinnedOff, IconPlus } from "@tabler/icons-react";
import { api } from "../../api/client";
import { RoleGate } from "../../auth/AuthContext";
import { ErrorState, LoadingState, PrimaryButton, SecondaryButton } from "../../components";
import { formatRub } from "../../lib/format";
import { pickableDishes } from "../hot-offers/manager";
import { useHotOffers } from "../hot-offers/useHotOffers";
import { useRisk } from "../today/useToday";
import { batchesWord, todaySummary } from "./model";
import { haptic } from "./sdk";
import { TgCard, errorText, heading, listReset, muted, section, useBackButton, useMainButton } from "./ui";

/** Правка горячих: ошибка остаётся в строке, где нажали; успех и ошибка — тактильный отклик. */
function useEdit(after: () => void | Promise<void>) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
      haptic("success");
      await after();
    } catch (e) {
      haptic("error");
      setError(e instanceof Error ? e.message : "Не удалось сохранить");
    } finally {
      setBusy(false);
    }
  };
  return { busy, error, run };
}

const TAP = { height: 44 } as const;

/** «Менеджер»: «Сегодня» коротко и правка горячих — закрепить, скрыть, добавить. */
export function ManagerTab() {
  const [risk] = useRisk();
  const [hot, reload] = useHotOffers(null);
  const [adding, setAdding] = useState(false);
  const [dish, setDish] = useState("");
  const [note, setNote] = useState("");
  const edit = useEdit(reload);
  const add = useEdit(async () => { setDish(""); setNote(""); setAdding(false); await reload(); });
  useBackButton(adding ? () => setAdding(false) : null);
  const inline = useMainButton(adding ? (dish ? "Добавить в предложения" : null) : "Добавить блюдо", () => {
    if (!adding) setAdding(true);
    else if (dish) void add.run(() => api.setHotOverride(dish, "pin", note));
  });

  if (hot.kind === "loading") return <LoadingState rows={3} label="Загрузка" />;
  if (hot.kind === "error") return <ErrorState what="горячие предложения" reason={hot.message} onRetry={() => void reload()} />;

  const { data, menu } = hot;
  const options = pickableDishes(menu, data);
  const picks = data.manager_picks ?? [];
  const hidden = data.hidden ?? [];
  const sum = risk.kind === "ready" ? todaySummary(risk.alerts, risk.prices, menu) : null;

  if (adding)
    return (
      <form
        style={section}
        onSubmit={(e) => { e.preventDefault(); if (dish) void add.run(() => api.setHotOverride(dish, "pin", note)); }}
      >
        <h2 style={heading}>Добавить блюдо</h2>
        <NativeSelect
          label="Блюдо"
          data={[{ value: "", label: options.length ? "Выберите блюдо" : "Все доступные блюда уже в списке" }, ...options.map((d) => ({ value: d.id, label: d.name }))]}
          value={dish}
          onChange={(e) => setDish(e.currentTarget.value)}
        />
        <TextInput label="Подсказка для зала" placeholder="например, предлагать к обеду" maxLength={200} value={note} onChange={(e) => setNote(e.currentTarget.value)} />
        <p style={muted}>В списке только блюда, которые можно приготовить. Если блюдо уйдёт в стоп-лист, из предложений оно уйдёт само.</p>
        {add.error && <p role="alert" style={errorText}>{add.error}</p>}
        {inline && <PrimaryButton type="submit" style={{ height: 48 }} disabled={!dish || add.busy} leftSection={<IconPlus size={16} aria-hidden="true" />}>Добавить</PrimaryButton>}
      </form>
    );

  return (
    <div style={section}>
      <h2 style={heading}>Сегодня</h2>
      {risk.kind === "loading" && <p style={muted}>Считаем…</p>}
      {risk.kind === "error" && <p role="alert" style={errorText}>{risk.message}</p>}
      {sum && (
        <div data-testid="tg-today" style={{ display: "grid", gap: 8, gridTemplateColumns: "repeat(3, minmax(0, 1fr))" }}>
          <TgCard>
            <span style={muted}>Под риском</span>
            <strong style={{ fontSize: 18 }}>{formatRub(sum.atRiskMinor)}</strong>
            {sum.unpriced > 0 && <span style={muted}>+ {sum.unpriced} без цены</span>}
          </TgCard>
          <TgCard>
            <span style={muted}>Истекает</span>
            <strong style={{ fontSize: 18 }}>{batchesWord(sum.expiring)}</strong>
          </TgCard>
          <TgCard>
            <span style={muted}>В стопе</span>
            <strong style={{ fontSize: 18 }}>{sum.stopped}</strong>
          </TgCard>
        </div>
      )}

      <h2 style={{ ...heading, marginTop: 8 }}>Горячие предложения</h2>
      {data.offers.length === 0 && picks.length === 0 && <p style={muted}>Сегодня расчёт ничего не предлагает.</p>}
      <ul aria-label="Горячие предложения" style={listReset}>
        {data.offers.map((o) => (
          <li key={o.dish_id}>
            <TgCard accent={!!o.pinned} testid="tg-offer">
              <strong>{o.dish_name}</strong>
              {o.manager_note && <span style={muted}>{o.manager_note}</span>}
              <RoleGate min="operator">
                <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
                  {o.pinned ? (
                    <SecondaryButton style={TAP} disabled={edit.busy} leftSection={<IconPinnedOff size={16} aria-hidden="true" />} onClick={() => void edit.run(() => api.deleteHotOverride(o.dish_id))}>Открепить</SecondaryButton>
                  ) : (
                    <SecondaryButton style={TAP} disabled={edit.busy} leftSection={<IconPin size={16} aria-hidden="true" />} onClick={() => void edit.run(() => api.setHotOverride(o.dish_id, "pin"))}>Закрепить</SecondaryButton>
                  )}
                  <SecondaryButton style={TAP} disabled={edit.busy} leftSection={<IconEyeOff size={16} aria-hidden="true" />} onClick={() => void edit.run(() => api.setHotOverride(o.dish_id, "hide"))}>Скрыть</SecondaryButton>
                </div>
              </RoleGate>
            </TgCard>
          </li>
        ))}
        {picks.map((p) => (
          <li key={p.dish_id}>
            <TgCard accent testid="tg-pick">
              <strong>{p.dish_name}</strong>
              <span style={muted}>добавлено менеджером{p.note ? ` — ${p.note}` : ""}</span>
              <RoleGate min="operator">
                <div><SecondaryButton style={TAP} disabled={edit.busy} onClick={() => void edit.run(() => api.deleteHotOverride(p.dish_id))}>Убрать</SecondaryButton></div>
              </RoleGate>
            </TgCard>
          </li>
        ))}
      </ul>

      {hidden.length > 0 && (
        <>
          <h2 style={{ ...heading, marginTop: 8 }}>Скрыто</h2>
          <ul aria-label="Скрыто менеджером" style={listReset}>
            {hidden.map((h) => (
              <li key={h.dish_id}>
                <TgCard>
                  <strong>{h.dish_name}</strong>
                  {h.in_offers && <span style={muted}>расчёт его предлагает</span>}
                  <RoleGate min="operator">
                    <div><SecondaryButton style={TAP} disabled={edit.busy} leftSection={<IconEye size={16} aria-hidden="true" />} onClick={() => void edit.run(() => api.deleteHotOverride(h.dish_id))}>Вернуть</SecondaryButton></div>
                  </RoleGate>
                </TgCard>
              </li>
            ))}
          </ul>
        </>
      )}
      {edit.error && <p role="alert" style={errorText}>{edit.error}</p>}
      <RoleGate min="operator">
        {inline && <PrimaryButton style={{ height: 48 }} onClick={() => setAdding(true)} leftSection={<IconPlus size={16} aria-hidden="true" />}>Добавить блюдо</PrimaryButton>}
      </RoleGate>
    </div>
  );
}
