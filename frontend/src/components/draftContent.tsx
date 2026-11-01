import type { ReactNode } from "react";
import type { ArticleContent, GalleryContent, MediaDraft } from "../lib/types";
import { t } from "../lib/i18n";
import { Badge, CopyButton, cx } from "./ui";

/**
 * The draft content renderers. One renderer per shape means the detail page's
 * preview can never show something the generator would not produce.
 */

export const asArray = <T,>(value: T[] | null | undefined): T[] => (Array.isArray(value) ? value : []);

export function draftTitle(draft: MediaDraft): string {
  return draft.article?.title || draft.gallery?.title || "";
}

/** The text a creator copies into the WeChat editor, without production notes. */
export function draftFullText(draft: MediaDraft): string {
  if (draft.gallery) {
    return [draft.gallery.title, draft.gallery.caption].filter(Boolean).join("\n\n");
  }
  const article = draft.article;
  if (!article) return "";
  return [
    article.title,
    article.summary,
    article.lead,
    articleBodyText(article),
    `${t("article.conclusion")}\n\n${article.conclusion}`,
    asArray(article.tags).join(t("common.listSep")),
  ]
    .filter(Boolean)
    .join("\n\n");
}

/** The chapters alone, which is what gets pasted between the lead and the end. */
export function articleBodyText(article: ArticleContent): string {
  return asArray(article.sections)
    .map((section) => `${section.heading}\n\n${section.body}`)
    .join("\n\n");
}

export function Section({ title, copy, children }: { title: string; copy?: string; children: ReactNode }) {
  return (
    <section>
      <div className="mb-1.5 flex items-center justify-between gap-2">
        <h3 className="text-xs font-semibold tracking-wide text-zinc-500 dark:text-zinc-400">{title}</h3>
        {copy ? <CopyButton text={copy} /> : null}
      </div>
      {children}
    </section>
  );
}

const textBlock = "rounded-lg bg-zinc-50 px-3.5 py-2.5 text-sm leading-relaxed whitespace-pre-wrap break-words dark:bg-zinc-800/50";

/**
 * Tags are WeChat "合集"/labels rather than hashtags, so they are shown as
 * plain words: adding a # would misrepresent what the platform expects.
 */
function Tags({ tags }: { tags: string[] }) {
  const list = asArray(tags);
  if (!list.length) return <p className="text-xs text-zinc-500">{t("common.none")}</p>;
  return (
    <div className="flex flex-wrap gap-1.5">
      {list.map((tag, index) => (
        <Badge key={`${tag}-${index}`} tone="green">
          {tag}
        </Badge>
      ))}
    </div>
  );
}

/** A picture post shown as the caption next to its ordered image prompts. */
export function GalleryView({ gallery }: { gallery: GalleryContent }) {
  const images = asArray(gallery.images);
  return (
    <div className="grid gap-5 lg:grid-cols-[1fr_20rem]">
      <div className="space-y-4">
        <Section title={t("gallery.title")} copy={gallery.title}>
          <p className="text-lg font-semibold break-words">{gallery.title}</p>
        </Section>
        <Section title={t("gallery.caption")} copy={gallery.caption}>
          <p className={textBlock}>{gallery.caption}</p>
        </Section>
      </div>
      <Section title={t("gallery.images", { n: images.length })}>
        <ul className="space-y-2">
          {images.map((image, index) => (
            <li key={image.order} className="rounded-lg border border-zinc-200 p-3 dark:border-zinc-800">
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs font-semibold text-zinc-500">{t("draftDetail.imageN", { n: index + 1 })}</span>
                <CopyButton text={image.prompt} label={t("article.copyPrompt")} />
              </div>
              <p className="mt-1 text-xs break-words text-zinc-600 dark:text-zinc-300">{image.prompt}</p>
            </li>
          ))}
        </ul>
        <p className="mt-2 text-xs text-zinc-500">{t("gallery.note")}</p>
      </Section>
    </div>
  );
}

export function ArticleView({ article }: { article: ArticleContent }) {
  const sections = asArray(article.sections);
  const withImage = sections.filter((section) => (section.image_prompt || "").trim().length > 0);
  return (
    <div className="grid gap-5 lg:grid-cols-[1fr_20rem]">
      <div className="space-y-4">
        <Section title={t("article.title")} copy={article.title}>
          <p className="text-lg font-semibold break-words">{article.title}</p>
        </Section>
        <Section title={t("article.summary")} copy={article.summary}>
          <p className={textBlock}>{article.summary}</p>
        </Section>
        <Section title={t("article.lead")} copy={article.lead}>
          <p className={cx(textBlock, "text-zinc-700 dark:text-zinc-200")}>{article.lead}</p>
        </Section>
        <Section title={t("article.body", { n: sections.length })} copy={articleBodyText(article)}>
          <div className="space-y-3">
            {sections.map((section) => (
              <article key={section.order} className="rounded-lg border border-zinc-200 p-3.5 dark:border-zinc-800">
                <h4 className="flex items-start gap-2 text-sm font-semibold">
                  <Badge tone="green">{section.order}</Badge>
                  <span className="min-w-0 flex-1 break-words">{section.heading}</span>
                </h4>
                <p className={cx(textBlock, "mt-2")}>{section.body}</p>
                {(section.image_prompt || "").trim() ? (
                  <div className="mt-2 rounded-md bg-zinc-50 px-2.5 py-2 dark:bg-zinc-800/50">
                    <p className="text-xs break-words text-zinc-500 dark:text-zinc-400">
                      {t("article.imagePrompt", { text: section.image_prompt || "" })}
                    </p>
                    {(section.image_caption || "").trim() ? (
                      <p className="mt-1 text-xs break-words text-zinc-600 dark:text-zinc-300">
                        {t("article.caption", { text: section.image_caption || "" })}
                      </p>
                    ) : null}
                  </div>
                ) : null}
              </article>
            ))}
          </div>
        </Section>
        <Section title={t("article.conclusion")} copy={article.conclusion}>
          <p className={textBlock}>{article.conclusion}</p>
        </Section>
        <Section title={t("article.tags")} copy={asArray(article.tags).join(t("common.listSep"))}>
          <Tags tags={article.tags} />
        </Section>
      </div>
      <div className="space-y-3">
        <Section title={t("article.cover")} copy={article.cover_image_prompt}>
          <p className={cx(textBlock, "text-xs")}>{article.cover_image_prompt}</p>
        </Section>
        <Section title={t("article.sectionImages", { n: withImage.length })}>
          {withImage.length ? (
            <ul className="space-y-2">
              {withImage.map((section) => (
                <li key={section.order} className="rounded-lg border border-zinc-200 p-3 dark:border-zinc-800">
                  <div className="flex items-center justify-between gap-2">
                    <span className="min-w-0 flex-1 truncate text-sm font-medium">{section.heading}</span>
                    <CopyButton text={section.image_prompt || ""} label={t("article.copyPrompt")} />
                  </div>
                  <p className="mt-1 text-xs break-words text-zinc-500 dark:text-zinc-400">{section.image_prompt}</p>
                  {(section.image_caption || "").trim() ? (
                    <p className="mt-1 text-xs break-words text-zinc-600 dark:text-zinc-300">
                      {t("article.caption", { text: section.image_caption || "" })}
                    </p>
                  ) : null}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-xs text-zinc-500">{t("article.noImages")}</p>
          )}
        </Section>
      </div>
    </div>
  );
}
