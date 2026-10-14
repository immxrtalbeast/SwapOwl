import { useState } from "react";
import { NativeSelect, TextInput } from "@mantine/core";
import { IconPin, IconPinnedOff, IconEyeOff, IconEye, IconPlus } from "@tabler/icons-react";
import { api } from "../../api/client";
import type { DishServings, HotOffer, HotOffersResponse } from "../../api/types";
import { RoleGate } from "../../auth/AuthContext";
import { Card, SecondaryButton, PrimaryButton } from "../../components";
import { pickableDishes } from "./manager";

/** Ошибку правки показываем в той строке, где нажали. */
function useAction(after: () => void | Promise<void>) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
      await after();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Не удалось сохранить");
    } finally {
      setBusy(false);
    }
  };
  return { busy, error, run };
}

/** Кнопки менеджера на карточке предложения: закрепить или открепить, скрыть. Только operator и выше. */
export function OfferActions({ offer, onChanged }: { offer: HotOffer; onChanged: () => void | Promise<void> }) {
  const { busy, error, run } = useAction(onChanged);
  return (
    <RoleGate min="operator">
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        {offer.pinned ? (
          <SecondaryButton size="xs" disabled={busy} leftSection={<IconPinnedOff size={14} aria-hidden="true" />} onClick={() => void run(() => api.deleteHotOverride(offer.dish_id))}>
            Открепить
          </SecondaryButton>
        ) : (
          <SecondaryButton size="xs" disabled={busy} leftSection={<IconPin size={14} aria-hidden="true" />} onClick={() => void run(() => api.setHotOverride(offer.dish_id, "pin"))}>
            Закрепить
          </SecondaryButton>
        )}
        <SecondaryButton size="xs" disabled={busy} leftSection={<IconEyeOff size={14} aria-hidden="true" />} onClick={() => void run(() => api.setHotOverride(offer.dish_id, "hide"))}>
          Скрыть
        </SecondaryButton>
      </div>
      {error && <span role="alert" style={{ color: "var(--urgency-critical-fg)", font: "var(--type-small)" }}>{error}</span>}
    </RoleGate>
  );
}

/** Блок менеджера под списком: добавленные вручную, скрытые и форма «Добавить блюдо». */
export function ManagerPanel({ data, menu, onChanged }: { data: HotOffersResponse; menu: readonly DishServings[]; onChanged: () => void | Promise<void> }) {
  const picks = data.manager_picks ?? [];
  const hidden = data.hidden ?? [];
  const options = pickableDishes(menu, data);
  const [dish, setDish] = useState("");
  const [note, setNote] = useState("");
  const add = useAction(async () => { setDish(""); setNote(""); await onChanged(); });
  const row = useAction(onChanged);

  return (
    <div style={{ display: "grid", gap: 16, marginTop: 24 }}>
      {picks.length > 0 && (
        <Card title="Добавлено менеджером">
          <ul aria-label="Добавлено менеджером" style={{ margin: 0, paddingLeft: 0, listStyle: "none", display: "grid", gap: 8 }}>
            {picks.map((p) => (
              <li key={p.dish_id} data-testid="manager-pick" style={{ display: "flex", gap: 12, alignItems: "center", flexWrap: "wrap" }}>
                <IconPin size={16} aria-hidden="true" color="var(--primary)" />
                <strong>{p.dish_name}</strong>
                {p.note && <span style={{ color: "var(--text-muted)" }}>— {p.note}</span>}
                <RoleGate min="operator">
                  <SecondaryButton size="xs" disabled={row.busy} onClick={() => void row.run(() => api.deleteHotOverride(p.dish_id))}>Убрать</SecondaryButton>
                </RoleGate>
              </li>
            ))}
          </ul>
        </Card>
      )}
      {hidden.length > 0 && (
        <Card title="Скрыто менеджером">
          <ul aria-label="Скрыто менеджером" style={{ margin: 0, paddingLeft: 0, listStyle: "none", display: "grid", gap: 8 }}>
            {hidden.map((h) => (
              <li key={h.dish_id} style={{ display: "flex", gap: 12, alignItems: "center", flexWrap: "wrap" }}>
                <IconEyeOff size={16} aria-hidden="true" />
                <span>{h.dish_name}</span>
                {h.in_offers && <span style={{ color: "var(--text-muted)", font: "var(--type-small)" }}>расчёт его предлагает</span>}
                <RoleGate min="operator">
                  <SecondaryButton size="xs" disabled={row.busy} leftSection={<IconEye size={14} aria-hidden="true" />} onClick={() => void row.run(() => api.deleteHotOverride(h.dish_id))}>Вернуть</SecondaryButton>
                </RoleGate>
              </li>
            ))}
          </ul>
        </Card>
      )}
      {row.error && <p role="alert" style={{ color: "var(--urgency-critical-fg)", margin: 0 }}>{row.error}</p>}
      <RoleGate min="operator">
        <Card title="Добавить блюдо в предложения">
          <form
            style={{ display: "flex", gap: 12, flexWrap: "wrap", alignItems: "flex-end" }}
            onSubmit={(e) => { e.preventDefault(); if (dish) void add.run(() => api.setHotOverride(dish, "pin", note)); }}
          >
            <NativeSelect
              label="Блюдо"
              data={[{ value: "", label: options.length ? "Выберите блюдо" : "Все доступные блюда уже в списке" }, ...options.map((d) => ({ value: d.id, label: d.name }))]}
              value={dish}
              onChange={(e) => setDish(e.currentTarget.value)}
              style={{ minWidth: 220 }}
            />
            <TextInput label="Подсказка для зала" placeholder="например, предлагать к обеду" maxLength={200} value={note} onChange={(e) => setNote(e.currentTarget.value)} style={{ minWidth: 260, flex: 1 }} />
            <PrimaryButton type="submit" disabled={!dish || add.busy} leftSection={<IconPlus size={16} aria-hidden="true" />}>Добавить</PrimaryButton>
          </form>
          <p style={{ margin: "8px 0 0", font: "var(--type-small)", color: "var(--text-muted)" }}>
            В списке только блюда, которые можно приготовить. Добавленное блюдо попадёт в стоп-лист — из предложений уйдёт само.
          </p>
          {add.error && <p role="alert" style={{ color: "var(--urgency-critical-fg)", margin: "8px 0 0" }}>{add.error}</p>}
        </Card>
      </RoleGate>
    </div>
  );
}
