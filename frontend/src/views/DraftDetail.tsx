import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowLeft,
  Download,
  FileText,
  Plus,
  Save,
  Send,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import { api, deleteJSON, downloadFile, putJSON, withQuery } from "../lib/api";
import { toastError, useApp } from "../lib/context";
import { formatDate, formatLabel, platformLabel, platformTone } from "../lib/format";
import { t } from "../lib/i18n";
import { DEFAULT_PLATFORM } from "../lib/nav";
import type { ArticleContent, ArticleSection, GalleryContent, MediaDraft, PlatformID } from "../lib/types";
import { ArticleView, GalleryView, draftFullText, draftTitle } from "../components/draftContent";
import {
  Badge,
  Button,
  Card,
  CardHeader,
  CopyButton,
  Empty,
  ErrorNotice,
  Field,
  ListSkeleton,
  LongTask,
  Notice,
  PageHeader,
} from "../components/ui";

/**
 * One draft on its own page. A draft that is still being written shows its
 * progress; a finished one becomes an editor, and the same page is where the
 * owner pushes it to the Official Account's draft box or deletes it.
 */
export default function DraftDetailView({ platform: platformID }: { platform: PlatformID }) {
  const { focusTarget, jumpTo, notify } = useApp();
  const listView = DEFAULT_PLATFORM.list;
  const id = useMemo(() => {
    const match = /^draft:(\d+)$/.exec(focusTarget);
    return match ? Number(match[1]) : 0;
  }, [focusTarget]);

  const [draft, setDraft] = useState<MediaDraft | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [edit, setEdit] = useState<ArticleContent | null>(null);
  const [galleryEdit, setGalleryEdit] = useState<GalleryContent | null>(null);
  const [tagsText, setTagsText] = useState("");
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const latestID = useRef(id);
  latestID.current = id;

  const load = useCallback(
    async (silent = false) => {
      if (!id) {
        setLoading(false);
        return;
      }
      if (!silent) setLoading(true);
      try {
        const next = await api<MediaDraft>(`/api/v1/media-drafts/${id}`);
        // A response for a draft the page has already left is dropped.
        if (latestID.current !== id) return;
        setDraft(next);
        setError(null);
      } catch (reason) {
        // A failed background poll keeps the page; the next tick retries.
        if (latestID.current === id && !silent) setError(reason);
      } finally {
        if (!silent && latestID.current === id) setLoading(false);
      }
    },
    [id],
  );

  useEffect(() => {
    setDraft(null);
    setEdit(null);
    setGalleryEdit(null);
    void load();
  }, [load]);

  // The article is written in the background, so a running draft is polled
  // until it either becomes editable or fails.
  useEffect(() => {
    if (draft?.status !== "running") return;
    const timer = window.setInterval(() => void load(true), 2500);
    return () => window.clearInterval(timer);
  }, [draft?.status, load]);

  useEffect(() => {
    if (draft?.status === "generated" && draft.gallery) {
      const gallery = draft.gallery;
      setGalleryEdit({ ...gallery, images: (gallery.images ?? []).map((image) => ({ ...image })) });
      setEdit(null);
    } else if (draft?.status === "generated" && draft.article) {
      const article = draft.article;
      setEdit({ ...article, sections: article.sections.map((section) => ({ ...section })) });
      setTagsText((article.tags ?? []).join("，"));
      setGalleryEdit(null);
    } else {
      setEdit(null);
      setGalleryEdit(null);
    }
  }, [draft?.id, draft?.status]);

  const backToList = () => jumpTo(listView);

  // Pushing sends the saved draft, so unsaved edits block it until saved.
  const dirty = useMemo(() => {
    if (!draft) return false;
    if (galleryEdit && draft.gallery) {
      return (
        sameContent(
          { ...galleryEdit, images: galleryEdit.images.map((image, index) => ({ ...image, order: index + 1 })) },
          draft.gallery,
        ) === false
      );
    }
    if (edit && draft.article) {
      return (
        sameContent(
          {
            ...edit,
            sections: edit.sections.map((section, index) => ({ ...section, order: index + 1 })),
            tags: splitTags(tagsText),
          },
          draft.article,
        ) === false
      );
    }
    return false;
  }, [draft, edit, galleryEdit, tagsText]);

  const save = async () => {
    if (!edit && !galleryEdit) return;
    setSaving(true);
    try {
      let next: MediaDraft;
      if (galleryEdit) {
        const gallery: GalleryContent = {
          ...galleryEdit,
          images: galleryEdit.images.map((image, index) => ({ ...image, order: index + 1 })),
        };
        next = await putJSON<MediaDraft>(`/api/v1/media-drafts/${id}`, { gallery });
        const saved = next.gallery ?? gallery;
        setGalleryEdit({ ...saved, images: (saved.images ?? []).map((image) => ({ ...image })) });
      } else if (edit) {
        const article: ArticleContent = {
          ...edit,
          sections: edit.sections.map((section, index) => ({ ...section, order: index + 1 })),
          tags: splitTags(tagsText),
        };
        next = await putJSON<MediaDraft>(`/api/v1/media-drafts/${id}`, { article });
        const saved = next.article ?? article;
        setEdit({ ...saved, sections: saved.sections.map((section) => ({ ...section })) });
        setTagsText((saved.tags ?? []).join("，"));
      } else {
        return;
      }
      setDraft(next);
      notify({ kind: "success", title: t("draftDetail.saved") });
    } catch (reason) {
      toastError(notify, reason, t("draftDetail.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    if (!window.confirm(t("draftDetail.confirmDelete", { id }))) return;
    setDeleting(true);
    try {
      await deleteJSON(`/api/v1/media-drafts/${id}`);
      notify({ kind: "success", title: t("draftDetail.deleted", { id }) });
      backToList();
    } catch (reason) {
      toastError(notify, reason, t("common.deleteFailed"));
    } finally {
      setDeleting(false);
    }
  };

  const [cover, setCover] = useState<File | null>(null);
  const [uploads, setUploads] = useState<Record<number, File>>({});

  useEffect(() => {
    setCover(null);
    setUploads({});
  }, [id]);

  const setUpload = (slot: number, file: File | null) => {
    setUploads((current) => {
      const next = { ...current };
      if (file) next[slot] = file;
      else delete next[slot];
      return next;
    });
  };

  const removeImage = (index: number) => {
    setGalleryEdit((current) =>
      current
        ? {
            ...current,
            images: (current.images ?? [])
              .filter((_, at) => at !== index)
              .map((image, at) => ({ ...image, order: at + 1 })),
          }
        : current,
    );
    setUploads((current) => {
      const next: Record<number, File> = {};
      for (const [key, file] of Object.entries(current)) {
        const slot = Number(key);
        if (slot === index + 1) continue;
        next[slot > index + 1 ? slot - 1 : slot] = file;
      }
      return next;
    });
  };

  const patchSection = (index: number, patch: Partial<ArticleSection>) => {
    setEdit((current) =>
      current
        ? { ...current, sections: current.sections.map((section, at) => (at === index ? { ...section, ...patch } : section)) }
        : current,
    );
  };

  const addSection = () => {
    setEdit((current) =>
      current
        ? {
            ...current,
            sections: [
              ...current.sections,
              { order: current.sections.length + 1, heading: "", body: "", image_prompt: "", image_caption: "" },
            ],
          }
        : current,
    );
  };

  const removeSection = (index: number) => {
    setEdit((current) =>
      current
        ? {
            ...current,
            sections: current.sections
              .filter((_, at) => at !== index)
              .map((section, at) => ({ ...section, order: at + 1 })),
          }
        : current,
    );
  };

  return (
    <>
      <PageHeader
        title={draft ? draftTitle(draft) || draft.topic : t("draftDetail.title")}
        description={
          draft ? (
            <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <span>{t("drafts.meta", { id: draft.id, format: formatLabel(draft.format), date: formatDate(draft.created_at) })}</span>
              <span>{t("drafts.topic", { topic: draft.topic })}</span>
              {draft.angle ? <span>{t("drafts.angle", { angle: draft.angle })}</span> : null}
            </span>
          ) : (
            t("draftDetail.backHint")
          )
        }
        actions={
          <Button variant="secondary" icon={ArrowLeft} onClick={backToList}>
            {t("draftDetail.back")}
          </Button>
        }
      />

      {!id ? (
        <Empty icon={FileText} title={t("draftDetail.noID")} description={t("draftDetail.noIDDesc")} />
      ) : loading && !draft ? (
        <Card className="mt-5">
          <div className="p-5">
            <ListSkeleton rows={5} />
          </div>
        </Card>
      ) : error && !draft ? (
        <Card className="mt-5">
          <div className="p-5">
            <ErrorNotice error={error} />
            <div className="mt-3">
              <Button variant="secondary" onClick={backToList}>
                {t("draftDetail.back")}
              </Button>
            </div>
          </div>
        </Card>
      ) : draft ? (
        <div className="mt-5 space-y-5">
          <Card>
            <CardHeader
              icon={FileText}
              title={
                <span className="flex flex-wrap items-center gap-2">
                  <Badge tone={platformTone(draft.platform)}>{platformLabel(platformID)}</Badge>
                  <span className="break-words">{draftTitle(draft) || draft.topic}</span>
                  {draft.status === "running" ? <Badge tone="blue">{t("drafts.statusRunning")}</Badge> : null}
                  {draft.status === "failed" ? <Badge tone="red">{t("drafts.statusFailed")}</Badge> : null}
                  {draft.status === "generated" && draft.wechat_pushed_at ? (
                    <Badge tone="green">{t("drafts.statusPushed")}</Badge>
                  ) : null}
                </span>
              }
              actions={
                <div className="flex flex-wrap items-center gap-2">
                  {draft.status === "generated" && (draft.article || draft.gallery) ? (
                    <>
                      <CopyButton text={draftFullText(draft)} label={t("common.copyAll")} />
                      <Button
                        variant="secondary"
                        size="sm"
                        icon={Download}
                        onClick={() =>
                          downloadFile(`/api/v1/media-drafts/${draft.id}/export`, `draft-${draft.id}.zip`).catch((reason) => toastError(notify, reason))
                        }
                      >
                        {t("draftDetail.exportZip")}
                      </Button>
                    </>
                  ) : null}
                  <Button variant="ghost" size="sm" icon={Trash2} loading={deleting} disabled={draft.status === "running"} onClick={() => void remove()}>
                    {t("common.delete")}
                  </Button>
                </div>
              }
            />
            <div className="space-y-4 p-5">
              {draft.status === "running" ? <Notice tone="info">{t("drafts.generatingHint")}</Notice> : null}
              {draft.status === "failed" ? (
                <Notice tone="warning">
                  {t("drafts.failedHint")}
                  {draft.error_message ? <span className="mt-1 block break-words text-xs">{draft.error_message}</span> : null}
                </Notice>
              ) : null}
              {error ? <ErrorNotice error={error} /> : null}
              {draft.status === "generated" && draft.article && edit ? (
                <ArticleEditor
                  article={edit}
                  tagsText={tagsText}
                  saving={saving}
                  cover={cover}
                  onCover={setCover}
                  onArticle={(next) => setEdit(next)}
                  onSection={(index, patch) => patchSection(index, patch)}
                  onAddSection={addSection}
                  onRemoveSection={removeSection}
                  onTags={setTagsText}
                  onSave={() => void save()}
                />
              ) : null}
              {draft.status === "generated" && galleryEdit ? (
                <GalleryEditor
                  gallery={galleryEdit}
                  saving={saving}
                  uploads={uploads}
                  onChange={setGalleryEdit}
                  onUpload={setUpload}
                  onRemoveImage={removeImage}
                  onSave={() => void save()}
                />
              ) : null}
              {draft.status === "generated" && (draft.article || draft.gallery) ? (
                <details className="rounded-xl border border-zinc-200 p-4 dark:border-zinc-800">
                  <summary className="cursor-pointer list-none text-sm font-semibold text-zinc-700 [&::-webkit-details-marker]:hidden dark:text-zinc-300">
                    {t("draftDetail.preview")}
                  </summary>
                  <div className="mt-4">
                    {draft.gallery ? <GalleryView gallery={draft.gallery} /> : draft.article ? <ArticleView article={draft.article} /> : null}
                  </div>
                </details>
              ) : null}
            </div>
          </Card>

          {draft.status === "generated" ? (
            <WechatPush draft={draft} cover={cover} uploads={uploads} dirty={dirty} onPushed={setDraft} />
          ) : null}
        </div>
      ) : null}
    </>
  );
}

function splitTags(value: string): string[] {
  return value
    .split(/[,，\n]/)
    .map((tag) => tag.trim().replace(/^#+/, ""))
    .filter(Boolean);
}

/** Compares two contents, treating empty strings, null and empty lists as absent. */
function sameContent(a: unknown, b: unknown): boolean {
  const normalize = (value: unknown) =>
    JSON.stringify(value, (_key, item: unknown) => {
      if (item === "" || item === null || (Array.isArray(item) && item.length === 0)) return undefined;
      if (item && typeof item === "object" && !Array.isArray(item)) {
        return Object.fromEntries(Object.entries(item as Record<string, unknown>).sort(([x], [y]) => x.localeCompare(y)));
      }
      return item;
    });
  return normalize(a) === normalize(b);
}

function ArticleEditor({
  article,
  tagsText,
  saving,
  cover,
  onCover,
  onArticle,
  onSection,
  onAddSection,
  onRemoveSection,
  onTags,
  onSave,
}: {
  article: ArticleContent;
  tagsText: string;
  saving: boolean;
  cover: File | null;
  onCover: (file: File | null) => void;
  onArticle: (article: ArticleContent) => void;
  onSection: (index: number, patch: Partial<ArticleSection>) => void;
  onAddSection: () => void;
  onRemoveSection: (index: number) => void;
  onTags: (value: string) => void;
  onSave: () => void;
}) {
  const { health } = useApp();
  const defaultCover = Boolean(health?.wechat_default_cover);
  const sections = article.sections ?? [];
  return (
    <div className="space-y-4">
      <div className="grid gap-4 md:grid-cols-2">
        <Field label={t("draftDetail.fieldTitle")}>
          <input className="field-input" value={article.title} disabled={saving} onChange={(event) => onArticle({ ...article, title: event.target.value })} />
        </Field>
        <Field label={t("draftDetail.fieldTags")} hint={t("draftDetail.fieldTagsHint")}>
          <input className="field-input" value={tagsText} disabled={saving} onChange={(event) => onTags(event.target.value)} />
        </Field>
      </div>
      <Field label={t("draftDetail.fieldSummary")}>
        <textarea className="field-input" rows={2} value={article.summary} disabled={saving} onChange={(event) => onArticle({ ...article, summary: event.target.value })} />
      </Field>
      <Field label={t("draftDetail.fieldLead")}>
        <textarea className="field-input" rows={4} value={article.lead} disabled={saving} onChange={(event) => onArticle({ ...article, lead: event.target.value })} />
      </Field>
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <Field
            label={t("draftDetail.fieldCover")}
            hint={defaultCover ? `${t("push.coverHint")} ${t("push.coverOptional")}` : t("push.coverHint")}
          >
            <textarea className="field-input" rows={4} value={article.cover_image_prompt} disabled={saving} onChange={(event) => onArticle({ ...article, cover_image_prompt: event.target.value })} />
          </Field>
        </div>
        <ImageSlot label={t("draftDetail.fieldCover")} file={cover} title={article.title} index={1} total={1} disabled={saving} onChange={onCover} />
      </div>

      <div className="space-y-3">
        <div className="flex items-center justify-between gap-2">
          <p className="text-sm font-medium text-zinc-700 dark:text-zinc-300">{t("draftDetail.sections")}</p>
          <Button variant="secondary" size="sm" icon={Plus} disabled={saving} onClick={onAddSection}>
            {t("draftDetail.addSection")}
          </Button>
        </div>
        {sections.map((section, index) => (
          <div key={index} className="space-y-2 rounded-xl border border-zinc-200 p-3 dark:border-zinc-800">
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs font-semibold text-zinc-500">{t("draftDetail.section", { n: index + 1 })}</span>
              <Button variant="ghost" size="sm" icon={X} disabled={saving} onClick={() => onRemoveSection(index)} aria-label={t("draftDetail.removeSection")} />
            </div>
            <Field label={t("draftDetail.fieldHeading")}>
              <input className="field-input" value={section.heading} disabled={saving} onChange={(event) => onSection(index, { heading: event.target.value })} />
            </Field>
            <Field label={t("draftDetail.fieldBody")}>
              <textarea className="field-input" rows={6} value={section.body} disabled={saving} onChange={(event) => onSection(index, { body: event.target.value })} />
            </Field>
            <div className="grid gap-2 md:grid-cols-2">
              <Field label={t("draftDetail.fieldImagePrompt")}>
                <input className="field-input" value={section.image_prompt ?? ""} disabled={saving} onChange={(event) => onSection(index, { image_prompt: event.target.value })} />
              </Field>
              <Field label={t("draftDetail.fieldImageCaption")}>
                <input className="field-input" value={section.image_caption ?? ""} disabled={saving} onChange={(event) => onSection(index, { image_caption: event.target.value })} />
              </Field>
            </div>
          </div>
        ))}
      </div>

      <Field label={t("draftDetail.fieldConclusion")}>
        <textarea className="field-input" rows={4} value={article.conclusion} disabled={saving} onChange={(event) => onArticle({ ...article, conclusion: event.target.value })} />
      </Field>

      <div className="flex justify-end border-t border-zinc-100 pt-4 dark:border-zinc-800">
        <Button icon={Save} loading={saving} onClick={onSave}>
          {t("common.save")}
        </Button>
      </div>
    </div>
  );
}

/** Editor for a picture post: title, short caption and the ordered image prompts. */
function GalleryEditor({
  gallery,
  saving,
  uploads,
  onChange,
  onUpload,
  onRemoveImage,
  onSave,
}: {
  gallery: GalleryContent;
  saving: boolean;
  uploads: Record<number, File>;
  onChange: (gallery: GalleryContent) => void;
  onUpload: (slot: number, file: File | null) => void;
  onRemoveImage: (index: number) => void;
  onSave: () => void;
}) {
  const images = gallery.images ?? [];
  const { platforms } = useApp();
  const imageMax = platforms.find((item) => item.id === DEFAULT_PLATFORM.id)?.gallery_image_max ?? images.length;
  return (
    <div className="space-y-4">
      <Field label={t("draftDetail.fieldTitle")}>
        <input
          className="field-input"
          value={gallery.title}
          disabled={saving}
          onChange={(event) => onChange({ ...gallery, title: event.target.value })}
        />
      </Field>
      <Field label={t("draftDetail.fieldCaption")}>
        <textarea
          className="field-input"
          rows={5}
          value={gallery.caption}
          disabled={saving}
          onChange={(event) => onChange({ ...gallery, caption: event.target.value })}
        />
      </Field>

      <div className="space-y-3">
        <div className="flex items-center justify-between gap-2">
          <p className="text-sm font-medium text-zinc-700 dark:text-zinc-300">{t("draftDetail.images")}</p>
          <Button
            variant="secondary"
            size="sm"
            icon={Plus}
            disabled={saving || images.length >= imageMax}
            onClick={() => onChange({ ...gallery, images: [...images, { order: images.length + 1, prompt: "" }] })}
          >
            {t("draftDetail.addImage")}
          </Button>
        </div>
        {images.map((image, index) => (
          <div key={index} className="space-y-2 rounded-xl border border-zinc-200 p-3 dark:border-zinc-800">
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs font-semibold text-zinc-500">
                {t(index === 0 ? "draftDetail.imageCover" : "draftDetail.imageN", { n: index + 1 })}
              </span>
              <Button
                variant="ghost"
                size="sm"
                icon={X}
                disabled={saving}
                onClick={() => onRemoveImage(index)}
                aria-label={t("draftDetail.removeImage")}
              />
            </div>
            <div className="flex items-start gap-3">
              <div className="min-w-0 flex-1">
                <Field label={t("draftDetail.fieldGalleryImagePrompt")}>
                  <textarea
                    className="field-input"
                    rows={4}
                    value={image.prompt}
                    disabled={saving}
                    onChange={(event) =>
                      onChange({
                        ...gallery,
                        images: images.map((item, at) => (at === index ? { ...item, prompt: event.target.value } : item)),
                      })
                    }
                  />
                </Field>
              </div>
              <ImageSlot
                label={t("draftDetail.imageN", { n: index + 1 })}
                file={uploads[index + 1] ?? null}
                title={gallery.title}
                index={index + 1}
                total={images.length}
                disabled={saving}
                onChange={(file) => onUpload(index + 1, file)}
              />
            </div>
          </div>
        ))}
        <p className="text-xs text-zinc-500">{t("push.imagesHint")}</p>
      </div>

      <div className="flex justify-end border-t border-zinc-100 pt-4 dark:border-zinc-800">
        <Button icon={Save} loading={saving} onClick={onSave}>
          {t("common.save")}
        </Button>
      </div>
    </div>
  );
}

/**
 * Pushes the draft into the Official Account's draft box; the owner may also
 * choose to submit it for publishing right after (needs the account's permission).
 */
function WechatPush({
  draft,
  cover,
  uploads,
  dirty,
  onPushed,
}: {
  draft: MediaDraft;
  cover: File | null;
  uploads: Record<number, File>;
  dirty: boolean;
  onPushed: (draft: MediaDraft) => void;
}) {
  const { health, notify, jumpTo } = useApp();
  const configView = DEFAULT_PLATFORM.config;
  const [pushing, setPushing] = useState<"draft" | "publish" | null>(null);
  const [error, setError] = useState<unknown>(null);
  const configured = Boolean(health?.wechat_configured);
  const defaultCover = Boolean(health?.wechat_default_cover);
  const isGallery = draft.format === "gallery";
  const imageCount = draft.gallery?.images?.length ?? 0;
  const needCover = !isGallery && !cover && !defaultCover;

  const push = async (publish: boolean) => {
    if (publish && !window.confirm(t("push.confirmPublish"))) return;
    setPushing(publish ? "publish" : "draft");
    setError(null);
    try {
      let init: RequestInit = { method: "POST" };
      const form = new FormData();
      if (!isGallery && cover) form.append("cover", cover);
      if (isGallery) {
        for (let slot = 1; slot <= imageCount; slot++) {
          if (uploads[slot]) form.append(`image${slot}`, uploads[slot]);
        }
      }
      if (Array.from(form.keys()).length > 0) init = { method: "POST", body: form };
      const next = await api<MediaDraft>(
        `/api/v1/media-drafts/${draft.id}/wechat${publish ? "?mode=publish" : ""}`,
        init,
      );
      notify({ kind: "success", title: publish ? t("push.donePublish") : t("push.done") });
      onPushed(next);
    } catch (reason) {
      setError(reason);
      toastError(notify, reason, t("push.failed"));
    } finally {
      setPushing(null);
    }
  };

  return (
    <Card>
      <CardHeader icon={Send} title={t("push.title")} description={t("push.hint")} />
      <div className="space-y-4 p-5">
        {draft.wechat_pushed_at ? (
          <Notice tone="success">
            <span className="block">{t("push.pushedAt", { date: formatDate(draft.wechat_pushed_at) })}</span>
            {draft.wechat_media_id ? (
              <span className="mt-1 block text-xs break-all">
                {t("push.mediaID", { id: draft.wechat_media_id })}
              </span>
            ) : null}
            {draft.wechat_published_at ? (
              <span className="mt-1 block text-xs break-all">
                {t("push.publishedAt", { date: formatDate(draft.wechat_published_at) })}
                {draft.wechat_publish_id ? ` · ${t("push.publishID", { id: draft.wechat_publish_id })}` : ""}
              </span>
            ) : null}
          </Notice>
        ) : (
          <p className="text-xs text-zinc-500">{t("push.notPushed")}</p>
        )}
        {!configured ? (
          <p className="text-xs text-amber-700 dark:text-amber-400">
            {t("push.notConfigured")}{" "}
            <button
              type="button"
              className="underline"
              onClick={() => {
                notify({ kind: "info", title: t("drafts.pushNeedConfig") });
                jumpTo(configView, "wechat");
              }}
            >
              {t("push.goSettings")}
            </button>
          </p>
        ) : (
          <>
            {needCover ? <p className="text-xs text-amber-700 dark:text-amber-400">{t("push.needCover")}</p> : null}
            {dirty ? <p className="text-xs text-amber-700 dark:text-amber-400">{t("push.unsavedEdits")}</p> : null}
            {pushing ? <LongTask active title={t(pushing === "publish" ? "push.publishing" : "push.pushing")} /> : null}
            <div className="flex flex-wrap justify-end gap-2">
              <Button
                size="sm"
                variant="secondary"
                icon={Send}
                loading={pushing === "draft"}
                disabled={needCover || dirty || pushing !== null}
                onClick={() => void push(false)}
              >
                {draft.wechat_pushed_at ? t("push.repush") : t("push.submit")}
              </Button>
              <Button
                size="sm"
                icon={Send}
                loading={pushing === "publish"}
                disabled={needCover || dirty || pushing !== null}
                onClick={() => void push(true)}
              >
                {t("push.submitPublish")}
              </Button>
            </div>
          </>
        )}
        {error ? <ErrorNotice error={error} /> : null}
      </div>
    </Card>
  );
}

/**
 * One picture slot. Without an upload it previews the title card that is
 * pushed in its place (rendered by the server, so the two always match); with
 * one, the server resizes the upload to fit the card.
 */
function ImageSlot({
  label,
  file,
  title,
  index,
  total,
  disabled,
  onChange,
}: {
  label: string;
  file: File | null;
  title: string;
  index: number;
  total: number;
  disabled: boolean;
  onChange: (file: File | null) => void;
}) {
  const [preview, setPreview] = useState("");
  useEffect(() => {
    if (!file) {
      setPreview("");
      return;
    }
    const url = URL.createObjectURL(file);
    setPreview(url);
    return () => URL.revokeObjectURL(url);
  }, [file]);
  const [card, setCard] = useState("");
  useEffect(() => {
    if (file) return;
    let alive = true;
    const timer = window.setTimeout(() => {
      api<{ svg: string }>(withQuery("/api/v1/imaging/card", { title, index, total }))
        .then((data) => alive && setCard(`data:image/svg+xml;charset=utf-8,${encodeURIComponent(data.svg)}`))
        .catch(() => alive && setCard(""));
    }, 300);
    return () => {
      alive = false;
      window.clearTimeout(timer);
    };
  }, [file, title, index, total]);
  const shown = preview || card;
  return (
    <div className="w-24 flex-none sm:w-28">
      <div className="relative flex aspect-[3/4] items-center justify-center overflow-hidden rounded-lg border border-zinc-300 bg-zinc-50 dark:border-zinc-700 dark:bg-zinc-900">
        {shown ? <img src={shown} alt={label} className="h-full w-full object-cover" /> : null}
        {file ? (
          <button
            type="button"
            disabled={disabled}
            onClick={() => onChange(null)}
            aria-label={t("push.clearImage")}
            className="absolute top-1.5 right-1.5 rounded-full bg-black/60 p-1 text-white transition hover:bg-black/80"
          >
            <X className="size-3.5" aria-hidden />
          </button>
        ) : (
          <label className="absolute inset-x-0 bottom-0 flex cursor-pointer items-center justify-center gap-1 bg-black/55 py-1.5 text-xs text-white transition hover:bg-black/75">
            <Upload className="size-3.5" aria-hidden />
            {t("push.pickImage")}
            <input
              type="file"
              accept="image/jpeg,image/png,image/gif"
              className="sr-only"
              disabled={disabled}
              onChange={(event) => onChange(event.target.files?.[0] ?? null)}
            />
          </label>
        )}
      </div>
    </div>
  );
}

