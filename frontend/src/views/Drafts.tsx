import { useEffect, useRef, useState } from "react";
import { FileText, History, RefreshCw, Send, Trash2 } from "lucide-react";
import { api, deleteJSON, withQuery } from "../lib/api";
import { toastError, useApp } from "../lib/context";
import { draftDisclaimer, formatDate, formatLabel, platformLabel } from "../lib/format";
import { useAsync } from "../lib/hooks";
import { t } from "../lib/i18n";
import { DEFAULT_PLATFORM } from "../lib/nav";
import type { MediaDraft, Paged, PlatformID } from "../lib/types";
import { draftTitle } from "../components/draftContent";
import {
  Badge,
  Button,
  Card,
  CardHeader,
  Empty,
  ErrorNotice,
  ListSkeleton,
  Notice,
  PageHeader,
  Pager,
} from "../components/ui";

const PAGE = 20;

/**
 * The draft list: one row per article, newest first. Opening a row leaves this
 * screen for its own page, where the article is read, edited, pushed or
 * deleted.
 */
export default function DraftsView({ platform: platformID }: { platform: PlatformID }) {
  const { jumpTo, health, notify } = useApp();
  const composeView = DEFAULT_PLATFORM.compose;
  const detailView = DEFAULT_PLATFORM.detail;
  const configView = DEFAULT_PLATFORM.config;
  const [page, setPage] = useState(1);
  const [busy, setBusy] = useState<{ id: number; kind: "push" | "delete" } | null>(null);

  const drafts = useAsync(
    () => api<Paged<MediaDraft>>(withQuery("/api/v1/media-drafts", { platform: platformID, limit: PAGE, offset: (page - 1) * PAGE })),
    [platformID, page],
  );
  const rows = drafts.data?.items ?? [];
  const total = drafts.data?.total ?? 0;

  const pushDraft = async (draft: MediaDraft) => {
    if (!health?.wechat_configured) {
      notify({ kind: "info", title: t("drafts.pushNeedConfig") });
      jumpTo(configView, "wechat");
      return;
    }
    // A picture post is covered by its first generated image, so it does not
    // need the account's default cover.
    if (draft.format !== "gallery" && !health?.wechat_default_cover) {
      notify({ kind: "info", title: t("drafts.pushNeedCover") });
      jumpTo(detailView, `draft:${draft.id}`);
      return;
    }
    setBusy({ id: draft.id, kind: "push" });
    try {
      await api<MediaDraft>(`/api/v1/media-drafts/${draft.id}/wechat`, { method: "POST" });
      notify({ kind: "success", title: t("push.done") });
      drafts.reload();
    } catch (reason) {
      toastError(notify, reason, t("push.failed"));
    } finally {
      setBusy(null);
    }
  };

  const removeDraft = async (draft: MediaDraft) => {
    if (!window.confirm(t("draftDetail.confirmDelete", { id: draft.id }))) return;
    setBusy({ id: draft.id, kind: "delete" });
    try {
      await deleteJSON(`/api/v1/media-drafts/${draft.id}`);
      notify({ kind: "success", title: t("draftDetail.deleted", { id: draft.id }) });
      drafts.reload();
    } catch (reason) {
      toastError(notify, reason, t("common.deleteFailed"));
    } finally {
      setBusy(null);
    }
  };

  // Writing happens in the background; the list refreshes itself while any row
  // is still running.
  const reloadRef = useRef(drafts.reload);
  reloadRef.current = drafts.reload;
  const anyRunning = rows.some((draft) => draft.status === "running");
  useEffect(() => {
    if (!anyRunning) return;
    const timer = window.setInterval(() => reloadRef.current(), 2500);
    return () => window.clearInterval(timer);
  }, [anyRunning]);

  useEffect(() => {
    setPage(1);
  }, [platformID]);

  return (
    <>
      <PageHeader
        title={t("drafts.title", { platform: platformLabel(platformID) })}
        description={t("drafts.description")}
        actions={
          <Button variant="secondary" icon={FileText} onClick={() => jumpTo(composeView)}>
            {t("drafts.goWrite")}
          </Button>
        }
      />

      <Notice tone="warning">{draftDisclaimer()}</Notice>

      <Card className="mt-5">
        <CardHeader
          icon={History}
          title={t("drafts.history")}
          actions={<Button variant="ghost" size="sm" icon={RefreshCw} aria-label={t("common.refresh")} onClick={drafts.reload} />}
        />
        <div className="p-2 sm:p-4">
          {drafts.error ? <ErrorNotice error={drafts.error} className="mb-4" /> : null}
          {drafts.loading && !drafts.data ? (
            <ListSkeleton rows={4} />
          ) : !rows.length ? (
            <Empty
              icon={FileText}
              frame={false}
              title={t("drafts.emptyTitle")}
              description={t("drafts.emptyDesc")}
              action={
                <Button variant="secondary" onClick={() => jumpTo(composeView)}>
                  {t("drafts.goWrite")}
                </Button>
              }
            />
          ) : (
            <>
              <div className="hidden grid-cols-[minmax(0,1fr)_7rem_9rem_9.5rem] gap-3 border-b border-zinc-100 px-3 pb-2 text-[11px] font-semibold tracking-wide text-zinc-500 md:grid dark:border-zinc-800 dark:text-zinc-400">
                <span>{t("drafts.colTitle")}</span>
                <span>{t("drafts.colStatus")}</span>
                <span>{t("drafts.colCreated")}</span>
                <span className="text-right">{t("drafts.colActions")}</span>
              </div>
              <ul className="divide-y divide-zinc-100 dark:divide-zinc-800">
                {rows.map((draft) => {
                  const title = draftTitle(draft) || draft.topic || `#${draft.id}`;
                  return (
                    <li
                      key={draft.id}
                      className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 rounded-lg px-3 py-3 transition hover:bg-zinc-50 md:grid-cols-[minmax(0,1fr)_7rem_9rem_9.5rem] dark:hover:bg-zinc-800/50"
                    >
                      <button
                        type="button"
                        onClick={() => jumpTo(detailView, `draft:${draft.id}`)}
                        className="min-w-0 text-left"
                      >
                        <span className="block truncate text-sm font-medium">
                          <span className="mr-1.5 text-xs font-normal text-zinc-400">#{draft.id}</span>
                          {title}
                        </span>
                        <span className="block truncate text-xs text-zinc-500">
                          {formatLabel(draft.format)} · {draft.topic}
                        </span>
                      </button>
                      <div className="flex items-center gap-1.5 justify-self-end md:justify-self-start">
                        <DraftStatus draft={draft} />
                      </div>
                      <span className="hidden text-xs text-zinc-500 md:block">{formatDate(draft.created_at)}</span>
                      <div className="col-span-2 flex items-center justify-end gap-1 md:col-span-1">
                        <Button variant="ghost" size="sm" onClick={() => jumpTo(detailView, `draft:${draft.id}`)}>
                          {t("drafts.view")}
                        </Button>
                        {draft.status === "generated" ? (
                          <Button
                            variant="ghost"
                            size="sm"
                            icon={Send}
                            loading={busy?.id === draft.id && busy.kind === "push"}
                            title={draft.wechat_pushed_at ? t("push.repush") : t("push.submit")}
                            aria-label={t("push.submit")}
                            onClick={() => void pushDraft(draft)}
                          >
                            <span className="hidden lg:inline">{draft.wechat_pushed_at ? t("push.repush") : t("push.submit")}</span>
                          </Button>
                        ) : null}
                        <Button
                          variant="ghost"
                          size="sm"
                          icon={Trash2}
                          loading={busy?.id === draft.id && busy.kind === "delete"}
                          disabled={draft.status === "running"}
                          title={t("common.delete")}
                          aria-label={t("common.delete")}
                          onClick={() => void removeDraft(draft)}
                        >
                          <span className="hidden lg:inline">{t("common.delete")}</span>
                        </Button>
                      </div>
                    </li>
                  );
                })}
              </ul>
            </>
          )}
          <Pager page={page} pageSize={PAGE} total={total} onPage={setPage} />
        </div>
      </Card>
    </>
  );
}

function DraftStatus({ draft }: { draft: MediaDraft }) {
  return (
    <>
      {draft.status === "running" ? <Badge tone="blue">{t("drafts.statusRunning")}</Badge> : null}
      {draft.status === "failed" ? <Badge tone="red">{t("drafts.statusFailed")}</Badge> : null}
      {draft.status === "generated" && draft.wechat_pushed_at ? (
        <Badge tone="green">
          <Send className="mr-1 size-3" />
          {t("drafts.statusPushed")}
        </Badge>
      ) : null}
      {draft.status === "generated" && !draft.wechat_pushed_at ? <Badge tone="neutral">{t("drafts.statusGenerated")}</Badge> : null}
    </>
  );
}
