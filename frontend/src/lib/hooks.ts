import { useEffect, useRef, useState } from "react";

export function useElapsed(active: boolean): number {
  const [elapsed, setElapsed] = useState(0);
  const started = useRef(0);
  useEffect(() => {
    if (!active) {
      setElapsed(0);
      return;
    }
    started.current = Date.now();
    setElapsed(0);
    const timer = window.setInterval(() => setElapsed(Date.now() - started.current), 1000);
    return () => window.clearInterval(timer);
  }, [active]);
  return elapsed;
}

export function formatElapsed(ms: number): string {
  const total = Math.floor(ms / 1000);
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${minutes}:${seconds.toString().padStart(2, "0")}`;
}

/** Keeps a save spinner on screen for a short minimum. Saving to a local
 *  SQLite completes in a few milliseconds, so without this the button looks
 *  like it did nothing at all. */
export async function holdSpinner(startedAt: number, minimumMs = 400): Promise<void> {
  const remaining = minimumMs - (Date.now() - startedAt);
  if (remaining > 0) await new Promise((resolve) => setTimeout(resolve, remaining));
}

export interface AsyncState<T> {
  data: T | undefined;
  loading: boolean;
  error: unknown;
  reload: () => void;
  setData: (value: T) => void;
}

export function useAsync<T>(load: () => Promise<T>, deps: unknown[]): AsyncState<T> {
  const [data, setData] = useState<T>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    load()
      .then((value) => {
        if (!cancelled) setData(value);
      })
      .catch((reason) => {
        if (!cancelled) setError(reason);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick]);
  return { data, loading, error, reload: () => setTick((value) => value + 1), setData };
}
