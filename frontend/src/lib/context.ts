import { createContext, useContext } from "react";
import type { Health, KindInfo, LibraryStats, Platform } from "./types";
import { describeError } from "./api";
import { t, type Lang } from "./i18n";

/**
 * Every screen in the workbench. Platform-scoped screens are spelled out
 * rather than derived so a typo is a compile error, and lib/nav.ts is the one
 * place that maps a view back to its platform.
 */
export type ViewName =
  | "upload"
  | "documents"
  | "documentDetail"
  | "knowledge"
  | "agent"
  | "gzh-config"
  | "gzh-compose"
  | "gzh-list"
  | "gzh-draft"
  | "vectors"
  | "prompts"
  | "security";

export interface ToastInput {
  kind?: "info" | "success" | "error";
  title: string;
  detail?: string;
}

export interface AppContextValue {
  platforms: Platform[];
  /** The knowledge taxonomy the server uses for classification: the built-in
   *  categories plus any custom kind an operator has typed onto an item. */
  kinds: KindInfo[];
  /** Refreshes the taxonomy after an item is saved under a new kind. */
  reloadKinds: () => Promise<void>;
  reloadPlatforms: () => Promise<void>;
  /** Whole-library counts, refreshed after imports and knowledge changes. */
  stats: LibraryStats | null;
  reloadStats: () => Promise<void>;
  /** Target of the last jump from another screen, e.g. "platform:gongzhonghao",
   *  "knowledge:12" or "draft:3". It is how the knowledge list hands one item
   *  to the draft screen, and how the draft screen hands a finished draft to
   *  the list. */
  focusTarget: string;
  jumpTo: (view: ViewName, target?: string) => void;
  health: Health | null;
  navigate: (view: ViewName) => void;
  notify: (toast: ToastInput) => void;
  knowledgeVersion: number;
  bumpKnowledge: () => void;
  /** UI language; switching re-renders the whole app. */
  lang: Lang;
  setLang: (lang: Lang) => void;
  /** Whether a global password is set on the server. */
  authEnabled: boolean;
  /** Tells the shell the password was set or removed so the lock button follows. */
  setAuthEnabled: (enabled: boolean) => void;
  /** Forgets the unlock token and returns to the lock screen. */
  lockApp: () => void;
}

export const AppContext = createContext<AppContextValue | null>(null);

export function useApp(): AppContextValue {
  const value = useContext(AppContext);
  if (!value) throw new Error("AppContext is missing");
  return value;
}

export function toastError(notify: (toast: ToastInput) => void, error: unknown, fallback = t("common.opFailed")): void {
  const described = describeError(error);
  // The server's validation message says what is actually wrong ("images must
  // contain 1-9 items"); without it the toast only shows a bare error code.
  const meta = [described.detail, described.code, described.requestID && `Request ${described.requestID}`]
    .filter(Boolean)
    .join(" · ");
  notify({ kind: "error", title: described.title || fallback, detail: meta });
}
