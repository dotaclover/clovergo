import { useEffect, useState } from "react";
import { Inbox, RefreshCw, RotateCcw, Search, Trash2, FileText } from "lucide-react";
import { api, deleteJSON } from "../lib/api";
import { toastError, useApp } from "../lib/context";
import { formatDate, statusLabel } from "../lib/format";
import { useAsync } from "../lib/hooks";
import { t } from "../lib/i18n";
import type { DocumentItem, MaterialImportResult } from "../lib/types";
import { Badge, Button, Empty, ErrorNotice, LongTask, PageHeader, Pager, Segmented } from "../components/ui";

type StatusFilter = "all" | "running" | "done" | "failed";

const PAGE = 20;
const GRID = "lg:grid lg:grid-cols-[3rem_minmax(0,1fr)_7rem_auto]";

// A plain list of materials. Each row offers Detail / Extract / Delete; the
// summary and original text live on the detail page.
export default function MaterialsListView() {
  const { notify, bumpKnowledge, navigate, jumpTo } = useApp();
  const documents = useAsync(() => api<DocumentItem[]>("/api/v1/documents"), []);
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [page, setPage] = useState(1);
  const [busyDoc, setBusyDoc] = useState<DocumentItem | null>(null);

  const list = Array.isArray(documents.data) ? documents.data : [];
  const inFlight = list.some((item) => item.status === "processing");

  useEffect(() => {
    if (!inFlight) return;
    const timer = window.setInterval(documents.reload, 4000);
    return () => window.clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inFlight]);

  // Only a click on Refresh spins the button; the background poll stays quiet.
  const [manualRefresh, setManualRefresh] = useState(false);
  useEffect(() => {
    if (!documents.loading) setManualRefresh(false);
  }, [documents.loading]);

  const needle = query.trim().toLowerCase();
  const filtered = list.filter((doc) => {
    if (statusFilter !== "all" && statusGroupFilter(doc.status) !== statusFilter) return false;
    if (!needle) return true;
    return [doc.filename, doc.summary ?? ""].some((field) => field.toLowerCase().includes(needle));
  });

  useEffect(() => {
    setPage(1);
  }, [query, statusFilter]);

  const pages = Math.max(1, Math.ceil(filtered.length / PAGE));
  const current = Math.min(page, pages);
  const pageItems = filtered.slice((current - 1) * PAGE, current * PAGE);

  const reprocess = async (doc: DocumentItem) => {
    setBusyDoc(doc);
    try {
      await api<MaterialImportResult>(`/api/v1/materials/${doc.id}/reprocess`, { method: "POST" });
      notify({ kind: "success", title: t("materials.resubmitted", { name: doc.filename }) });
      bumpKnowledge();
    } catch (reason) {
      toastError(notify, reason);
    } finally {
      setBusyDoc(null);
      documents.reload();
    }
  };

  const remove = async (doc: DocumentItem) => {
    if (!window.confirm(t("documents.confirmDelete", { name: doc.filename }))) return;
    setBusyDoc(doc);
    try {
      await deleteJSON(`/api/v1/documents/${doc.id}`);
      notify({ kind: "success", title: t("documents.deleted") });
      bumpKnowledge();
    } catch (reason) {
      toastError(notify, reason, t("common.deleteFailed"));
    } finally {
      setBusyDoc(null);
      documents.reload();
    }
  };

  return (
    <>
      <PageHeader
        title={t("documents.title")}
        description={t("documents.description")}
        actions={
          <Button
            variant="secondary"
            size="sm"
            icon={RefreshCw}
            loading={manualRefresh && documents.loading}
            onClick={() => {
              setManualRefresh(true);
              documents.reload();
            }}
          >
            {t("common.refresh")}
          </Button>
        }
      />

      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="relative min-w-56 flex-1">
          <Search className="pointer-events-none absolute inset-y-0 left-2.5 my-auto size-3.5 text-zinc-400" />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t("documents.searchPlaceholder")}
            aria-label={t("documents.searchPlaceholder")}
            className="field-input h-9 pl-8"
          />
        </div>
        <Segmented
          value={statusFilter}
          onChange={setStatusFilter}
          options={[
            { value: "all", label: t("documents.statusAll") },
            { value: "running", label: statusLabel("processing") },
            { value: "done", label: statusLabel("published") },
            { value: "failed", label: statusLabel("analysis_failed") },
          ]}
        />
      </div>

      <div className="overflow-hidden rounded-lg border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900/60">
        <div
          className={`hidden ${GRID} items-center gap-3 border-b border-zinc-100 bg-zinc-50/70 px-3 py-2 text-xs font-medium text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400`}
        >
          <span>{t("documents.rowId")}</span>
          <span>{t("documents.rowMaterial")}</span>
          <span>{t("documents.rowDate")}</span>
          <span className="text-right">{t("documents.rowActions")}</span>
        </div>

        {documents.error ? (
          <div className="p-3">
            <ErrorNotice error={documents.error} />
          </div>
        ) : documents.loading && !documents.data ? (
          <div className="space-y-2 p-3">
            {[0, 1, 2].map((row) => (
              <div key={row} className="h-10 animate-pulse rounded-md bg-zinc-100 dark:bg-zinc-800" />
            ))}
          </div>
        ) : !list.length ? (
          <Empty
            icon={Inbox}
            title={t("documents.emptyTitle")}
            description={t("documents.emptyDesc")}
            frame={false}
            action={
              <Button variant="secondary" onClick={() => navigate("upload")}>
                {t("documents.goUpload")}
              </Button>
            }
          />
        ) : !filtered.length ? (
          <Empty icon={Inbox} title={t("documents.emptyFilter")} frame={false} />
        ) : (
          <ul className="divide-y divide-zinc-100 dark:divide-zinc-800">
            {pageItems.map((doc) => (
              <DocumentRow
                key={doc.id}
                doc={doc}
                busy={busyDoc?.id === doc.id}
                disabled={!!busyDoc}
                onDetail={() => jumpTo("documentDetail", `document:${doc.id}`)}
                onProcess={() => reprocess(doc)}
                onDelete={() => remove(doc)}
              />
            ))}
          </ul>
        )}
      </div>

      {filtered.length ? <Pager page={current} pageSize={PAGE} total={filtered.length} onPage={setPage} /> : null}

      {busyDoc && busyDoc.status !== "processing" ? (
        <div className="mt-3">
          <LongTask active title={t("materials.resubmitting", { name: busyDoc.filename })} hint={t("materials.resubmittingHint")} />
        </div>
      ) : null}
    </>
  );
}

function statusGroupFilter(status: string): StatusFilter {
  switch (status) {
    case "processing":
      return "running";
    case "published":
    case "processed_empty":
      return "done";
    case "analysis_failed":
      return "failed";
    default:
      return "all";
  }
}

function DocumentRow({
  doc,
  busy,
  disabled,
  onDetail,
  onProcess,
  onDelete,
}: {
  doc: DocumentItem;
  busy: boolean;
  disabled: boolean;
  onDetail: () => void;
  onProcess: () => void;
  onDelete: () => void;
}) {
  const processing = doc.status === "processing";
  const complete = doc.status === "published" || doc.status === "processed_empty";

  return (
    <li className="px-4 py-3.5 text-[13px]">
      <div className={`flex flex-wrap items-start gap-x-4 gap-y-2 ${GRID} lg:items-center lg:gap-4`}>
        <span className="hidden font-mono text-xs text-zinc-400 lg:block">#{doc.id}</span>
        <div className="min-w-0 flex-1">
          <button type="button" onClick={onDetail} className="block max-w-full truncate text-left text-sm font-medium hover:underline" title={doc.filename}>
            {doc.filename}
          </button>
          {doc.missing || (doc.use_count ?? 0) > 0 ? (
            <div className="mt-1 flex flex-wrap items-center gap-1.5">
              {doc.missing ? <Badge tone="red">{t("materials.missing")}</Badge> : null}
              {(doc.use_count ?? 0) > 0 ? (
                <span className="text-xs text-zinc-500 dark:text-zinc-400">{t("materials.useCount", { n: doc.use_count ?? 0 })}</span>
              ) : null}
            </div>
          ) : null}
          {doc.summary ? (
            <p className="mt-1.5 line-clamp-2 text-xs leading-6 break-words text-zinc-500 dark:text-zinc-400">{doc.summary}</p>
          ) : null}
        </div>

        <span className="hidden text-xs text-zinc-400 lg:block">{formatDate(doc.created_at)}</span>

        <div className="flex items-center gap-1.5 lg:justify-end">
          <Button size="sm" variant="secondary" icon={FileText} disabled={disabled} onClick={onDetail}>
            {t("documents.detail")}
          </Button>
          <Button
            size="sm"
            variant={complete ? "secondary" : "subtle"}
            icon={RotateCcw}
            loading={busy && !processing}
            disabled={processing || disabled}
            onClick={onProcess}
          >
            {processing ? t("materials.processing") : t("documents.extract")}
          </Button>
          <Button size="sm" variant="secondary" icon={Trash2} disabled={processing || disabled} onClick={onDelete}>
            {t("documents.delete")}
          </Button>
        </div>
      </div>
    </li>
  );
}
