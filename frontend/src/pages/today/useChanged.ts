import { useEffect, useRef, useState } from "react";

/** Ключи, чьё значение изменилось с прошлого рендера (первый показ не считается): для подсветки по SSE. */
export function useChangedKeys(items: Record<string, string>): ReadonlySet<string> {
  const prev = useRef<Record<string, string> | null>(null);
  const [changed, setChanged] = useState<ReadonlySet<string>>(new Set());
  const sig = JSON.stringify(items);
  useEffect(() => {
    const before = prev.current;
    prev.current = items;
    if (!before) return;
    const next = new Set(Object.keys(items).filter((k) => before[k] !== items[k]));
    setChanged(next);
    if (next.size === 0) return;
    const t = setTimeout(() => setChanged(new Set()), 1600);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sig]);
  return changed;
}
