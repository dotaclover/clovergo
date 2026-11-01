import { useState, type FormEvent } from "react";
import { KeyRound, LockKeyhole, ShieldOff } from "lucide-react";
import { postJSON, putJSON, setToken } from "../lib/api";
import { toastError, useApp } from "../lib/context";
import { t } from "../lib/i18n";
import { Badge, Button, Card, CardHeader, Field, Notice, PageHeader } from "../components/ui";

/**
 * Global lock password. The hash lives in the server database; this browser
 * keeps only the unlock token the server hands back, in localStorage.
 */
export default function SecurityView() {
  const { authEnabled, setAuthEnabled, notify } = useApp();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [repeat, setRepeat] = useState("");
  const [removeCurrent, setRemoveCurrent] = useState("");
  const [saving, setSaving] = useState(false);
  const [removing, setRemoving] = useState(false);

  async function save(event: FormEvent) {
    event.preventDefault();
    if (next !== repeat) {
      notify({ kind: "error", title: t("security.mismatch") });
      return;
    }
    setSaving(true);
    try {
      const result = await putJSON<{ enabled: boolean; token: string }>("/api/v1/auth/password", { current, password: next });
      setToken(result.token);
      setAuthEnabled(true);
      setCurrent("");
      setNext("");
      setRepeat("");
      notify({ kind: "success", title: t("security.saved") });
    } catch (reason) {
      toastError(notify, reason);
    } finally {
      setSaving(false);
    }
  }

  async function remove(event: FormEvent) {
    event.preventDefault();
    if (!window.confirm(t("security.confirmRemove"))) return;
    setRemoving(true);
    try {
      await postJSON("/api/v1/auth/password/remove", { current: removeCurrent });
      setToken("");
      setAuthEnabled(false);
      setRemoveCurrent("");
      notify({ kind: "success", title: t("security.removed") });
    } catch (reason) {
      toastError(notify, reason);
    } finally {
      setRemoving(false);
    }
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title={t("security.title")}
        description={t("security.desc")}
        actions={<Badge tone={authEnabled ? "green" : "neutral"}>{authEnabled ? t("security.statusOn") : t("security.statusOff")}</Badge>}
      />
      <Card>
        <CardHeader icon={KeyRound} title={authEnabled ? t("security.changeHeading") : t("security.setHeading")} />
        <form onSubmit={save} className="max-w-md space-y-3 p-4">
          {authEnabled ? (
            <Field label={t("security.current")}>
              <input type="password" className="field-input" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} />
            </Field>
          ) : null}
          <Field label={t("security.new")} hint={t("security.hint")}>
            <input type="password" className="field-input" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} />
          </Field>
          <Field label={t("security.confirm")}>
            <input type="password" className="field-input" autoComplete="new-password" value={repeat} onChange={(e) => setRepeat(e.target.value)} />
          </Field>
          <Button type="submit" icon={LockKeyhole} loading={saving} disabled={!next || (authEnabled && !current)}>
            {authEnabled ? t("security.change") : t("security.set")}
          </Button>
        </form>
      </Card>
      {authEnabled ? (
        <Card>
          <CardHeader icon={ShieldOff} title={t("security.removeHeading")} />
          <form onSubmit={remove} className="max-w-md space-y-3 p-4">
            <Field label={t("security.current")}>
              <input type="password" className="field-input" autoComplete="current-password" value={removeCurrent} onChange={(e) => setRemoveCurrent(e.target.value)} />
            </Field>
            <Button type="submit" variant="secondary" loading={removing} disabled={!removeCurrent}>
              {t("security.remove")}
            </Button>
          </form>
        </Card>
      ) : null}
      <Notice>{t("security.note")}</Notice>
    </div>
  );
}
