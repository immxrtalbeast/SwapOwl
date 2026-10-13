import { useCallback, useState } from "react";
import type { FormEvent } from "react";
import { Alert, Group, PasswordInput, Select, Stack, TextInput } from "@mantine/core";
import { IconAlertTriangle, IconCopy } from "@tabler/icons-react";
import { api } from "../../api/client";
import type { ApiKeyInfo, CreatedApiKey, UserAccount } from "../../api/types";
import { useAuth } from "../../auth/AuthContext";
import { roleLabel } from "../../auth/roles";
import type { Role } from "../../auth/roles";
import { useLive } from "../../app/useLive";
import { Card, DataTable, EmptyState, ErrorState, LoadingState, PrimaryButton, ScreenTitle, SecondaryButton, TextButton } from "../../components";
import type { Column } from "../../components";
import { createUserError, deleteUserError } from "./errors";

type Data = { users: UserAccount[]; keys: ApiKeyInfo[] };
type State = { kind: "loading" } | { kind: "error"; message: string } | ({ kind: "ready" } & Data);

const msg = (e: unknown, fallback: string) => (e instanceof Error ? e.message : fallback);
const fmtDate = (iso: string) => new Date(iso).toLocaleString("ru-RU", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
const roleOptions = (roles: Role[]) => roles.map((r) => ({ value: r, label: roleLabel[r] }));

export function UsersPage() {
  const { state, logout } = useAuth();
  const role = state.kind === "user" ? state.role : null;
  if (role !== "admin") {
    return (
      <section>
        <ScreenTitle>Пользователи и ключи</ScreenTitle>
        <EmptyState title="Нужен вход администратора" text="Войдите под учётной записью администратора, чтобы управлять пользователями и ключами." action={{ label: "Войти", onClick: () => void logout() }} />
      </section>
    );
  }
  return <UsersAdmin />;
}

function UsersAdmin() {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [notice, setNotice] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [users, keys] = await Promise.all([api.users(), api.apiKeys()]);
      setState({ kind: "ready", users, keys });
    } catch (e) {
      setState({ kind: "error", message: msg(e, "Не удалось загрузить данные") });
    }
  }, []);

  useLive(load, ["stand.reset"]);

  return (
    <section aria-labelledby="users-title">
      <ScreenTitle>Пользователи и ключи</ScreenTitle>
      <p style={{ margin: "0 0 16px" }}><a href="#/account" style={{ color: "var(--brand-ink)" }}>Аккаунт: сменить свой пароль</a></p>
      {notice && <Alert color="red" role="alert" data-testid="users-notice" mb="md">{notice}</Alert>}
      {state.kind === "loading" && <LoadingState label="Загрузка пользователей и ключей" />}
      {state.kind === "error" && (
        <ErrorState what="пользователей и ключи" reason={state.message} onRetry={() => { setState({ kind: "loading" }); void load(); }} />
      )}
      {state.kind === "ready" && (
        <div style={{ display: "grid", gap: 24 }}>
          <UsersBlock users={state.users} reload={load} setNotice={setNotice} />
          <KeysBlock keys={state.keys} reload={load} setNotice={setNotice} />
        </div>
      )}
    </section>
  );
}

interface BlockProps {
  reload: () => Promise<void>;
  setNotice: (m: string | null) => void;
}

/** Кнопка удаления с подтверждением в самом интерфейсе (без window.confirm). */
function ConfirmAction({ label, ariaLabel, question, confirmLabel, onConfirm }: {
  label: string;
  ariaLabel: string;
  question: string;
  confirmLabel: string;
  onConfirm: () => Promise<void>;
}) {
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  if (!asking) {
    return <TextButton size="compact-sm" aria-label={ariaLabel} onClick={() => setAsking(true)}>{label}</TextButton>;
  }
  return (
    <Group gap={8} wrap="wrap" role="group" aria-label={question}>
      <span style={{ font: "var(--type-small)", color: "var(--text-muted)" }}>{question}</span>
      <PrimaryButton
        size="compact-sm"
        loading={busy}
        styles={{ root: { background: "var(--urgency-critical-solid)" } }}
        onClick={() => { setBusy(true); void onConfirm().finally(() => { setBusy(false); setAsking(false); }); }}
      >
        {confirmLabel}
      </PrimaryButton>
      <SecondaryButton size="compact-sm" onClick={() => setAsking(false)}>Отмена</SecondaryButton>
    </Group>
  );
}

function UsersBlock({ users, reload, setNotice }: BlockProps & { users: UserAccount[] }) {
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const [formError, setFormError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(ev: FormEvent) {
    ev.preventDefault();
    setBusy(true);
    setFormError(null);
    setNotice(null);
    try {
      await api.createUser({ login: login.trim(), password, role });
      setLogin("");
      setPassword("");
      await reload();
    } catch (e) {
      setFormError(createUserError(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove(u: UserAccount) {
    setNotice(null);
    try {
      await api.deleteUser(u.id);
      await reload();
    } catch (e) {
      setNotice(deleteUserError(e));
    }
  }

  const columns: Column<UserAccount>[] = [
    { key: "login", header: "Логин", cell: (u) => u.login },
    { key: "role", header: "Роль", cell: (u) => roleLabel[u.role] },
    { key: "created", header: "Создан", cell: (u) => fmtDate(u.created_at) },
    {
      key: "actions",
      header: <span className="sr-only">Действия</span>,
      cell: (u) => (
        <ConfirmAction
          label="Удалить"
          ariaLabel={`Удалить пользователя ${u.login}`}
          question={`Удалить «${u.login}»?`}
          confirmLabel="Да, удалить"
          onConfirm={() => remove(u)}
        />
      ),
    },
  ];

  return (
    <Card title="Пользователи" flush>
      {users.length === 0 ? (
        <EmptyState title="Пользователей нет" text="Создайте первого пользователя в форме ниже." />
      ) : (
        <div>
          <DataTable caption="Пользователи" columns={columns} rows={users} rowKey={(u) => String(u.id)} />
        </div>
      )}
      <form onSubmit={(e) => void submit(e)} aria-label="Новый пользователь" style={{ padding: "var(--card-pad)" }}>
        <Stack gap="md">
          <Group align="flex-start" gap="md" grow>
            <TextInput label="Логин" value={login} onChange={(e) => setLogin(e.currentTarget.value)} required maxLength={128} />
            <PasswordInput label="Пароль" description="От 8 до 72 байт" value={password} onChange={(e) => setPassword(e.currentTarget.value)} required minLength={8} maxLength={72} autoComplete="new-password" />
            <Select label="Роль" value={role} onChange={(v) => v && setRole(v as Role)} data={roleOptions(["viewer", "operator", "admin"])} allowDeselect={false} />
          </Group>
          <div><PrimaryButton type="submit" loading={busy}>Создать пользователя</PrimaryButton></div>
          {formError && <Alert color="red" role="alert">{formError}</Alert>}
        </Stack>
      </form>
    </Card>
  );
}

function KeysBlock({ keys, reload, setNotice }: BlockProps & { keys: ApiKeyInfo[] }) {
  const [name, setName] = useState("");
  const [role, setRole] = useState<ApiKeyInfo["role"]>("operator");
  const [formError, setFormError] = useState<string | null>(null);
  const [created, setCreated] = useState<CreatedApiKey | null>(null);
  const [copied, setCopied] = useState<"ok" | "fail" | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(ev: FormEvent) {
    ev.preventDefault();
    setBusy(true);
    setFormError(null);
    setNotice(null);
    try {
      const k = await api.createApiKey({ name: name.trim(), role });
      setCreated(k);
      setCopied(null);
      setName("");
      await reload();
    } catch (e) {
      setFormError(msg(e, "Не удалось создать ключ"));
    } finally {
      setBusy(false);
    }
  }

  async function copy() {
    if (!created) return;
    try {
      await navigator.clipboard.writeText(created.key);
      setCopied("ok");
    } catch {
      setCopied("fail");
    }
  }

  async function revoke(k: ApiKeyInfo) {
    setNotice(null);
    try {
      await api.deleteApiKey(k.id);
      if (created?.id === k.id) setCreated(null);
      await reload();
    } catch (e) {
      setNotice(msg(e, "Не удалось отозвать ключ"));
    }
  }

  const columns: Column<ApiKeyInfo>[] = [
    { key: "name", header: "Название", cell: (k) => k.name },
    { key: "role", header: "Роль", cell: (k) => roleLabel[k.role] },
    { key: "created", header: "Создан", cell: (k) => fmtDate(k.created_at) },
    {
      key: "actions",
      header: <span className="sr-only">Действия</span>,
      cell: (k) => (
        <ConfirmAction
          label="Отозвать"
          ariaLabel={`Отозвать ключ ${k.name}`}
          question="Адаптер с этим ключом перестанет работать. Отозвать?"
          confirmLabel="Да, отозвать"
          onConfirm={() => revoke(k)}
        />
      ),
    },
  ];

  return (
    <Card title="Ключи интеграции" flush>
      <p style={{ margin: 0, padding: "12px var(--card-pad) var(--card-pad)", font: "var(--type-body)", color: "var(--text-muted)" }}>
        Ключ нужен адаптеру: укажите его в настройках подключения. Роль — просмотр или оператор.
      </p>
      {created && (
        <div
          role="status"
          data-testid="created-key"
          style={{ margin: "0 var(--card-pad) var(--card-pad)", padding: 16, borderRadius: 10, background: "var(--brand-tint)", border: "1px solid var(--brand)" }}
        >
          <Group gap={8} mb={8} wrap="nowrap" align="flex-start">
            <IconAlertTriangle size={20} color="var(--brand-ink)" aria-hidden="true" style={{ flex: "none" }} />
            <strong style={{ color: "var(--brand-ink)" }}>
              Ключ «{created.name}» создан. Он показывается один раз — скопируйте его сейчас, позже его не увидеть.
            </strong>
          </Group>
          <code data-testid="key-value" style={{ display: "block", padding: 12, borderRadius: 6, background: "var(--surface)", font: "var(--type-num)", wordBreak: "break-all" }}>
            {created.key}
          </code>
          <Group gap={8} mt={12}>
            <PrimaryButton leftSection={<IconCopy size={16} aria-hidden="true" />} onClick={() => void copy()}>Скопировать</PrimaryButton>
            <SecondaryButton onClick={() => setCreated(null)}>Я сохранил ключ</SecondaryButton>
            {copied === "ok" && <span role="status" style={{ color: "var(--brand-ink)" }}>Скопировано.</span>}
            {copied === "fail" && <span role="alert" style={{ color: "var(--urgency-critical-fg)" }}>Не удалось скопировать — выделите ключ вручную.</span>}
          </Group>
        </div>
      )}
      {keys.length === 0 ? (
        <EmptyState title="Ключей нет" text="Создайте ключ, чтобы подключить адаптер." />
      ) : (
        <DataTable caption="Ключи интеграции" columns={columns} rows={keys} rowKey={(k) => String(k.id)} />
      )}
      <form onSubmit={(e) => void submit(e)} aria-label="Новый ключ" style={{ padding: "var(--card-pad)" }}>
        <Stack gap="md">
          <Group align="flex-start" gap="md" grow>
            <TextInput label="Название" value={name} onChange={(e) => setName(e.currentTarget.value)} required maxLength={128} placeholder="iiko-адаптер" />
            <Select label="Роль" value={role} onChange={(v) => v && setRole(v as ApiKeyInfo["role"])} data={roleOptions(["viewer", "operator"])} allowDeselect={false} />
          </Group>
          <div><PrimaryButton type="submit" loading={busy}>Создать ключ</PrimaryButton></div>
          {formError && <Alert color="red" role="alert">{formError}</Alert>}
        </Stack>
      </form>
    </Card>
  );
}
