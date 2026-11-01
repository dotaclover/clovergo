import { isMessageKey, locale, t } from "./i18n";
import { DEFAULT_PLATFORM } from "./nav";

export function formatDate(value?: string | null): string {
  if (!value) return t("format.dateUnknown");
  const normalized = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(value) ? `${value.replace(" ", "T")}Z` : value;
  const date = new Date(normalized);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString(locale(), { hour12: false });
}

export function formatNumber(value?: number | null): string {
  return typeof value === "number" && Number.isFinite(value) ? value.toLocaleString(locale()) : "—";
}

/** Looks up `${prefix}.${id}` in the dictionary; undefined when there is no such key. */
function lookup(prefix: string, id?: string): string | undefined {
  if (!id) return undefined;
  const key = `${prefix}.${id}`;
  return isMessageKey(key) ? t(key) : undefined;
}

export const statusLabel = (status?: string) => lookup("status", status) || status || t("common.unknown");

/**
 * Labels a knowledge kind. The taxonomy is fixed in the server's domain.Kinds
 * and delivered by /api/v1/kinds; the fallback table only covers the moment
 * before that request resolves.
 */
export function kindLabel(kind?: string, kinds?: Array<{ id: string; label: string }> | null): string {
  if (!kind) return t("kind.default");
  const known = Array.isArray(kinds) ? kinds.find((item) => item.id === kind)?.label : undefined;
  return known || (kind !== "default" ? lookup("kind", kind) : undefined) || kind;
}

export const platformLabel = (platform?: string) =>
  (platform !== "unknown" ? lookup("platform", platform) : undefined) || platform || t("platform.unknown");

export const formatLabel = (format?: string) =>
  (format === "article" ? t("format.article") : format === "gallery" ? t("format.gallery") : undefined) ||
  format ||
  t("format.draft");

export function platformTone(platform?: string): "red" | "blue" | "violet" | "green" | "neutral" {
  return platform === DEFAULT_PLATFORM.id ? "green" : "neutral";
}

/** The manual-publishing reminder shown on every draft screen. */
export const draftDisclaimer = () => t("draft.disclaimer");
