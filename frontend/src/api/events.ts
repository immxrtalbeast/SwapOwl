// Один EventSource на приложение (solution.md, 7.5). События — сигналы «перечитай».
// При подключении и переподключении вызываются обработчики resync: экраны перечитывают все данные.

export type ConnectionStatus = "connecting" | "online" | "offline";

export interface StandEvent {
  type: string;
  data: Record<string, unknown>;
}

interface SourceLike {
  addEventListener(type: string, fn: (e: MessageEvent) => void): void;
  onopen: ((e: Event) => void) | null;
  onerror: ((e: Event) => void) | null;
  close(): void;
}

export const EVENT_TYPES = [
  "delivery.accepted",
  "order.accepted",
  "order.rejected",
  "batch.expiring",
  "batch.expired",
  "stock.changed",
  "stoplist.changed",
  "hotoffers.changed",
  "clock.changed",
  "stand.reset",
  "ping",
] as const;

export interface EventsOptions {
  url?: string;
  /** Пауза перед переподключением, мс. */
  retryMs?: number;
  /** Сервер шлёт ping каждые 15 с; тишина дольше этого срока считается потерей связи, мс. */
  silenceMs?: number;
  create?: (url: string) => SourceLike;
}

export function createEvents(opts: EventsOptions = {}) {
  const url = opts.url ?? "/events";
  const retryMs = opts.retryMs ?? 3000;
  const silenceMs = opts.silenceMs ?? 40000;
  const create = opts.create ?? ((u: string) => new EventSource(u, { withCredentials: true }) as unknown as SourceLike);

  let status: ConnectionStatus = "connecting";
  let source: SourceLike | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let silenceTimer: ReturnType<typeof setTimeout> | null = null;
  let started = false;
  const statusFns = new Set<(s: ConnectionStatus) => void>();
  const eventFns = new Set<(e: StandEvent) => void>();
  const resyncFns = new Set<() => void>();

  const setStatus = (s: ConnectionStatus) => {
    if (s === status) return;
    status = s;
    statusFns.forEach((f) => f(s));
  };

  const armSilence = () => {
    if (silenceTimer) clearTimeout(silenceTimer);
    silenceTimer = setTimeout(() => lost(), silenceMs);
  };

  function lost() {
    if (silenceTimer) clearTimeout(silenceTimer);
    silenceTimer = null;
    source?.close();
    source = null;
    setStatus("offline");
    if (started && !retryTimer) {
      retryTimer = setTimeout(() => {
        retryTimer = null;
        connect();
      }, retryMs);
    }
  }

  function connect() {
    if (!started || source) return;
    const es = create(url);
    source = es;
    es.onopen = () => {
      if (source !== es) return;
      setStatus("online");
      armSilence();
      resyncFns.forEach((f) => f());
    };
    es.onerror = () => {
      if (source === es) lost();
    };
    for (const t of EVENT_TYPES) {
      es.addEventListener(t, (m) => {
        if (source !== es) return;
        armSilence();
        if (t === "ping") return;
        let data: Record<string, unknown> = {};
        try {
          data = JSON.parse(m.data) ?? {};
        } catch {
          /* полезная нагрузка минимальна, это только сигнал */
        }
        eventFns.forEach((f) => f({ type: t, data }));
      });
    }
  }

  return {
    start() {
      if (started) return;
      started = true;
      connect();
    },
    stop() {
      started = false;
      if (retryTimer) clearTimeout(retryTimer);
      if (silenceTimer) clearTimeout(silenceTimer);
      retryTimer = silenceTimer = null;
      source?.close();
      source = null;
      status = "connecting";
    },
    getStatus: () => status,
    onStatus(fn: (s: ConnectionStatus) => void) {
      statusFns.add(fn);
      return () => void statusFns.delete(fn);
    },
    /** Подписка на события; types — список нужных типов или пусто для всех. */
    onEvent(fn: (e: StandEvent) => void, types?: readonly string[]) {
      const h = (e: StandEvent) => {
        if (!types || types.includes(e.type)) fn(e);
      };
      eventFns.add(h);
      return () => void eventFns.delete(h);
    },
    /** Вызывается при каждом подключении и переподключении. */
    onResync(fn: () => void) {
      resyncFns.add(fn);
      return () => void resyncFns.delete(fn);
    },
  };
}

export const events = createEvents();
