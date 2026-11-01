import { useState, type FormEvent } from "react";
import { Clover, LockKeyhole } from "lucide-react";
import { describeError, postJSON, setToken } from "../lib/api";
import { t } from "../lib/i18n";
import { Button } from "./ui";

/**
 * Full-screen gate shown while a global password is set and this browser holds
 * no valid unlock token. It sits outside AppContext on purpose: nothing behind
 * it may load before the server accepts the token.
 */
export default function LockScreen({ onUnlocked }: { onUnlocked: () => void }) {
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!password || busy) return;
    setBusy(true);
    setError("");
    try {
      const result = await postJSON<{ token: string }>("/api/v1/auth/unlock", { password });
      setToken(result.token);
      setPassword("");
      onUnlocked();
    } catch (reason) {
      setError(describeError(reason).title);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="grid min-h-screen place-items-center px-4">
      <form onSubmit={submit} className="w-full max-w-sm rounded-2xl border border-zinc-200 bg-white p-6 shadow-sm dark:border-zinc-800 dark:bg-zinc-900/60">
        <div className="flex items-center gap-2.5">
          <span className="grid size-9 place-items-center rounded-xl bg-gradient-to-br from-emerald-500 to-teal-600 text-white">
            <Clover className="size-5" />
          </span>
          <p className="font-semibold tracking-tight">CloverGo</p>
        </div>
        <div className="mt-5 flex items-center gap-2">
          <LockKeyhole className="size-4 text-emerald-600 dark:text-emerald-400" />
          <h1 className="text-base font-semibold">{t("lock.title")}</h1>
        </div>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">{t("lock.subtitle")}</p>
        <label className="mt-4 block">
          <span className="mb-1.5 block text-sm font-medium text-zinc-700 dark:text-zinc-300">{t("lock.password")}</span>
          <input
            type="password"
            className="field-input"
            autoFocus
            autoComplete="current-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </label>
        {error ? (
          <p role="alert" className="mt-2 text-sm text-rose-600 dark:text-rose-400">
            {error}
          </p>
        ) : null}
        <Button type="submit" className="mt-4 w-full" loading={busy} disabled={!password}>
          {t("lock.unlock")}
        </Button>
      </form>
    </div>
  );
}
