import { useEffect, useRef, useState, type FormEvent } from "react";
import { PenLine } from "lucide-react";
import { api, postJSON, withQuery } from "../lib/api";
import { toastError, useApp } from "../lib/context";
import { platformLabel } from "../lib/format";
import { useAsync } from "../lib/hooks";
import { t } from "../lib/i18n";
import { DEFAULT_PLATFORM } from "../lib/nav";
import type { DocumentItem, DraftFormat, KnowledgeItem, MediaDraft, PlatformID } from "../lib/types";
import { Button, Card, PageHeader, Segmented } from "../components/ui";

type SourceMode = "none" | "random" | "knowledge" | "document";

/**
 * The draft screen: an optional inspiration and an optional source. Either one
 * alone is enough to write one article. A source is either a named knowledge
 * item, a material's original text, or a knowledge item picked by the app. The
 * knowledge and materials lists hand their item over through jumpTo.
 */
export default function DraftComposeView({ platform: platformID }: { platform: PlatformID }) {
  const { focusTarget, jumpTo, notify, platforms } = useApp();
  const detailView = DEFAULT_PLATFORM.detail;
  const platformInfo = platforms.find((item) => item.id === platformID);
  const imageMax = platformInfo?.gallery_image_max ?? 1;
  const imageDefault = platformInfo?.gallery_image_default ?? 1;

  const [format, setFormat] = useState<DraftFormat>("article");
  const [imageCount, setImageCount] = useState<number | null>(null);
  const [inspiration, setInspiration] = useState("");
  const [sourceMode, setSourceMode] = useState<SourceMode>("none");
  const [knowledgeID, setKnowledgeID] = useState<number | null>(null);
  const [documentID, setDocumentID] = useState<number | null>(null);
  const [running, setRunning] = useState(false);
  const previousPlatform = useRef<PlatformID | null>(null);

  const [knowledgeQuery, setKnowledgeQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedQuery(knowledgeQuery.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [knowledgeQuery]);

  // Without a keyword the newest 100 items are offered; a keyword goes through
  // the same retrieval as /search, so older knowledge stays reachable.
  const knowledgeList = useAsync(
    () =>
      sourceMode !== "knowledge"
        ? Promise.resolve([] as KnowledgeItem[])
        : debouncedQuery
          ? api<KnowledgeItem[]>(withQuery("/api/v1/search", { q: debouncedQuery, limit: 50 }))
          : api<{ items: KnowledgeItem[]; total: number }>(withQuery("/api/v1/knowledge", { limit: 100 })).then(
              (page) => page.items,
            ),
    [sourceMode, debouncedQuery],
  );
  const knowledgeOptions = Array.isArray(knowledgeList.data) ? knowledgeList.data : [];

  const documents = useAsync(
    () => (sourceMode === "document" ? api<DocumentItem[]>("/api/v1/documents") : Promise.resolve([])),
    [sourceMode],
  );
  const documentOptions = Array.isArray(documents.data) ? documents.data : [];

  // The knowledge list hands one item over as "knowledge:<id>", and the
  // materials list hands a whole material over as "document:<id>".
  useEffect(() => {
    const knowledgeMatch = /^knowledge:(\d+)$/.exec(focusTarget);
    const documentMatch = /^document:(\d+)$/.exec(focusTarget);
    if (knowledgeMatch) {
      setSourceMode("knowledge");
      setKnowledgeID(Number(knowledgeMatch[1]));
      setDocumentID(null);
    } else if (documentMatch) {
      setSourceMode("document");
      setDocumentID(Number(documentMatch[1]));
      setKnowledgeID(null);
    }
  }, [focusTarget]);

  // Reset only when the platform actually changes. On mount this effect runs
  // after the focusTarget effect, and an unconditional reset would clear the
  // source just selected by the materials or knowledge list.
  useEffect(() => {
    if (previousPlatform.current === null) {
      previousPlatform.current = platformID;
      return;
    }
    if (previousPlatform.current === platformID) return;
    previousPlatform.current = platformID;
    setFormat("article");
    setInspiration("");
    setSourceMode("none");
    setKnowledgeID(null);
    setDocumentID(null);
  }, [platformID]);

  const changeSourceMode = (mode: SourceMode) => {
    setSourceMode(mode);
    if (mode !== "knowledge") setKnowledgeID(null);
    if (mode !== "document") setDocumentID(null);
  };

  const hasSource =
    sourceMode === "random" ||
    (sourceMode === "knowledge" && knowledgeID !== null) ||
    (sourceMode === "document" && documentID !== null);
  const hasInspiration = Boolean(inspiration.trim());
  const canSubmit = hasInspiration || hasSource;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!canSubmit || running) return;
    setRunning(true);
    try {
      const draft = await postJSON<MediaDraft>("/api/v1/media-drafts", {
        platform: platformID,
        format,
        ...(format === "gallery" ? { image_count: imageCount ?? imageDefault } : {}),
        ...(inspiration.trim() ? { topic: inspiration.trim() } : {}),
        ...(sourceMode === "knowledge" && knowledgeID !== null ? { knowledge_ids: [knowledgeID] } : {}),
        ...(sourceMode === "document" && documentID !== null ? { document_ids: [documentID] } : {}),
        ...(sourceMode === "random" ? { random_knowledge: true } : {}),
      });
      notify({ kind: "success", title: t("compose.generated", { id: draft.id }), detail: t("compose.generatedDetail") });
      setInspiration("");
      jumpTo(detailView, `draft:${draft.id}`);
    } catch (reason) {
      toastError(notify, reason, t("compose.failed"));
    } finally {
      setRunning(false);
    }
  };

  return (
    <>
      <PageHeader title={t("compose.title", { platform: platformLabel(platformID) })} />

      <Card>
        <form onSubmit={submit} className="space-y-8 p-6">
          <section className="space-y-3">
            <h2 className="text-sm font-semibold">{t("compose.format")}</h2>
            <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
              <Segmented
                value={format}
                onChange={(value) => !running && setFormat(value)}
                options={[
                  { value: "article", label: t("compose.formatArticle") },
                  { value: "gallery", label: t("compose.formatGallery") },
                ]}
              />
              {format === "gallery" && imageMax > 1 ? (
                <div className="flex items-center gap-2">
                  <span className="text-sm text-zinc-500">{t("compose.imageCount")}</span>
                  <Segmented
                    value={String(imageCount ?? imageDefault)}
                    onChange={(value) => !running && setImageCount(Number(value))}
                    options={Array.from({ length: imageMax }, (_, index) => ({
                      value: String(index + 1),
                      label: String(index + 1),
                    }))}
                  />
                </div>
              ) : null}
            </div>
          </section>

          <section className="space-y-3">
            <h2 className="text-sm font-semibold">{t("compose.inspiration")}</h2>
            <textarea
              className="field-input min-h-28 resize-y"
              rows={4}
              value={inspiration}
              maxLength={500}
              disabled={running}
              onChange={(event) => setInspiration(event.target.value)}
              placeholder={t("compose.inspirationPlaceholder")}
              aria-label={t("compose.inspiration")}
            />
          </section>

          <section className="space-y-3">
            <h2 className="text-sm font-semibold">{t("compose.source")}</h2>
            <Segmented
              value={sourceMode}
              onChange={(value) => !running && changeSourceMode(value)}
              options={[
                { value: "none", label: t("compose.sourceNone") },
                { value: "random", label: t("compose.sourceRandom") },
                { value: "knowledge", label: t("compose.sourceKnowledge") },
                { value: "document", label: t("compose.sourceDocument") },
              ]}
            />

            {sourceMode === "knowledge" ? (
              <div className="grid gap-3 sm:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
                <input
                  className="field-input h-9"
                  value={knowledgeQuery}
                  disabled={running}
                  onChange={(event) => setKnowledgeQuery(event.target.value)}
                  placeholder={t("knowledgePicker.search")}
                  aria-label={t("knowledgePicker.search")}
                />
                <select
                  className="field-select"
                  value={knowledgeID ?? ""}
                  disabled={running || knowledgeList.loading}
                  onChange={(event) =>
                    setKnowledgeID(event.target.value === "" ? null : Number(event.target.value))
                  }
                  aria-label={t("compose.sourceKnowledge")}
                >
                  <option value="">{t("compose.pickKnowledge")}</option>
                  {knowledgeID !== null && !knowledgeOptions.some((item) => item.id === knowledgeID) ? (
                    <option value={knowledgeID}>#{knowledgeID}</option>
                  ) : null}
                  {knowledgeOptions.map((item) => (
                    <option key={item.id} value={item.id}>
                      #{item.id} {item.title}
                    </option>
                  ))}
                </select>
              </div>
            ) : null}

            {sourceMode === "document" ? (
              <select
                className="field-select"
                value={documentID ?? ""}
                disabled={running || documents.loading}
                onChange={(event) =>
                  setDocumentID(event.target.value === "" ? null : Number(event.target.value))
                }
                aria-label={t("compose.sourceDocument")}
              >
                <option value="">{t("compose.pickDocument")}</option>
                {documentID !== null && !documentOptions.some((item) => item.id === documentID) ? (
                  <option value={documentID}>#{documentID}</option>
                ) : null}
                {documentOptions.map((item) => (
                  <option key={item.id} value={item.id}>
                    #{item.id} {item.filename}
                  </option>
                ))}
              </select>
            ) : null}
          </section>

          <div className="border-t border-zinc-100 pt-5 dark:border-zinc-800">
            <Button type="submit" icon={PenLine} loading={running} disabled={!canSubmit}>
              {format === "gallery" ? t("compose.submitGallery") : t("compose.submit")}
            </Button>
          </div>
        </form>
      </Card>
    </>
  );
}
