import { useEffect, useState } from "react";
import { ChevronDown, Eye, Pencil, PenLine, Quote, Trash2 } from "lucide-react";
import type { Evidence, KnowledgeItem } from "../lib/types";
import { kindLabel } from "../lib/format";
import { useApp, toastError } from "../lib/context";
import { deleteJSON, postJSON, putJSON } from "../lib/api";
import { holdSpinner } from "../lib/hooks";
import { t } from "../lib/i18n";
import { Badge, Button, cx } from "./ui";

export function EvidenceList({ evidence, compact }: { evidence?: Evidence[] | null; compact?: boolean }) {
  const items = Array.isArray(evidence) ? [...evidence].sort((a, b) => Number(!!a.superseded) - Number(!!b.superseded)) : [];
  if (!items.length) return <p className="text-xs text-zinc-500">{t("kcard.noEvidence")}</p>;
  return (
    <div className="space-y-2">
      {items.map((item, index) => (
        <figure
          key={`${item.document_id}-${item.quote_start}-${index}`}
          className={cx(
            "rounded-lg border-l-2",
            item.superseded
              ? "border-zinc-300 bg-zinc-50 opacity-70 dark:border-zinc-700 dark:bg-zinc-900"
              : "border-emerald-400 bg-emerald-50/50 dark:border-emerald-500/60 dark:bg-emerald-500/5",
            compact ? "px-3 py-2" : "px-3.5 py-2.5",
          )}
        >
          <blockquote
            className={cx(
              "text-sm leading-relaxed whitespace-pre-wrap break-words",
              item.superseded ? "text-zinc-500 line-through decoration-zinc-300 dark:text-zinc-500 dark:decoration-zinc-600" : "text-zinc-800 dark:text-zinc-200",
            )}
          >
            <Quote className={cx("mr-1 inline size-3 -translate-y-0.5", item.superseded ? "text-zinc-400" : "text-emerald-500")} />
            {item.quote}
          </blockquote>
          <figcaption className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-zinc-500 dark:text-zinc-400">
            <span className="font-medium text-zinc-600 dark:text-zinc-300">{item.document_name || t("kcard.documentN", { id: item.document_id })}</span>
            {item.superseded ? (
              <Badge tone="neutral">{t("kcard.oldVersion", { v: item.document_version ?? "?" })}</Badge>
            ) : item.document_version ? (
              <Badge tone="green">v{item.document_version}</Badge>
            ) : null}
          </figcaption>
        </figure>
      ))}
    </div>
  );
}

export function KnowledgeCard({
  item,
  onChanged,
  onCreate,
}: {
  item: KnowledgeItem;
  /** Called after an edit or a delete lands, so the list can reload. */
  onChanged?: () => void;
  /** Hands this item to the draft screen as the article's single source. */
  onCreate?: (id: number) => void;
}) {
  const { kinds, notify, reloadKinds } = useApp();
  const [open, setOpen] = useState(false);
  const [readCount, setReadCount] = useState(item.read_count ?? 0);
  // Re-sync when the card is reused for a different item.
  useEffect(() => setReadCount(item.read_count ?? 0), [item.id]);
  const [editing, setEditing] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [form, setForm] = useState({ kind: item.kind, title: item.title, body: item.body, tags: "" });
  const evidence = Array.isArray(item.evidence) ? item.evidence : [];
  const tags = Array.isArray(item.tags) ? item.tags : [];
  const topics = Array.isArray(item.topics) ? item.topics : [];

  const startEdit = () => {
    setForm({ kind: item.kind, title: item.title, body: item.body, tags: tags.join(t("common.tagSep")) });
    setConfirming(false);
    setEditing(true);
  };

  const save = async () => {
    const startedAt = Date.now();
    setBusy(true);
    try {
      await putJSON(`/api/v1/knowledge/${item.id}`, {
        kind: form.kind,
        title: form.title,
        body: form.body,
        tags: form.tags.split(/[,，]/).map((tag) => tag.trim()).filter(Boolean),
      });
      await holdSpinner(startedAt);
      notify({ kind: "success", title: t("kcard.saved", { id: item.id }), detail: t("kcard.savedDetail") });
      // A kind that was just typed is new to the server, so refresh the
      // taxonomy: without this it would not appear in the filter row.
      reloadKinds().catch(() => undefined);
      setEditing(false);
      onChanged?.();
    } catch (error) {
      toastError(notify, error, t("common.saveFailed"));
    } finally {
      setBusy(false);
    }
  };

  // Opening the evidence is what counts as reading an item: it is the moment
  // the owner actually looks at the conclusion and where it came from.
  const toggleOpen = async () => {
    const next = !open;
    setOpen(next);
    if (!next) return;
    try {
      const result = await postJSON<{ read_count: number }>(`/api/v1/knowledge/${item.id}/read`);
      if (typeof result.read_count === "number") setReadCount(result.read_count);
    } catch {
      // A counter that fails to record must never block reading.
    }
  };

  const remove = async () => {
    setBusy(true);
    try {
      await deleteJSON(`/api/v1/knowledge/${item.id}`);
      notify({ kind: "success", title: t("kcard.deleted", { id: item.id }), detail: t("kcard.deletedDetail") });
      onChanged?.();
    } catch (error) {
      toastError(notify, error, t("common.deleteFailed"));
    } finally {
      setBusy(false);
      setConfirming(false);
    }
  };

  return (
    <article className="group rounded-xl border border-zinc-200 bg-white p-4 transition hover:border-zinc-300 dark:border-zinc-800 dark:bg-zinc-900/60 dark:hover:border-zinc-700">
      <div className="flex gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <Badge tone="green">#{item.id}</Badge>
            <Badge>{kindLabel(item.kind, kinds)}</Badge>
            {topics.map((topic) => (
              <Badge key={topic} tone="blue">
                {topic}
              </Badge>
            ))}
            {item.match ? <MatchBadge match={item.match} score={item.score} /> : null}
            {typeof item.use_count === "number" ? (
              item.use_count > 0 ? (
                <Badge tone="violet">{t("kcard.written", { n: item.use_count })}</Badge>
              ) : (
                <Badge tone="amber">{t("knowledge.unused")}</Badge>
              )
            ) : null}
            <Badge tone="neutral" className="font-normal">
              <Eye className="mr-1 inline size-3 -translate-y-px" />
              {t("kcard.readCount", { n: readCount })}
            </Badge>
            {!editing ? (
              <span className="ml-auto flex shrink-0 gap-1">
                {onCreate ? (
                  <Button size="sm" variant="secondary" icon={PenLine} onClick={() => onCreate(item.id)}>
                    {t("kcard.create")}
                  </Button>
                ) : null}
                <button
                  type="button"
                  onClick={startEdit}
                  aria-label={t("kcard.editAria", { id: item.id })}
                  title={t("common.edit")}
                  className="rounded-md p-1 text-zinc-400 transition hover:bg-zinc-100 hover:text-zinc-700 dark:hover:bg-zinc-800 dark:hover:text-zinc-200"
                >
                  <Pencil className="size-3.5" />
                </button>
                <button
                  type="button"
                  onClick={() => setConfirming(true)}
                  aria-label={t("kcard.deleteAria", { id: item.id })}
                  title={t("common.delete")}
                  className="rounded-md p-1 text-zinc-400 transition hover:bg-rose-50 hover:text-rose-600 dark:hover:bg-rose-500/10 dark:hover:text-rose-400"
                >
                  <Trash2 className="size-3.5" />
                </button>
              </span>
            ) : null}
          </div>

          {editing ? (
            <div className="mt-2 space-y-2">
              <input
                value={form.kind}
                onChange={(event) => setForm((value) => ({ ...value, kind: event.target.value }))}
                aria-label={t("kcard.kind")}
                placeholder={t("kcard.kindPlaceholder")}
                list={`kind-options-${item.id}`}
                className="field-input h-9"
              />
              <datalist id={`kind-options-${item.id}`}>
                {kinds.map((entry) => (
                  <option key={entry.id} value={entry.id}>
                    {entry.label}
                  </option>
                ))}
              </datalist>
              <input
                value={form.title}
                onChange={(event) => setForm((value) => ({ ...value, title: event.target.value }))}
                aria-label={t("kcard.titleField")}
                placeholder={t("kcard.titleField")}
                className="field-input h-9"
              />
              <textarea
                value={form.body}
                onChange={(event) => setForm((value) => ({ ...value, body: event.target.value }))}
                aria-label={t("kcard.bodyField")}
                rows={5}
                placeholder={t("kcard.bodyField")}
                className="field-input"
              />
              <input
                value={form.tags}
                onChange={(event) => setForm((value) => ({ ...value, tags: event.target.value }))}
                aria-label={t("kcard.tagsField")}
                placeholder={t("kcard.tagsPlaceholder")}
                className="field-input h-9"
              />
              <div className="flex gap-2">
                <Button size="sm" loading={busy} onClick={save}>
                  {busy ? t("common.saving") : t("common.save")}
                </Button>
                <Button size="sm" variant="ghost" disabled={busy} onClick={() => setEditing(false)}>
                  {t("common.cancel")}
                </Button>
              </div>
              <p className="text-xs text-zinc-500">{t("kcard.editNote")}</p>
            </div>
          ) : (
            <>
              <h3 className="mt-2 font-semibold tracking-tight break-words">{item.title}</h3>
              <p className="mt-1 text-sm leading-relaxed whitespace-pre-wrap break-words text-zinc-600 dark:text-zinc-300">{item.body}</p>
              {tags.length ? (
                <div className="mt-2 flex flex-wrap gap-1">
                  {tags.map((tag) => (
                    <Badge key={tag} tone="neutral" className="font-normal">
                      {tag}
                    </Badge>
                  ))}
                </div>
              ) : null}
            </>
          )}

          {confirming ? (
            <div className="mt-2.5 flex flex-wrap items-center gap-2 rounded-lg border border-rose-200 bg-rose-50/70 px-3 py-2 dark:border-rose-500/30 dark:bg-rose-500/5">
              <span className="text-xs text-rose-800 dark:text-rose-200">
                {t("kcard.confirmDelete", { id: item.id, n: evidence.length })}
              </span>
              <span className="ml-auto flex gap-2">
                <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirming(false)}>
                  {t("common.cancel")}
                </Button>
                <Button size="sm" loading={busy} onClick={remove}>
                  {t("common.confirmDelete")}
                </Button>
              </span>
            </div>
          ) : null}

          <button
            type="button"
            onClick={() => void toggleOpen()}
            className="mt-2.5 inline-flex items-center gap-1 text-xs font-medium text-emerald-700 hover:text-emerald-900 dark:text-emerald-400 dark:hover:text-emerald-300"
            aria-expanded={open}
          >
            <ChevronDown className={cx("size-3.5 transition", open && "rotate-180")} />
            {t("kcard.evidence", { n: evidence.filter((entry) => !entry.superseded).length })}
            {evidence.some((entry) => entry.superseded) ? t("kcard.evidenceOld", { n: evidence.filter((entry) => entry.superseded).length }) : ""}
          </button>
          {open ? (
            <div className="mt-2">
              <EvidenceList evidence={evidence} compact />
            </div>
          ) : null}
        </div>
      </div>
    </article>
  );
}

export function CitationCard({ id, title, evidence }: { id: number; title: string; evidence?: Evidence[] }) {
  return (
    <article className="rounded-xl border border-zinc-200 p-4 dark:border-zinc-800">
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone="green">{t("kcard.knowledgeN", { id })}</Badge>
        <h4 className="font-medium break-words">{title}</h4>
      </div>
      <div className="mt-2.5">
        <EvidenceList evidence={evidence} compact />
      </div>
    </article>
  );
}

export function MatchBadge({ match, score }: { match: string; score?: number }) {
  const label =
    match === "both" ? t("match.both") : match === "semantic" ? t("match.semantic") : match === "keyword" ? t("match.keyword") : match;
  const tone = match === "both" ? "green" : match === "semantic" ? "violet" : "neutral";
  const showScore = typeof score === "number" && score > 0;
  return (
    <Badge tone={tone} className="font-normal">
      {label}
      {showScore ? <span className="font-mono tabular-nums opacity-80">{score.toFixed(2)}</span> : null}
    </Badge>
  );
}
