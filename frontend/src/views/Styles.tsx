import { useEffect, useRef, useState } from "react";
import { Info, KeyRound, ListChecks, Palette, PlugZap, RotateCcw, Save } from "lucide-react";
import { ApiError, api, postJSON, putJSON } from "../lib/api";
import { toastError, useApp } from "../lib/context";
import { formatLabel, formatDate, platformLabel } from "../lib/format";
import { useAsync } from "../lib/hooks";
import { t } from "../lib/i18n";
import type { Platform, PlatformID, WechatSettings } from "../lib/types";
import { Badge, Button, Card, CardHeader, Counter, Field, ListSkeleton, Notice, PageHeader, Segmented } from "../components/ui";

type Tab = "style" | "api" | "rules";

/**
 * One platform's configuration, in three tabs: the style layer of the draft
 * prompt, the Official Account credentials, and the read-only format rules.
 * Everything numeric is a format constant on the server, so the rules are
 * listed for reading rather than given as inputs.
 */
export default function StylesView({ platform: platformID }: { platform: PlatformID }) {
  const { platforms, focusTarget } = useApp();
  const platform = platforms.find((item) => item.id === platformID);
  const [tab, setTab] = useState<Tab>(focusTarget === "wechat" ? "api" : "style");

  // A push that failed for missing credentials lands here with target "wechat",
  // so open the credentials tab instead of leaving the owner to find it.
  useEffect(() => {
    if (focusTarget === "wechat") setTab("api");
  }, [focusTarget]);

  return (
    <>
      <PageHeader
        title={t("styles.title", { platform: platformLabel(platformID) })}
        actions={
          <Segmented<Tab>
            value={tab}
            onChange={setTab}
            options={[
              { value: "style", label: <span className="inline-flex items-center gap-1.5"><Palette className="size-3.5" />{t("styles.tabStyle")}</span> },
              { value: "api", label: <span className="inline-flex items-center gap-1.5"><PlugZap className="size-3.5" />{t("styles.tabApi")}</span> },
              { value: "rules", label: <span className="inline-flex items-center gap-1.5"><ListChecks className="size-3.5" />{t("styles.tabRules")}</span> },
            ]}
          />
        }
      />

      {tab === "style" ? (
        platform ? (
          <PlatformCard platform={platform} />
        ) : (
          <Card className="mt-5">
            <div className="p-5">
              <ListSkeleton rows={4} />
            </div>
          </Card>
        )
      ) : null}

      {tab === "api" ? <WechatConfig /> : null}

      {tab === "rules" ? (
        <div className="mt-5 space-y-5">
          <Card>
            <CardHeader icon={Info} title={t("styles.formatRules")} description={t("styles.formatReadonly")} />
            <dl className="divide-y divide-zinc-100 dark:divide-zinc-800">
              {(platform?.rules ?? []).map((rule) => (
                <div key={rule.label} className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 px-5 py-2.5">
                  <dt className="w-16 shrink-0 text-sm font-medium text-zinc-700 dark:text-zinc-300">{rule.label}</dt>
                  <dd className="min-w-0 flex-1 text-sm text-zinc-600 dark:text-zinc-400">{rule.value}</dd>
                </div>
              ))}
            </dl>
          </Card>
          {(platform?.gallery_rules ?? []).length ? (
            <Card>
              <CardHeader icon={Info} title={t("styles.galleryRules")} description={t("styles.formatReadonly")} />
              <dl className="divide-y divide-zinc-100 dark:divide-zinc-800">
                {(platform?.gallery_rules ?? []).map((rule) => (
                  <div key={rule.label} className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 px-5 py-2.5">
                    <dt className="w-16 shrink-0 text-sm font-medium text-zinc-700 dark:text-zinc-300">{rule.label}</dt>
                    <dd className="min-w-0 flex-1 text-sm text-zinc-600 dark:text-zinc-400">{rule.value}</dd>
                  </div>
                ))}
              </dl>
            </Card>
          ) : null}
        </div>
      ) : null}
    </>
  );
}

function PlatformCard({ platform }: { platform: Platform }) {
  const { reloadPlatforms, notify } = useApp();
  const [style, setStyle] = useState(platform.style_rules ?? "");
  const baseRef = useRef(platform.style_rules ?? "");
  const styleMax = platform.style_rules_max ?? 0;
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState<"save" | "reset" | null>(null);

  useEffect(() => {
    // Follow the server's text only when there are no unsaved edits, so a
    // platform reload (or a revision conflict refresh) never discards typing.
    const previousBase = baseRef.current;
    const next = platform.style_rules ?? "";
    baseRef.current = next;
    setStyle((current) => (current === previousBase ? next : current));
  }, [platform.id, platform.revision, platform.style_rules]);

  const dirty = style !== (platform.style_rules ?? "");
  const problem = !style.trim() ? t("styles.empty") : "";

  const onConflict = async (reason: unknown, fallback: string) => {
    if (reason instanceof ApiError && reason.code === "REVISION_CONFLICT") {
      await reloadPlatforms();
      notify({ kind: "error", title: fallback, detail: t("common.conflictReloaded") });
      return;
    }
    toastError(notify, reason, fallback);
  };

  const save = async () => {
    setBusy("save");
    try {
      await putJSON<Platform>(`/api/v1/platforms/${encodeURIComponent(platform.id)}/style`, {
        style_rules: style,
        if_revision: platform.revision ?? 0,
        note: note.trim(),
      });
      await reloadPlatforms();
      setNote("");
      notify({ kind: "success", title: t("styles.saved", { name: platform.name }), detail: t("styles.savedDetail") });
    } catch (reason) {
      await onConflict(reason, t("common.saveFailed"));
    } finally {
      setBusy(null);
    }
  };

  const reset = async () => {
    setBusy("reset");
    try {
      await postJSON<Platform>(`/api/v1/platforms/${encodeURIComponent(platform.id)}/style/reset`, {
        if_revision: platform.revision ?? 0,
      });
      setStyle(platform.defaults?.style_rules ?? "");
      baseRef.current = platform.defaults?.style_rules ?? "";
      await reloadPlatforms();
      setNote("");
      notify({ kind: "success", title: t("styles.reset", { name: platform.name }) });
    } catch (reason) {
      await onConflict(reason, t("styles.resetFailed"));
    } finally {
      setBusy(null);
    }
  };

  return (
    <Card className="mt-5">
      <CardHeader
        icon={Palette}
        title={
          <span className="flex flex-wrap items-center gap-2">
            {platform.name}
            <Badge>{formatLabel(platform.format)}</Badge>
          </span>
        }
        description={platform.description}
      />
      <div className="space-y-5 p-5">
        <Field
          label={t("styles.rulesLabel")}
          hint={
            <span className="flex justify-between gap-2">
              <span>{t("styles.rulesHint")}</span>
              {styleMax ? <Counter text={style} max={styleMax} /> : null}
            </span>
          }
        >
          <textarea
            className="field-input text-[13px] leading-relaxed"
            rows={12}
            value={style}
            maxLength={styleMax || undefined}
            disabled={!!busy}
            onChange={(event) => setStyle(event.target.value)}
          />
        </Field>

        {platform.defaults && style !== platform.defaults.style_rules ? (
          <details className="rounded-xl bg-zinc-50 px-4 py-3 text-sm dark:bg-zinc-900">
            <summary className="cursor-pointer list-none text-zinc-600 [&::-webkit-details-marker]:hidden dark:text-zinc-300">{t("styles.viewDefault")}</summary>
            <pre className="mt-2 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words text-zinc-600 dark:text-zinc-400">
              {platform.defaults.style_rules}
            </pre>
          </details>
        ) : null}

        <div className="flex flex-col gap-3 border-t border-zinc-100 pt-4 sm:flex-row sm:items-end dark:border-zinc-800">
          <div className="flex-1">
            <Field label={t("styles.note")}>
              <input
                className="field-input"
                value={note}
                maxLength={200}
                disabled={!!busy}
                onChange={(event) => setNote(event.target.value)}
              />
            </Field>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button variant="ghost" icon={RotateCcw} disabled={!!busy || !dirty} onClick={() => setStyle(platform.style_rules ?? "")}>
              {t("styles.discard")}
            </Button>
            <Button
              variant="secondary"
              icon={RotateCcw}
              loading={busy === "reset"}
              disabled={!!busy || !!platform.is_default}
              onClick={() => void reset()}
            >
              {t("styles.restore")}
            </Button>
            <Button icon={Save} loading={busy === "save"} disabled={!!busy || !dirty || !!problem} onClick={() => void save()}>
              {t("common.save")}
            </Button>
          </div>
        </div>
        {problem && dirty ? <Notice tone="warning">{problem}</Notice> : null}
      </div>
    </Card>
  );
}

/**
 * The Official Account credentials. They are stored in the local SQLite, so
 * they can be changed here instead of editing .env and restarting.
 */
function WechatConfig() {
  const { notify } = useApp();
  const settings = useAsync(() => api<WechatSettings>("/api/v1/wechat/settings"), []);
  const [form, setForm] = useState<WechatSettings>({ app_id: "", app_secret: "", author: "", thumb_media_id: "", proxy_url: "" });
  const [busy, setBusy] = useState<"save" | "test" | null>(null);
  const [testResult, setTestResult] = useState<"ok" | "failed" | null>(null);

  useEffect(() => {
    if (settings.data) setForm({ ...settings.data, app_secret: "" });
  }, [settings.data]);

  const dirty =
    form.app_id !== (settings.data?.app_id ?? "") ||
    form.app_secret !== "" ||
    form.author !== (settings.data?.author ?? "") ||
    form.thumb_media_id !== (settings.data?.thumb_media_id ?? "") ||
    form.proxy_url !== (settings.data?.proxy_url ?? "");

  const save = async () => {
    setBusy("save");
    setTestResult(null);
    try {
      const saved = await putJSON<WechatSettings>("/api/v1/wechat/settings", {
        app_id: form.app_id,
        app_secret: form.app_secret,
        author: form.author,
        thumb_media_id: form.thumb_media_id,
        proxy_url: form.proxy_url,
      });
      setForm({ ...saved, app_secret: "" });
      settings.setData(saved);
      notify({ kind: "success", title: t("wechat.saved") });
    } catch (reason) {
      toastError(notify, reason, t("common.saveFailed"));
    } finally {
      setBusy(null);
    }
  };

  const test = async () => {
    setBusy("test");
    setTestResult(null);
    try {
      await postJSON<unknown>("/api/v1/wechat/settings/test");
      setTestResult("ok");
      notify({ kind: "success", title: t("wechat.testOK") });
    } catch (reason) {
      setTestResult("failed");
      toastError(notify, reason, t("wechat.testFailed"));
    } finally {
      setBusy(null);
    }
  };

  const secretSaved = Boolean(settings.data?.app_secret_set || settings.data?.app_secret);
  const configured = Boolean(form.app_id && (form.app_secret || secretSaved));
  return (
    <Card className="mt-5">
      <CardHeader
        icon={KeyRound}
        title={t("wechat.title")}
        description={t("wechat.description")}
        actions={
          <Badge tone={configured ? "green" : "neutral"}>
            {configured ? t("wechat.configured") : t("wechat.notConfigured")}
          </Badge>
        }
      />
      <div className="space-y-4 p-5">
        {settings.error ? <Notice tone="warning">{t("wechat.loadFailed")}</Notice> : null}
        {settings.loading && !settings.data ? (
          <ListSkeleton rows={4} />
        ) : (
          <>
            <div className="grid gap-4 md:grid-cols-2">
              <Field label={t("wechat.appID")} hint={t("wechat.appIDHint")}>
                <input
                  className="field-input"
                  value={form.app_id}
                  disabled={!!busy}
                  autoComplete="off"
                  onChange={(event) => setForm({ ...form, app_id: event.target.value })}
                />
              </Field>
              <Field label={t("wechat.appSecret")} hint={t("wechat.appSecretHint")}>
                <input
                  className="field-input"
                  type="password"
                  value={form.app_secret}
                  disabled={!!busy}
                  autoComplete="new-password"
                  placeholder={secretSaved ? t("wechat.secretSaved") : undefined}
                  onChange={(event) => setForm({ ...form, app_secret: event.target.value })}
                />
              </Field>
              <Field label={t("wechat.author")} hint={t("wechat.authorHint")}>
                <input
                  className="field-input"
                  value={form.author}
                  maxLength={32}
                  disabled={!!busy}
                  onChange={(event) => setForm({ ...form, author: event.target.value })}
                />
              </Field>
              <Field label={t("wechat.thumb")} hint={t("wechat.thumbHint")}>
                <input
                  className="field-input"
                  value={form.thumb_media_id}
                  maxLength={200}
                  disabled={!!busy}
                  onChange={(event) => setForm({ ...form, thumb_media_id: event.target.value })}
                />
              </Field>
              <div className="md:col-span-2">
                <Field label={t("wechat.proxy")} hint={t("wechat.proxyHint")}>
                  <input
                    className="field-input font-mono text-[13px]"
                    value={form.proxy_url}
                    disabled={!!busy}
                    autoComplete="off"
                    spellCheck={false}
                    placeholder="socks5://user:password@host:port"
                    onChange={(event) => setForm({ ...form, proxy_url: event.target.value })}
                  />
                </Field>
              </div>
            </div>

            <Notice tone="info">
              {t("wechat.whitelistHint")}
              <span className="mt-1 block text-xs text-zinc-500">{t("wechat.clearHint")}</span>
              {settings.data?.updated_at ? (
                <span className="mt-1 block text-xs text-zinc-500">
                  {t("wechat.updatedAt", { date: formatDate(settings.data.updated_at) })}
                </span>
              ) : (
                <span className="mt-1 block text-xs text-zinc-500">{t("wechat.envSeed")}</span>
              )}
            </Notice>

            {testResult === "ok" ? <Notice tone="success">{t("wechat.testOK")}</Notice> : null}

            <div className="flex flex-wrap items-center gap-2 border-t border-zinc-100 pt-4 dark:border-zinc-800">
              <Button icon={Save} loading={busy === "save"} disabled={!!busy || !dirty} onClick={() => void save()}>
                {t("common.save")}
              </Button>
              <Button variant="secondary" icon={PlugZap} loading={busy === "test"} disabled={!!busy || dirty || !configured} onClick={() => void test()}>
                {t("wechat.test")}
              </Button>
              {dirty ? <span className="text-xs text-zinc-500">{t("wechat.saveBeforeTest")}</span> : null}
            </div>
          </>
        )}
      </div>
    </Card>
  );
}
