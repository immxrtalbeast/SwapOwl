import type { HubSeriesPoint } from "../../api/network";
import type { HubPoint } from "../../api/network";
import { Card, HandNote } from "../../components";
import { formatRub } from "../../lib/format";
import { abScenarioNote, clockText, compareAb, diffText } from "./model";
import type { AbSide } from "./model";

const W = 640;
const H = 240;
const PAD = { l: 64, r: 16, t: 16, b: 32 };

/** Красивый верх оси: 1, 2, 5 × 10^n. */
function niceMax(v: number): number {
  if (v <= 0) return 100;
  const p = 10 ** Math.floor(Math.log10(v));
  const n = v / p;
  return (n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10) * p;
}

/** Два графика списаний на одной оси: X — часы стенда с начала прогона, Y — накопленные ₽. */
export function AbChart({ ours, base }: { ours: AbSide; base: AbSide }) {
  const all: HubSeriesPoint[] = [...ours.series, ...base.series];
  const xMax = Math.max(1, ...all.map((p) => p.hour));
  const yMax = niceMax(Math.max(0, ...all.map((p) => p.cost_minor)));
  const x = (h: number) => PAD.l + (h / xMax) * (W - PAD.l - PAD.r);
  const y = (c: number) => H - PAD.b - (c / yMax) * (H - PAD.t - PAD.b);
  const path = (s: HubSeriesPoint[]) => s.map((p, i) => `${i ? "L" : "M"}${x(p.hour).toFixed(1)} ${y(p.cost_minor).toFixed(1)}`).join(" ");
  const yTicks = [0, 0.5, 1].map((f) => f * yMax);
  const step = xMax <= 6 ? 1 : xMax <= 12 ? 2 : xMax <= 30 ? 6 : 12;
  const xTicks: number[] = [];
  for (let h = 0; h <= xMax; h += step) xTicks.push(h);
  const last = (s: AbSide) => s.series[s.series.length - 1];
  const desc = `Списания, ₽, по часам стенда. ${ours.label}: ${formatRub(ours.totalMinor ?? 0)}. ${base.label}: ${formatRub(base.totalMinor ?? 0)}.`;
  return (
    <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label={desc} style={{ width: "100%", height: "auto", display: "block" }} data-testid="ab-chart">
      {yTicks.map((t) => (
        <g key={t}>
          <line x1={PAD.l} x2={W - PAD.r} y1={y(t)} y2={y(t)} stroke="var(--border)" />
          <text x={PAD.l - 8} y={y(t) + 4} textAnchor="end" fontSize="12" fill="var(--text-muted)">{formatRub(t).replace(/,\d\d/, "")}</text>
        </g>
      ))}
      {xTicks.map((t) => (
        <text key={t} x={x(t)} y={H - 10} textAnchor="middle" fontSize="12" fill="var(--text-muted)">{t} ч</text>
      ))}
      {base.series.length > 1 && <path d={path(base.series)} fill="none" stroke="var(--urgency-risk-solid)" strokeWidth="3" strokeDasharray="8 5" strokeLinecap="round" strokeLinejoin="round" />}
      {ours.series.length > 1 && <path d={path(ours.series)} fill="none" stroke="var(--brand)" strokeWidth="4" strokeLinecap="round" strokeLinejoin="round" />}
      {[ours, base].map((s, i) => {
        const l = last(s);
        return l ? <circle key={i} cx={x(l.hour)} cy={y(l.cost_minor)} r="5" fill={i === 0 ? "var(--brand)" : "var(--urgency-risk-solid)"} /> : null;
      })}
    </svg>
  );
}

function standLabel(p: HubPoint): string {
  const t = p.run?.stand_time ?? p.stand_time;
  if (!t) return "время стенда: —";
  const d = new Date(t);
  const day = Number.isNaN(d.getTime()) ? "" : d.toLocaleDateString("ru-RU", { day: "numeric", month: "short" });
  return `время стенда: ${day} ${clockText(t)}`;
}

/** Сколько часов стенда длился прогон: конец самого длинного ряда. */
export function runHours(...sides: AbSide[]): number {
  return Math.max(0, ...sides.flatMap((s) => s.series.map((p) => p.hour)));
}

function Big({ side, tone, running }: { side: AbSide; tone: "ours" | "base"; running: boolean }) {
  return (
    <div data-testid={`ab-${tone}`} style={{ minWidth: 0 }}>
      <div style={{ font: "var(--type-body-strong)", color: tone === "ours" ? "var(--brand-ink)" : "var(--urgency-risk-fg)" }}>{side.label}</div>
      <div className="so-num" style={{ font: "700 40px/44px var(--font-sans, inherit)" }}>
        {side.totalMinor === null ? "—" : formatRub(side.totalMinor)}
      </div>
      <div style={{ color: "var(--text-muted)", font: "var(--type-small)" }}>списано, ₽{running ? ` · ${standLabel(side.point)}` : ""}</div>
    </div>
  );
}

/** Живое A/B: две точки группы ab, списания в ₽ рядом, разница и графики. Без запросов — их делает NetworkPage. */
export function AbCompare({ pair }: { pair: readonly [HubPoint, HubPoint] }) {
  const c = compareAb(pair);
  const note = abScenarioNote(pair);
  const saved = c.diffMinor !== null && c.diffMinor < 0;
  const hasRun = c.ours.totalMinor !== null || c.base.totalMinor !== null || c.ours.series.length > 0 || c.base.series.length > 0;
  if (!hasRun) {
    return (
      <Card title="Живое сравнение: со SwapOwl и без">
        <p data-testid="ab-empty" style={{ margin: 0, color: "var(--text-muted)" }}>Сравнение ещё не запускали: <code>ab-demo.sh start</code></p>
      </Card>
    );
  }
  const hours = Math.round(runHours(c.ours, c.base));
  const status = c.running ? "Прогон идёт, обновление каждые пару секунд." : `Последний прогон, ${hours} ч стенда.`;
  return (
    <Card title="Живое сравнение: со SwapOwl и без" actions={<span style={{ color: "var(--text-muted)", font: "var(--type-small)" }}>{status}</span>}>
      <div style={{ display: "grid", gap: 24, gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", alignItems: "start" }}>
        <Big side={c.ours} tone="ours" running={c.running} />
        <Big side={c.base} tone="base" running={c.running} />
        <div data-testid="ab-diff">
          <div style={{ font: "var(--type-body-strong)" }}>Разница</div>
          <div className="so-num" style={{ font: "700 40px/44px var(--font-sans, inherit)", color: saved ? "var(--brand-ink)" : "var(--text)" }}>{diffText(c)}</div>
          {saved && <HandNote arrow={null}>столько не ушло в мусор</HandNote>}
        </div>
      </div>
      {note && <p data-testid="ab-scenario" style={{ margin: "16px 0 0", color: "var(--text-muted)", font: "var(--type-small)" }}>{note}</p>}
      <div style={{ marginTop: 16 }}>
        <AbChart ours={c.ours} base={c.base} />
        <div style={{ display: "flex", gap: 16, flexWrap: "wrap", color: "var(--text-muted)", font: "var(--type-small)" }}>
          <span><b style={{ color: "var(--brand-ink)" }}>━</b> {c.ours.label}</span>
          <span><b style={{ color: "var(--urgency-risk-fg)" }}>╌</b> {c.base.label}</span>
          <span>Горизонталь — часы стенда с начала прогона</span>
        </div>
      </div>
    </Card>
  );
}
