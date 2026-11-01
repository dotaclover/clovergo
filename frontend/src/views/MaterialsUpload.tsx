import { useRef, useState, type DragEvent } from "react";
import { CircleCheck, CircleX, Clock, FileUp, LoaderCircle, Trash2 } from "lucide-react";
import { api, describeError } from "../lib/api";
import { useApp } from "../lib/context";
import { t } from "../lib/i18n";
import type { MaterialImportResult } from "../lib/types";
import { Button, cx, LongTask } from "../components/ui";

type QueueState = "waiting" | "sending" | "queued" | "failed";

interface QueueItem {
  key: string;
  file: File;
  state: QueueState;
  result?: MaterialImportResult;
  error?: unknown;
}

const ACCEPTED = /\.(md|markdown|txt|docx)$/i;

// Choose files and they upload one by one. Importing only archives.
export default function MaterialsUploadView() {
  const { notify, bumpKnowledge, reloadStats } = useApp();
  const [queue, setQueue] = useState<QueueItem[]>([]);
  const [uploading, setUploading] = useState(false);
  const [dragging, setDragging] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const update = (key: string, patch: Partial<QueueItem>) =>
    setQueue((current) => current.map((item) => (item.key === key ? { ...item, ...patch } : item)));

  const upload = async (items: QueueItem[]) => {
    setUploading(true);
    let ok = 0;
    let failed = 0;
    for (const item of items) {
      update(item.key, { state: "sending", error: undefined });
      const form = new FormData();
      form.append("file", item.file);
      try {
        const data = await api<MaterialImportResult>("/api/v1/materials", { method: "POST", body: form });
        update(item.key, { state: "queued", result: data });
        ok++;
      } catch (reason) {
        update(item.key, { state: "failed", error: reason });
        failed++;
      }
    }
    setUploading(false);
    bumpKnowledge();
    reloadStats().catch(() => undefined);
    if (failed) notify({ kind: ok ? "info" : "error", title: t("materials.submittedWithFailures", { ok, failed }) });
  };

  // Picking files starts the upload straight away; no second "Import" click.
  const addFiles = (files: FileList | File[] | null | undefined) => {
    if (inputRef.current) inputRef.current.value = "";
    if (uploading) return;
    const incoming = Array.from(files ?? []);
    const rejected = incoming.filter((file) => !ACCEPTED.test(file.name));
    if (rejected.length) {
      notify({ kind: "error", title: t("materials.onlyFormats"), detail: rejected.map((file) => file.name).join(t("common.listSep")) });
    }
    const items = incoming
      .filter((file) => ACCEPTED.test(file.name))
      .map((file) => ({ key: `${file.name}:${file.size}:${file.lastModified}:${Math.random()}`, file, state: "waiting" as QueueState }));
    if (!items.length) return;
    setQueue((current) => [...current.filter((item) => item.state !== "queued"), ...items]);
    void upload(items);
  };

  const onDrop = (event: DragEvent<HTMLLabelElement>) => {
    event.preventDefault();
    setDragging(false);
    addFiles(event.dataTransfer.files);
  };

  const retry = (item: QueueItem) => void upload([item]);

  return (
    <div className="space-y-4">
      <label
        onDragOver={(event) => {
          event.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
        className={cx(
          "flex cursor-pointer flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed px-6 py-12 text-center transition",
          dragging
            ? "border-emerald-500 bg-emerald-50 dark:bg-emerald-500/10"
            : "border-zinc-300 hover:border-zinc-400 hover:bg-zinc-50 dark:border-zinc-700 dark:hover:border-zinc-600 dark:hover:bg-zinc-900",
          uploading && "pointer-events-none opacity-60",
        )}
      >
        <input
          ref={inputRef}
          type="file"
          multiple
          accept=".md,.markdown,.txt,.docx,text/plain,text/markdown,application/vnd.openxmlformats-officedocument.wordprocessingml.document"
          className="sr-only"
          disabled={uploading}
          onChange={(event) => addFiles(event.target.files)}
        />
        <FileUp className="size-6 text-emerald-600 dark:text-emerald-400" />
        <p className="text-sm font-medium">{t("materials.dropHere")}</p>
        <p className="text-xs text-zinc-500">.md · .txt · .docx</p>
      </label>

      <LongTask active={uploading} title={t("materials.uploading")} hint={t("materials.uploadingHint")} />

      {queue.length ? (
        <ul className="divide-y divide-zinc-100 rounded-lg border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
          {queue.map((item) => (
            <QueueRow
              key={item.key}
              item={item}
              disabled={uploading}
              onRetry={() => retry(item)}
              onRemove={() => setQueue((current) => current.filter((entry) => entry.key !== item.key))}
            />
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function QueueRow({ item, disabled, onRetry, onRemove }: { item: QueueItem; disabled: boolean; onRetry: () => void; onRemove: () => void }) {
  const Icon = { waiting: Clock, sending: LoaderCircle, queued: CircleCheck, failed: CircleX }[item.state];
  const described = item.error ? describeError(item.error) : null;
  return (
    <li className="flex flex-wrap items-center gap-3 px-3 py-2 text-sm">
      <Icon
        className={cx(
          "size-4 shrink-0",
          item.state === "sending" && "animate-spin text-sky-500",
          item.state === "queued" && "text-emerald-500",
          item.state === "failed" && "text-rose-500",
          item.state === "waiting" && "text-zinc-400",
        )}
      />
      <div className="min-w-0 flex-1">
        <p className="truncate font-medium" title={item.file.name}>
          {item.file.name}
        </p>
        {item.state === "queued" && item.result?.duplicate ? (
          <p className="text-xs text-zinc-500">{t("materials.alreadyExists")}</p>
        ) : null}
        {item.state === "failed" && described ? (
          <p className="text-xs text-rose-600 dark:text-rose-400">
            {described.code} · {described.title}
          </p>
        ) : null}
      </div>
      {item.state === "failed" && !disabled ? (
        <Button variant="ghost" size="sm" onClick={onRetry}>
          {t("common.retry")}
        </Button>
      ) : null}
      {item.state !== "sending" ? (
        <Button variant="ghost" size="sm" icon={Trash2} aria-label={t("materials.removeFromQueue")} disabled={disabled} onClick={onRemove} />
      ) : null}
    </li>
  );
}
