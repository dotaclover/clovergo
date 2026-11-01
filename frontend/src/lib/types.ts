/** One entry of the fixed knowledge taxonomy, generated in code on the server. */
export interface KindInfo {
  id: string;
  label: string;
  hint: string;
}

/** A page of a list endpoint: the rows plus the total the pager needs. */
export interface Paged<T> {
  items: T[];
  total: number;
}

/** GET /auth/status: whether a global password is set and the stored token still opens it. */
export interface AuthStatus {
  enabled: boolean;
  unlocked: boolean;
}

export interface PromptLayer {
  /** Set on a layer that maps to an editable prompt layer. */
  key?: string;
  name: string;
  source: string;
  editable: boolean;
  edit_target: string | null;
  text: string;
}

/** One embedded Markdown prompt layer with the owner's replacement, if any. */
export interface PromptLayerEdit {
  key: string;
  name: string;
  default_text: string;
  text: string;
  revision: number;
  customized: boolean;
  updated_at: string;
}

export interface PromptView {
  call: string;
  title: string;
  version: string;
  sha256: string;
  text: string;
  layers: PromptLayer[];
}

export interface Health {
  status: string;
  fts_mode: string;
  ark_configured: boolean;
  ark_model: string;
  version?: string;
  embedding_configured?: boolean;
  embedding_model?: string;
  wechat_configured?: boolean;
  wechat_default_cover?: boolean;
}

export interface LibraryStats {
  documents: number;
  knowledge: number;
  vectors: number;
  /** Materials embedded over their summary, counted separately from `vectors`. */
  document_vectors: number;
  /** Materials that have a summary and can be embedded at all. */
  document_vectors_total: number;
  /** Materials archived but not extracted yet; a resting state, not a job. */
  pending: number;
  /** Materials whose extraction is running right now. */
  processing: number;
}

/**
 * DocumentStatus tracks the background extraction queue. Import returns
 * "ready": the material is stored and waiting for a worker.
 */
export type DocumentStatus = "ready" | "processing" | "published" | "processed_empty" | "analysis_failed" | string;

export interface DocumentItem {
  id: number;
  filename: string;
  source_key: string;
  version: number;
  sha256: string;
  media_type: string;
  /** Display metadata only: extraction always writes Chinese. */
  language: string;
  /** The shelf this material is filed on (日语, 英语, PHP...). Free text. */
  topic: string;
  status: DocumentStatus;
  error_message?: string;
  /** How many extraction attempts this material has had. */
  attempts?: number;
  /** The model's summary of the whole material, written when it is imported. */
  summary?: string;
  useful_count: number;
  /** How many drafts were written from this material's original text. */
  use_count?: number;
  processing_run_id?: string;
  created_at: string;
  processed_at?: string;
  /** Registered folder this material was scanned from (0 = uploaded). */
  root_id: number;
  /** Path under the folder, "/"-separated; only set for scanned materials. */
  rel_path?: string;
  /** The file was gone from its folder at the last scan. */
  missing: boolean;
}

/** A folder the owner registered for scanning. */
export interface LibraryRoot {
  id: number;
  path: string;
  name: string;
  created_at: string;
  document_count: number;
}

export interface ScanEntry {
  rel_path: string;
  size: number;
  /** Why a file was skipped: too_large / too_long / empty / invalid_utf8 / invalid_docx / unreadable. */
  reason?: string;
  document_id?: number;
}

export interface ScanReport {
  root: LibraryRoot;
  new: ScanEntry[];
  changed: ScanEntry[];
  skipped: ScanEntry[];
  missing: ScanEntry[];
  unchanged: number;
  known: number;
  other: number;
  truncated?: boolean;
  error_dirs?: string[];
}

/** One material's stored text, re-read from disk when its full text is opened. */
export interface SourceDocument {
  document_id: number;
  document_name: string;
  topic: string;
  text: string;
}

export interface Evidence {
  document_id: number;
  document_name: string;
  /** The topic of the source material this quote came from. */
  topic?: string;
  quote: string;
  quote_start: number;
  quote_end: number;
  document_version?: number;
  superseded?: boolean;
}

export interface KnowledgeItem {
  id: number;
  kind: string;
  title: string;
  body: string;
  /** Free-form labels beyond the fixed kind. */
  tags: string[];
  metadata: Record<string, unknown> | null;
  created_at: string;
  updated_at: string;
  evidence: Evidence[];
  /** The shelf this item stands on, taken from its source material. */
  topics?: string[];
  /** How many drafts were written from this item. */
  use_count?: number;
  /** How many times this item has been opened. */
  read_count?: number;
  /** Present on search results: which retrieval path found the item. */
  match?: "keyword" | "semantic" | "both" | string;
  /** Semantic cosine score (0–1); 0 when only keyword matched. */
  score?: number;
}

/** One shelf and how much knowledge stands on it. */
export interface TopicSummary {
  topic: string;
  knowledge_count: number;
}

/** What an import returns: the material is stored and queued, nothing more. */
export interface MaterialImportResult {
  document: DocumentItem;
  duplicate?: boolean;
}

export interface Citation {
  knowledge_id: number;
  title: string;
  evidence: Evidence[];
}

export type PlatformID = "gongzhonghao" | string;
export type DraftFormat = "article" | "gallery" | string;

/** One read-only line of the article's format rules, rendered by the server. */
export interface FormatRule {
  label: string;
  value: string;
}

/**
 * A platform. The only thing an owner can change is the style layer of the
 * draft prompt: every number is a format constant on the server, and `rules`
 * is the server's own readable description of them.
 */
export interface Platform {
  id: PlatformID;
  name: string;
  format: DraftFormat;
  formats?: DraftFormat[];
  description: string;
  style_rules?: string;
  revision?: number;
  is_default?: boolean;
  defaults?: { style_rules: string };
  rules?: FormatRule[];
  gallery_rules?: FormatRule[];
  style_rules_max?: number;
  gallery_image_max?: number;
  gallery_image_default?: number;
}

/** One chapter: a heading and the prose under it. */
export interface ArticleSection {
  order: number;
  heading: string;
  body: string;
  /**
   * Both optional and used as a pair: an article uses images sparingly, and
   * when one appears it carries a caption for under the image.
   */
  image_prompt?: string;
  image_caption?: string;
}

export interface ArticleContent {
  title: string;
  summary: string;
  /** 引言：开篇，把读者带进这篇文章要讲的事。 */
  lead: string;
  cover_image_prompt: string;
  sections: ArticleSection[];
  /** 结尾：收束全文。 */
  conclusion: string;
  tags: string[];
}

/** One picture of a picture post: a prompt, rendered locally at push time. */
export interface GalleryImage {
  order: number;
  prompt: string;
}

export interface GalleryContent {
  title: string;
  /** The short plain-text body shown beside the pictures. */
  caption: string;
  images: GalleryImage[];
}

export interface MediaDraft {
  id: number;
  run_id: string;
  artifact_id: number;
  platform: PlatformID;
  format: DraftFormat;
  topic: string;
  angle: string;
  audience: string;
  article?: ArticleContent | null;
  gallery?: GalleryContent | null;
  model: string;
  prompt_version: string;
  /** running while the background worker writes, generated when done. */
  status: "running" | "generated" | "failed";
  /** Why a failed draft has no article. */
  error_message: string;
  created_at: string;
  wechat_media_id?: string;
  wechat_pushed_at?: string | null;
  wechat_publish_id?: string;
  wechat_published_at?: string | null;
}

/** The Official Account configuration, edited on the settings screen. */
export interface WechatSettings {
  app_id: string;
  app_secret: string;
  author: string;
  thumb_media_id: string;
  /** Optional socks5:// URL; empty means a direct connection. */
  proxy_url: string;
  /** The server holds a saved AppSecret (it is never sent back). */
  app_secret_set?: boolean;
  updated_at?: string;
}

export interface AgentMessage {
  role: "user" | "assistant";
  content: string;
}

export interface AgentStep {
  tool: string;
  arguments: unknown;
  summary: string;
  error?: string;
}

export interface AgentReply {
  run_id: string;
  reply: string;
  citations: Citation[];
  steps: AgentStep[];
}
