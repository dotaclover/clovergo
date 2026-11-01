import { useState } from "react";
import { BookOpen, FileText, RefreshCw, Sparkles } from "lucide-react";
import { api, postJSON } from "../lib/api";
import { toastError, useApp } from "../lib/context";
import { formatNumber } from "../lib/format";
import { useAsync } from "../lib/hooks";
import { t } from "../lib/i18n";
import { Badge, Button, Card, CardHeader, cx, LongTask, Notice, PageHeader } from "../components/ui";

interface QueueStatus {
  pending: number;
  vector_pending: number;
  document_vector_pending: number;
}

interface ReindexResult {
  run_id: string;
  model: string;
  embedded: number;
  failed: number;
  remaining: number;
  first_error?: string;
}

// Vectorization is manual and runs only from this button. The screen shows how
// far the vectors have caught up; pressing update fills in the missing or stale
// ones and leaves every current vector untouched.
export default function VectorsView() {
  const { health, stats, reloadStats, notify } = useApp();
  const queue = useAsync(() => api<QueueStatus>("/api/v1/health/queue"), []);
  const [reindexing, setReindexing] = useState(false);
  const [result, setResult] = useState<ReindexResult | null>(null);

  const configured = !!health?.embedding_configured;
  const refresh = () => {
    reloadStats().catch(() => undefined);
    queue.reload();
  };

  const reindex = async () => {
    setReindexing(true);
    try {
      const data = await postJSON<ReindexResult>("/api/v1/vectors/reindex");
      setResult(data);
      refresh();
    } catch (reason) {
      toastError(notify, reason, t("vectors.reindexFailed"));
    } finally {
      setReindexing(false);
    }
  };

  return (
    <>
      <PageHeader
        title={t("vectors.title")}
        description={t("vectors.description")}
        actions={
          <>
            <Button variant="secondary" size="sm" icon={RefreshCw} onClick={refresh}>
              {t("common.refresh")}
            </Button>
            <Button size="sm" icon={Sparkles} loading={reindexing} disabled={!configured || reindexing} onClick={reindex}>
              {reindexing ? t("vectors.reindexing") : t("vectors.reindex")}
            </Button>
          </>
        }
      />

      <div className="mb-3 flex flex-wrap items-center gap-2 rounded-lg border border-zinc-200 bg-white px-3 py-1.5 text-[13px] dark:border-zinc-800 dark:bg-zinc-900/60">
        <span className="text-zinc-500 dark:text-zinc-400">{t("vectors.model")}</span>
        {configured ? (
          <Badge tone="green">{health?.embedding_model || t("common.unknown")}</Badge>
        ) : (
          <Badge tone="neutral">{t("vectors.modelUnset")}</Badge>
        )}
        <span className="ml-auto text-xs text-zinc-400">{t("vectors.keywordOnly")}</span>
      </div>

      {!configured ? (
        <div className="mb-3">
          <Notice tone="warning">
            <p className="font-medium">{t("vectors.offTitle")}</p>
            <p className="mt-0.5 text-xs">{t("vectors.offDesc")}</p>
            <p className="mt-0.5 text-xs opacity-80">{t("vectors.modelHint")}</p>
          </Notice>
        </div>
      ) : null}

      {reindexing ? (
        <div className="mb-3">
          <LongTask active title={t("vectors.reindexing")} />
        </div>
      ) : null}

      {result ? (
        <p className="mb-3 text-xs text-zinc-500 dark:text-zinc-400">
          {t("vectors.reindexed", {
            embedded: formatNumber(result.embedded),
            failed: formatNumber(result.failed),
            remaining: formatNumber(result.remaining),
          })}
        </p>
      ) : null}

      <div className="grid gap-3 md:grid-cols-2">
        <Panel
          icon={BookOpen}
          title={t("vectors.knowledgeTitle")}
          description={t("vectors.knowledgeDesc")}
          covered={stats?.vectors ?? 0}
          total={stats?.knowledge ?? 0}
          pending={queue.data?.vector_pending ?? 0}
        />
        <Panel
          icon={FileText}
          title={t("vectors.documentsTitle")}
          description={t("vectors.documentsDesc")}
          covered={stats?.document_vectors ?? 0}
          // Only a material with a summary can be embedded, so the denominator
          // is that subset rather than every archived file.
          total={stats?.document_vectors_total ?? 0}
          pending={queue.data?.document_vector_pending ?? 0}
          note={
            (stats?.documents ?? 0) - (stats?.document_vectors_total ?? 0) > 0
              ? t("vectors.documentsNoSummary", {
                  n: formatNumber((stats?.documents ?? 0) - (stats?.document_vectors_total ?? 0)),
                })
              : undefined
          }
        />
      </div>
    </>
  );
}

function Panel({
  icon: Icon,
  title,
  description,
  covered,
  total,
  pending,
  note,
}: {
  icon: typeof BookOpen;
  title: string;
  description: string;
  covered: number;
  total: number;
  pending: number;
  note?: string;
}) {
  const pct = total > 0 ? Math.round((covered / total) * 100) : 0;
  return (
    <Card>
      <CardHeader icon={Icon} title={title} description={description} />
      <div className="p-4">
        <div className="flex items-baseline justify-between gap-2">
          <p className="text-lg font-semibold tabular-nums">{t("vectors.covered", { covered: formatNumber(covered), total: formatNumber(total) })}</p>
          <span className="text-xs tabular-nums text-zinc-500 dark:text-zinc-400">{pct}%</span>
        </div>
        <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-zinc-200 dark:bg-zinc-800">
          <div className={cx("h-full rounded-full transition-all", pct === 100 ? "bg-emerald-500" : "bg-amber-500")} style={{ width: `${pct}%` }} />
        </div>
        <p className="mt-2 text-xs text-zinc-500 dark:text-zinc-400">
          {pending > 0 ? t("vectors.pending", { n: formatNumber(pending) }) : t("vectors.ready")}
        </p>
        {note ? <p className="mt-1 text-xs text-zinc-400 dark:text-zinc-500">{note}</p> : null}
      </div>
    </Card>
  );
}
