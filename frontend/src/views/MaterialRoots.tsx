import { useState } from "react";
import { CircleCheck, CircleX, FileUp, FolderOpen, FolderPlus, LoaderCircle, RefreshCw, Trash2 } from "lucide-react";
import { api, deleteJSON, describeError, postJSON } from "../lib/api";
import { toastError, useApp } from "../lib/context";
import { formatNumber } from "../lib/format";
import { useAsync } from "../lib/hooks";
import { t, type MessageKey } from "../lib/i18n";
import type { LibraryRoot, MaterialImportResult, ScanEntry, ScanReport } from "../lib/types";
import { Badge, Button, Card, cx, ErrorNotice, LongTask, Notice } from "../components/ui";

type ImportState = "sending" | "done" | "failed";

interface ImportProgress {
  state: ImportState;
  duplicate?: boolean;
  error?: unknown;
}

const formatSize = (bytes: number) => (bytes >= 1024 * 1024 ? `${(bytes / 1024 / 1024).toFixed(1)} MB` : `${(bytes / 1024).toFixed(1)} KB`);

// Choosing a folder opens the system dialog on this computer (the server runs
// locally), registers it and scans it. Scanning only reads; files are imported
// one by one after the owner ticks them.
export default function MaterialRootsView() {
  const { notify, bumpKnowledge, reloadStats } = useApp();
  const roots = useAsync(() => api<LibraryRoot[]>("/api/v1/roots"), []);
  const [picking, setPicking] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [scanning, setScanning] = useState<number | null>(null);
  const [report, setReport] = useState<ScanReport | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [progress, setProgress] = useState<Record<string, ImportProgress>>({});
  const [importing, setImporting] = useState(false);

  const rootList = Array.isArray(roots.data) ? roots.data : [];
  const candidates = report ? [...report.new, ...report.changed] : [];
  const busy = picking || scanning !== null || importing;

  const scan = async (root: LibraryRoot) => {
    setScanning(root.id);
    setError(null);
    setProgress({});
    try {
      const data = await postJSON<ScanReport>(`/api/v1/roots/${root.id}/scan`);
      setReport(data);
      setSelected(new Set([...data.new, ...data.changed].map((entry) => entry.rel_path)));
      reloadStats().catch(() => undefined);
    } catch (reason) {
      setError(reason);
      toastError(notify, reason);
    } finally {
      setScanning(null);
    }
  };

  const pick = async () => {
    setPicking(true);
    setError(null);
    try {
      const { path } = await postJSON<{ path: string }>("/api/v1/pick-folder");
      if (!path) return;
      const root = await postJSON<LibraryRoot>("/api/v1/roots", { path, name: "" });
      roots.reload();
      setPicking(false);
      await scan(root);
    } catch (reason) {
      setError(reason);
      toastError(notify, reason);
    } finally {
      setPicking(false);
    }
  };

  const removeRoot = async (root: LibraryRoot) => {
    if (!window.confirm(t("roots.confirmDelete", { name: root.name }))) return;
    try {
      await deleteJSON(`/api/v1/roots/${root.id}`);
      if (report?.root.id === root.id) setReport(null);
      roots.reload();
      bumpKnowledge();
      notify({ kind: "success", title: t("roots.removed", { name: root.name }) });
    } catch (reason) {
      toastError(notify, reason);
    }
  };

  const toggle = (rel: string) =>
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(rel)) next.delete(rel);
      else next.add(rel);
      return next;
    });

  // Files go one at a time: each is a whole-file model summary, and one failure
  // must not stop the rest.
  const importSelected = async () => {
    if (!report || importing) return;
    const todo = candidates.filter((entry) => selected.has(entry.rel_path) && progress[entry.rel_path]?.state !== "done");
    if (!todo.length) return;
    setImporting(true);
    setError(null);
    let ok = 0;
    let failed = 0;
    for (const entry of todo) {
      setProgress((current) => ({ ...current, [entry.rel_path]: { state: "sending" } }));
      try {
        const data = await postJSON<MaterialImportResult>(`/api/v1/roots/${report.root.id}/import`, { path: entry.rel_path });
        setProgress((current) => ({ ...current, [entry.rel_path]: { state: "done", duplicate: data.duplicate } }));
        ok++;
      } catch (reason) {
        setProgress((current) => ({ ...current, [entry.rel_path]: { state: "failed", error: reason } }));
        failed++;
      }
    }
    setImporting(false);
    bumpKnowledge();
    reloadStats().catch(() => undefined);
    roots.reload();
    if (failed) {
      notify({ kind: ok ? "info" : "error", title: t("materials.submittedWithFailures", { ok, failed }) });
    } else {
      notify({ kind: "success", title: t("materials.submittedOk", { n: ok }) });
    }
  };

  return (
    <div className="space-y-4">
      <div>
        <Button icon={FolderPlus} loading={picking} disabled={busy} onClick={pick}>
          {t("roots.pick")}
        </Button>
      </div>

      {error ? <ErrorNotice error={error} /> : null}

      <LongTask active={scanning !== null} title={t("roots.scanning")} />
      <LongTask active={importing} title={t("roots.importing")} />

      {rootList.length ? (
        <Card>
          <ul className="divide-y divide-zinc-100 dark:divide-zinc-800">
            {rootList.map((root) => (
              <li key={root.id} className="flex flex-wrap items-center gap-3 px-4 py-3 text-sm">
                <FolderOpen className="size-4 shrink-0 text-zinc-400" />
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">{root.name}</p>
                  <p className="truncate font-mono text-xs text-zinc-500 dark:text-zinc-400" title={root.path}>
                    {root.path}
                  </p>
                </div>
                <Badge tone="blue">{t("roots.docCount", { n: formatNumber(root.document_count) })}</Badge>
                <Button variant="secondary" size="sm" icon={RefreshCw} loading={scanning === root.id} disabled={busy} onClick={() => scan(root)}>
                  {t("roots.scan")}
                </Button>
                <Button variant="ghost" size="sm" icon={Trash2} aria-label={t("roots.remove")} disabled={busy} onClick={() => removeRoot(root)} />
              </li>
            ))}
          </ul>
        </Card>
      ) : null}

      {report ? (
        <>
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <span className="font-medium">{report.root.name}</span>
            <Badge tone={report.new.length ? "green" : "neutral"}>
              {t("roots.statNew")} {formatNumber(report.new.length)}
            </Badge>
            <Badge tone={report.changed.length ? "amber" : "neutral"}>
              {t("roots.statChanged")} {formatNumber(report.changed.length)}
            </Badge>
            <Badge>
              {t("roots.statUnchanged")} {formatNumber(report.unchanged)}
            </Badge>
            {report.skipped.length ? (
              <Badge tone="red">
                {t("roots.statSkipped")} {formatNumber(report.skipped.length)}
              </Badge>
            ) : null}
            {report.missing.length ? (
              <Badge tone="red">
                {t("roots.statMissing")} {formatNumber(report.missing.length)}
              </Badge>
            ) : null}
          </div>
          {report.truncated ? <Notice tone="warning">{t("roots.truncated")}</Notice> : null}
          {report.error_dirs?.length ? (
            <Notice tone="warning">{t("roots.errorDirs", { dirs: report.error_dirs.join(", ") })}</Notice>
          ) : null}

          {candidates.length ? (
            <Card>
              <ul className="divide-y divide-zinc-100 dark:divide-zinc-800">
                {report.new.map((entry) => (
                  <CandidateRow key={`n:${entry.rel_path}`} entry={entry} kind="new" checked={selected.has(entry.rel_path)} disabled={importing} progress={progress[entry.rel_path]} onToggle={toggle} />
                ))}
                {report.changed.map((entry) => (
                  <CandidateRow key={`c:${entry.rel_path}`} entry={entry} kind="changed" checked={selected.has(entry.rel_path)} disabled={importing} progress={progress[entry.rel_path]} onToggle={toggle} />
                ))}
              </ul>
              <div className="flex flex-wrap items-center gap-2 border-t border-zinc-100 p-3 dark:border-zinc-800">
                <Button icon={FileUp} loading={importing} disabled={!selected.size || importing} onClick={importSelected}>
                  {t("roots.importN", { n: selected.size })}
                </Button>
                <Button variant="ghost" size="sm" disabled={importing} onClick={() => setSelected(new Set(candidates.map((entry) => entry.rel_path)))}>
                  {t("roots.selectAll")}
                </Button>
                <Button variant="ghost" size="sm" disabled={importing} onClick={() => setSelected(new Set())}>
                  {t("roots.selectNone")}
                </Button>
              </div>
            </Card>
          ) : null}

          {report.skipped.length ? (
            <Card>
              <ul className="divide-y divide-zinc-100 dark:divide-zinc-800">
                {report.skipped.map((entry) => (
                  <li key={entry.rel_path} className="flex flex-wrap items-center gap-3 px-4 py-2 text-sm">
                    <span className="min-w-0 flex-1 truncate font-mono text-xs" title={entry.rel_path}>
                      {entry.rel_path}
                    </span>
                    <span className="text-xs text-zinc-500">{formatSize(entry.size)}</span>
                    <Badge tone="red">{t(`roots.reason.${entry.reason ?? "unreadable"}` as MessageKey)}</Badge>
                  </li>
                ))}
              </ul>
            </Card>
          ) : null}

          {report.missing.length ? (
            <Card>
              <ul className="divide-y divide-zinc-100 dark:divide-zinc-800">
                {report.missing.map((entry) => (
                  <li key={entry.rel_path} className="flex flex-wrap items-center gap-3 px-4 py-2 text-sm">
                    <span className="min-w-0 flex-1 truncate font-mono text-xs" title={entry.rel_path}>
                      {entry.rel_path}
                    </span>
                    <Badge tone="red">{t("materials.missing")}</Badge>
                  </li>
                ))}
              </ul>
            </Card>
          ) : null}
        </>
      ) : null}
    </div>
  );
}

function CandidateRow({
  entry,
  kind,
  checked,
  disabled,
  progress,
  onToggle,
}: {
  entry: ScanEntry;
  kind: "new" | "changed";
  checked: boolean;
  disabled: boolean;
  progress?: ImportProgress;
  onToggle: (rel: string) => void;
}) {
  const described = progress?.error ? describeError(progress.error) : null;
  const Icon = progress?.state === "sending" ? LoaderCircle : progress?.state === "done" ? CircleCheck : progress?.state === "failed" ? CircleX : null;
  return (
    <li className="flex flex-wrap items-center gap-3 px-3 py-2 text-sm">
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled || progress?.state === "done"}
        onChange={() => onToggle(entry.rel_path)}
        aria-label={entry.rel_path}
        className="size-4 shrink-0"
      />
      <div className="min-w-0 flex-1">
        <p className="truncate font-mono text-xs" title={entry.rel_path}>
          {entry.rel_path}
        </p>
        {described ? (
          <p className="text-xs text-rose-600 dark:text-rose-400">
            {described.code} · {described.title}
          </p>
        ) : progress?.state === "done" && progress.duplicate ? (
          <p className="text-xs text-zinc-500">{t("materials.alreadyExists")}</p>
        ) : null}
      </div>
      <span className="text-xs text-zinc-500">{formatSize(entry.size)}</span>
      <Badge tone={kind === "new" ? "green" : "amber"}>{kind === "new" ? t("roots.statNew") : t("roots.statChanged")}</Badge>
      {Icon ? (
        <Icon
          className={cx(
            "size-4 shrink-0",
            progress?.state === "sending" && "animate-spin text-sky-500",
            progress?.state === "done" && "text-emerald-500",
            progress?.state === "failed" && "text-rose-500",
          )}
        />
      ) : null}
    </li>
  );
}
