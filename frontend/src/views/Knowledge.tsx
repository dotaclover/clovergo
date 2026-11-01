import { useEffect, useMemo, useState, type FormEvent } from "react";
import { BookOpen, RefreshCw, Search, X } from "lucide-react";
import { api, withQuery } from "../lib/api";
import { useApp } from "../lib/context";
import { kindLabel } from "../lib/format";
import { useAsync } from "../lib/hooks";
import { t, tx } from "../lib/i18n";
import { DEFAULT_PLATFORM } from "../lib/nav";
import type { KnowledgeItem, Paged, TopicSummary } from "../lib/types";
import { KnowledgeCard } from "../components/knowledge";
import { Button, cx, Empty, ErrorNotice, ListSkeleton, PageHeader, Pager, Segmented } from "../components/ui";

type Usage = "all" | "unused" | "used";

const PAGE = 20;

export default function KnowledgeView() {
  const { kinds, health, jumpTo, navigate, knowledgeVersion, stats, bumpKnowledge } = useApp();
  const compose = DEFAULT_PLATFORM.compose;
  // Creating a draft from one item is the whole reason the knowledge list is
  // here, so the item is handed over as the article's single source.
  const createFrom = (id: number) => jumpTo(compose, `knowledge:${id}`);
  const [usage, setUsage] = useState<Usage>("all");
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState("");
  const [topic, setTopic] = useState("");
  const [page, setPage] = useState(1);

  const topics = useAsync(() => api<TopicSummary[]>("/api/v1/topics"), [knowledgeVersion]);
  const topicList = Array.isArray(topics.data) ? topics.data : [];

  const isSearch = query !== "";
  const knowledge = useAsync<KnowledgeItem[] | Paged<KnowledgeItem>>(
    () =>
      isSearch
        ? api<KnowledgeItem[]>(withQuery("/api/v1/search", { q: query, topic, limit: 100 }))
        : api<Paged<KnowledgeItem>>(withQuery("/api/v1/knowledge", { kind, topic, usage: usage === "all" ? undefined : usage, limit: PAGE, offset: (page - 1) * PAGE })),
    [kind, usage, query, topic, knowledgeVersion, page],
  );

  // The browse list is paged on the server; search returns a bounded set
  // (limit 100) shown whole, so the page state only moves the browse list.
  const data = knowledge.data;
  const searchItems: KnowledgeItem[] = isSearch && Array.isArray(data) ? data : [];
  const listPage: Paged<KnowledgeItem> | undefined = !isSearch && data && !Array.isArray(data) ? data : undefined;
  const items = isSearch ? searchItems : (listPage?.items ?? []);
  const taxonomy = useMemo(() => (Array.isArray(kinds) ? kinds : []), [kinds]);
  // Search results come from a different endpoint, so the kind and usage
  // filters are applied locally there.
  const filtered = useMemo(() => {
    let visible = items;
    if (query) {
      if (kind) visible = visible.filter((item) => item.kind === kind);
      if (usage !== "all") visible = visible.filter((item) => (usage === "used" ? (item.use_count ?? 0) > 0 : !item.use_count));
    }
    return visible;
  }, [items, query, kind, usage]);

  const total = isSearch ? filtered.length : (listPage?.total ?? 0);

  useEffect(() => {
    setPage(1);
  }, [kind, usage, topic, query]);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    setQuery(draft.trim());
    setKind("");
  };

  const reset = () => {
    setDraft("");
    setQuery("");
    setKind("");
    setTopic("");
    setUsage("all");
    knowledge.reload();
  };

  return (
    <>
      <PageHeader
        title={t("knowledge.title")}
        description={t("knowledge.description")}
        actions={
          <>
            <span className="hidden text-xs text-zinc-500 sm:inline">{t("knowledge.total", { n: stats?.knowledge ?? "—" })}</span>
            <Button variant="secondary" icon={RefreshCw} loading={knowledge.loading && !!knowledge.data} onClick={reset}>
              {t("knowledge.refreshAll")}
            </Button>
          </>
        }
      />

      {topicList.length ? (
        <div className="mb-3 flex flex-wrap items-center gap-1.5">
          <span className="mr-0.5 text-xs font-medium text-zinc-500">{t("knowledge.topic")}</span>
          <button
            type="button"
            onClick={() => setTopic("")}
            className={cx(
              "rounded-full border px-3 py-1 text-xs font-medium transition",
              topic === ""
                ? "border-sky-500 bg-sky-50 text-sky-800 dark:bg-sky-500/10 dark:text-sky-200"
                : "border-zinc-200 text-zinc-600 hover:border-zinc-300 dark:border-zinc-700 dark:text-zinc-400",
            )}
          >
            {t("common.all")}
          </button>
          {topicList.map((entry) => (
            <button
              key={entry.topic}
              type="button"
              onClick={() => setTopic(entry.topic)}
              aria-pressed={topic === entry.topic}
              className={cx(
                "rounded-full border px-3 py-1 text-xs font-medium transition",
                topic === entry.topic
                  ? "border-sky-500 bg-sky-50 text-sky-800 dark:bg-sky-500/10 dark:text-sky-200"
                  : "border-zinc-200 text-zinc-600 hover:border-zinc-300 dark:border-zinc-700 dark:text-zinc-400",
              )}
            >
              {entry.topic}
              <span className="ml-1 tabular-nums opacity-60">{entry.knowledge_count}</span>
            </button>
          ))}
        </div>
      ) : null}

      <div className="flex flex-col gap-3 sm:flex-row">
        <form onSubmit={submit} className="relative flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-zinc-400" />
          <input
            type="search"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder={health?.embedding_configured ? t("knowledge.searchSemantic") : t("knowledge.searchKeyword")}
            aria-label={t("knowledge.searchAria")}
            className="field-input h-10 pr-20 pl-9"
          />
          <Button type="submit" size="sm" className="absolute top-1 right-1">
            {t("knowledge.search")}
          </Button>
        </form>
        <Segmented
          value={usage}
          onChange={setUsage}
          options={[
            { value: "all", label: t("common.all") },
            { value: "unused", label: t("knowledge.unused") },
            { value: "used", label: t("knowledge.used") },
          ]}
        />
      </div>

      <div className="mt-3 flex flex-wrap gap-1.5">
        {[{ id: "", label: t("knowledge.allKinds") }, ...taxonomy].map((entry) => (
          <button
            key={entry.id || "all"}
            type="button"
            onClick={() => setKind(entry.id)}
            className={cx(
              "rounded-full border px-3 py-1 text-xs font-medium transition",
              kind === entry.id
                ? "border-emerald-500 bg-emerald-50 text-emerald-800 dark:bg-emerald-500/10 dark:text-emerald-200"
                : "border-zinc-200 text-zinc-600 hover:border-zinc-300 dark:border-zinc-700 dark:text-zinc-400",
            )}
          >
            {entry.id ? kindLabel(entry.id, taxonomy) : entry.label}
          </button>
        ))}
      </div>

      {query ? (
        <p className="mt-4 flex items-center gap-2 text-sm text-zinc-500">
          {tx("knowledge.hits", {
            query: <span className="font-medium text-zinc-800 dark:text-zinc-200">{query}</span>,
            n: filtered.length,
          })}
          <span className="text-xs text-zinc-400">{health?.embedding_configured ? t("knowledge.modeBoth") : t("knowledge.modeKeyword")}</span>
          <button
            type="button"
            className="inline-flex items-center gap-0.5 text-emerald-700 hover:underline dark:text-emerald-400"
            onClick={() => {
              setDraft("");
              setQuery("");
            }}
          >
            <X className="size-3.5" />
            {t("knowledge.clear")}
          </button>
        </p>
      ) : null}

      <div className="mt-5">
        {knowledge.error ? <ErrorNotice error={knowledge.error} className="mb-4" /> : null}
        {knowledge.loading && !knowledge.data ? (
          <ListSkeleton rows={4} />
        ) : !filtered.length && !knowledge.error ? (
          <Empty
            icon={BookOpen}
            title={
              query
                ? t("knowledge.emptyQuery")
                : usage === "unused"
                  ? t("knowledge.emptyAllUsed")
                  : usage === "used"
                    ? t("knowledge.emptyNoneUsed")
                    : kind
                      ? t("knowledge.emptyKind")
                      : topic
                        ? t("knowledge.emptyTopic", { topic })
                        : t("knowledge.emptyAll")
            }
            description={
              query
                ? t("knowledge.emptyQueryDesc")
                : usage === "all" && !kind && !topic
                  ? t("knowledge.emptyAllDesc")
                  : t("knowledge.emptyFilterDesc")
            }
            action={
              query || usage !== "all" || kind || topic ? null : (
                <Button variant="secondary" onClick={() => navigate("upload")}>
                  {t("common.goImport")}
                </Button>
              )
            }
          />
        ) : (
          <div>
            <div className="grid gap-3 xl:grid-cols-2">
              {filtered.map((item) => (
                <KnowledgeCard
                  key={item.id}
                  item={item}
                  onCreate={createFrom}
                  onChanged={bumpKnowledge}
                />
              ))}
            </div>
            {!isSearch && items.length ? <Pager page={page} pageSize={PAGE} total={total} onPage={setPage} /> : null}
          </div>
        )}
      </div>
    </>
  );
}
