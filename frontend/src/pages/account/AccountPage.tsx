import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { Alert, PasswordInput, Stack } from "@mantine/core";
import { IconCircleCheck } from "@tabler/icons-react";
import { api } from "../../api/client";
import { useAuth } from "../../auth/AuthContext";
import { roleLabel } from "../../auth/roles";
import { Card, EmptyState, LoadingState, PrimaryButton, ScreenTitle, SecondaryButton } from "../../components";
import { changePasswordError, validatePasswordChange } from "./validate";

/** Привязка Telegram: одноразовый код → вводится в mini-app; права там те же, что в вебе. Нет входа через Telegram — карточки нет. */
function TelegramCard() {
  const [linked, setLinked] = useState<boolean | null>(null);
  const [code, setCode] = useState<{ value: string; until: number } | null>(null);
  const [left, setLeft] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [off, setOff] = useState(false);

  useEffect(() => {
    api.telegramLinkStatus().then((s) => setLinked(s.linked)).catch(() => setOff(true));
  }, []);
  useEffect(() => {
    if (!code) return;
    const tick = () => {
      const s = Math.max(0, Math.round((code.until - Date.now()) / 1000));
      setLeft(s);
      if (s === 0) setCode(null);
    };
    tick();
    const id = window.setInterval(tick, 1000);
    return () => window.clearInterval(id);
  }, [code]);

  if (off || linked === null) return null;

  async function issue() {
    setBusy(true);
    setError(null);
    try {
      const r = await api.telegramLinkCode();
      setCode({ value: r.code, until: Date.now() + r.expires_in_seconds * 1000 });
    } catch (e) {
      setError(e instanceof Error ? e.message : "Не удалось получить код");
    } finally {
      setBusy(false);
    }
  }
  async function unlink() {
    setBusy(true);
    setError(null);
    try {
      await api.telegramUnlink();
      setLinked(false);
      setCode(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Не удалось отвязать");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card title="Telegram" style={{ maxWidth: 480, marginBottom: 16 }}>
      <Stack gap="md">
        <p data-testid="tg-link-status" style={{ margin: 0 }}>
          {linked ? "Telegram привязан: mini-app открывается под вашим аккаунтом, с теми же правами." : "Telegram не привязан."}
        </p>
        {code ? (
          <div role="status" style={{ display: "grid", gap: 6 }}>
            <code data-testid="tg-link-code" className="so-num" style={{ font: "700 32px/40px var(--font-display)", letterSpacing: 3, userSelect: "all" }}>{code.value}</code>
            <span style={{ font: "var(--type-small)", color: "var(--text-muted)" }}>
              Откройте бота @swapowlbot → «Открыть SwapOwl» → введите код. Действует ещё {Math.floor(left / 60)}:{String(left % 60).padStart(2, "0")}.
            </span>
          </div>
        ) : null}
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          <PrimaryButton loading={busy} onClick={() => void issue()}>{linked ? "Привязать другой Telegram" : "Привязать Telegram"}</PrimaryButton>
          {linked && <SecondaryButton disabled={busy} onClick={() => void unlink()}>Отвязать</SecondaryButton>}
        </div>
        {error && <Alert color="red" role="alert">{error}</Alert>}
      </Stack>
    </Card>
  );
}

/** Экран «Аккаунт»: смена собственного пароля (POST /api/auth/password) и привязка Telegram. */
export function AccountPage() {
  const { state } = useAuth();
  const [oldPw, setOldPw] = useState("");
  const [newPw, setNewPw] = useState("");
  const [repeat, setRepeat] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const [busy, setBusy] = useState(false);

  if (state.kind !== "user") return <LoadingState rows={3} label="Загрузка аккаунта" />;
  if (state.login === null) {
    return (
      <section aria-labelledby="account-title">
        <ScreenTitle>Аккаунт</ScreenTitle>
        <Card>
          <EmptyState title="Личной учётной записи нет" text="Стенд открыт без входа, менять нечего." />
        </Card>
      </section>
    );
  }

  async function submit(ev: FormEvent) {
    ev.preventDefault();
    setDone(false);
    const invalid = validatePasswordChange(oldPw, newPw, repeat);
    if (invalid) {
      setError(invalid);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await api.changePassword(oldPw, newPw);
      setOldPw("");
      setNewPw("");
      setRepeat("");
      setDone(true);
    } catch (e) {
      setError(changePasswordError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section aria-labelledby="account-title">
      <ScreenTitle>Аккаунт</ScreenTitle>
      <p style={{ margin: "0 0 16px", font: "var(--type-body)" }}>
        Вы вошли как <strong>{state.login}</strong> ({roleLabel[state.role]}).
      </p>
      <TelegramCard />
      <Card title="Смена пароля" style={{ maxWidth: 480 }}>
        <form onSubmit={(e) => void submit(e)} aria-label="Смена пароля">
          <Stack gap="md">
            <PasswordInput label="Текущий пароль" value={oldPw} onChange={(e) => setOldPw(e.currentTarget.value)} autoComplete="current-password" required />
            <PasswordInput label="Новый пароль" description="От 8 до 72 байт" value={newPw} onChange={(e) => setNewPw(e.currentTarget.value)} autoComplete="new-password" required />
            <PasswordInput label="Повторите новый пароль" value={repeat} onChange={(e) => setRepeat(e.currentTarget.value)} autoComplete="new-password" required />
            <p style={{ margin: 0, font: "var(--type-small)", color: "var(--text-muted)" }}>
              После смены остальные ваши сессии (на других устройствах) закроются.
            </p>
            <div>
              <PrimaryButton type="submit" loading={busy}>Сменить пароль</PrimaryButton>
            </div>
            {error && <Alert color="red" role="alert">{error}</Alert>}
            {done && (
              <Alert color="green" role="status" data-testid="password-changed" icon={<IconCircleCheck size={18} aria-hidden="true" />}>
                Пароль изменён.
              </Alert>
            )}
          </Stack>
        </form>
      </Card>
    </section>
  );
}
