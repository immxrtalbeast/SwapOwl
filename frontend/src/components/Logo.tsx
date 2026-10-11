interface LogoProps {
  /** inverse — белый лист без квадрата, для боковой панели на --primary. */
  variant?: "full" | "inverse";
  size?: number;
  /** Слово «SwapOwl» рядом со знаком. */
  withWord?: boolean;
}

/** Логотип SwapOwl (docs/design/swapowl/logo.svg, logo-inverse.svg). */
export function Logo({ variant = "full", size = 40, withWord = false }: LogoProps) {
  const inverse = variant === "inverse";
  const accent = inverse ? "var(--primary)" : "var(--brand)";
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 10 }}>
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" width={size} height={size} role="img" aria-label="SwapOwl">
        {!inverse && <rect width="64" height="64" rx="12" fill="var(--brand)" />}
        <path d="M19 11h18l9 9v31a2 2 0 0 1-2 2H19a2 2 0 0 1-2-2V13a2 2 0 0 1 2-2z" fill="#fff" />
        <path d="M37 11v7a2 2 0 0 0 2 2h7z" fill={accent} opacity=".35" />
        <path d="M20.5 25.5 21.5 17.5 27 23.5z" fill={accent} />
        <path d="M42.5 25.5 41.5 17.5 36 23.5z" fill={accent} />
        <circle cx="25.8" cy="31" r="7" fill={accent} />
        <circle cx="37.2" cy="31" r="7" fill={accent} />
        <circle cx="25.8" cy="31" r="4.6" fill="#fff" />
        <circle cx="37.2" cy="31" r="4.6" fill="#fff" />
        <circle cx="25.8" cy="31.4" r="2.3" fill={accent} />
        <circle cx="37.2" cy="31.4" r="2.3" fill={accent} />
        <path d="M29.6 38.3h3.8l-1.9 3.6z" fill={accent} />
        <path d="M25.5 46.5l2 1.8 2-1.8M33.5 46.5l2 1.8 2-1.8" stroke={accent} strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" fill="none" opacity=".55" />
      </svg>
      {withWord && (
        <span className="so-nav-brand-word" style={{ font: "400 26px/1 var(--font-body)", color: inverse ? "var(--on-primary)" : "var(--brand-strong)" }}>
          SwapOwl
        </span>
      )}
    </span>
  );
}
