import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  BookOpen,
  Bot,
  Boxes,
  CircleCheck,
  CircleX,
  Clover,
  FileUp,
  Files,
  Info,
  KeyRound,
  LockKeyhole,
  Palette,
  PenLine,
  ScrollText,
  TriangleAlert,
  X,
  type LucideIcon,
} from "lucide-react";
import { LOCKED_EVENT, api, errorText, setToken } from "./lib/api";
import { AppContext, type AppContextValue, type ToastInput, type ViewName } from "./lib/context";
import { LANGS, t, useLang, type MessageKey } from "./lib/i18n";
import { PLATFORM_NAV, platformOf } from "./lib/nav";
import type { AuthStatus, Health, KindInfo, LibraryStats, Platform } from "./lib/types";
import { Button, cx, ErrorNotice, Segmented, Skeleton } from "./components/ui";
import LockScreen from "./components/LockScreen";
import SecurityView from "./views/Security";
import MaterialsUploadView from "./views/MaterialsUpload";
import MaterialRootsView from "./views/MaterialRoots";
import MaterialsListView from "./views/MaterialsList";
import DocumentDetailView from "./views/DocumentDetail";
import KnowledgeView from "./views/Knowledge";
import DraftComposeView from "./views/DraftCompose";
import DraftsView from "./views/Drafts";
import DraftDetailView from "./views/DraftDetail";
import AgentView from "./views/Agent";
import StylesView from "./views/Styles";
import VectorsView from "./views/Vectors";
import PromptsView from "./views/Prompts";

interface NavItem {
  view: ViewName;
  label: MessageKey;
  icon: LucideIcon;
  hint: MessageKey;
}

// Labels are i18n keys, translated at render time so a language switch
// re-labels the menu immediately.
const NAV_GROUPS: Array<{ label: MessageKey; items: NavItem[] }> = [
  {
    label: "nav.group.knowledge",
    items: [
      { view: "upload", label: "nav.upload", icon: FileUp, hint: "nav.upload.hint" },
      { view: "documents", label: "nav.documents", icon: Files, hint: "nav.documents.hint" },
      { view: "knowledge", label: "nav.knowledge", icon: BookOpen, hint: "nav.knowledge.hint" },
      { view: "agent", label: "nav.agent", icon: Bot, hint: "nav.agent.hint" },
    ],
  },
  // One group per platform, built from the same table the routes use so a
  // platform can never appear in the menu without its screens.
  ...PLATFORM_NAV.map((entry) => ({
    label: entry.labelKey,
    items: [
      { view: entry.config, label: "nav.config", icon: Palette, hint: "nav.config.hint" },
      { view: entry.compose, label: "nav.compose", icon: PenLine, hint: "nav.compose.hint" },
      { view: entry.list, label: "nav.list", icon: BookOpen, hint: "nav.list.hint" },
    ] satisfies NavItem[],
  })),
  {
    label: "nav.group.settings",
    items: [
      { view: "security", label: "nav.security", icon: KeyRound, hint: "nav.security.hint" },
      { view: "vectors", label: "nav.vectors", icon: Boxes, hint: "nav.vectors.hint" },
      { view: "prompts", label: "nav.prompts", icon: ScrollText, hint: "nav.prompts.hint" },
    ],
  },
];

const NAV: NavItem[] = NAV_GROUPS.flatMap((group) => group.items);

interface ToastItem extends ToastInput {
  id: number;
}

// Detail screens are reached from a list row, not from the menu, but the hash
// router still has to recognise them so a deep link opens the right view.
const HASH_VIEWS: ViewName[] = [...NAV.map((item) => item.view), "documentDetail", ...PLATFORM_NAV.map((entry) => entry.detail)];

function readRouteFromHash(): { view: ViewName; target: string } {
  const raw = window.location.hash.replace(/^#\/?/, "");
  const [path, query = ""] = raw.split("?");
  const view = (HASH_VIEWS.find((item) => item === path) ?? "upload") as ViewName;
  const params = new URLSearchParams(query);
  const draft = params.get("draft") ?? "";
  const document = params.get("document") ?? "";
  if (view === "documentDetail") return { view, target: /^\d+$/.test(document) ? `document:${document}` : "" };
  return { view, target: /^\d+$/.test(draft) ? `draft:${draft}` : "" };
}

/** The draft and material detail screens are the views whose identity lives in
 *  the URL, so reloading or going back returns to the same record. */
function hashFor(view: ViewName, target: string): string {
  const draft = /^draft:(\d+)$/.exec(target);
  if (draft) return `#/${view}?draft=${draft[1]}`;
  const doc = /^document:(\d+)$/.exec(target);
  if (doc && view === "documentDetail") return `#/${view}?document=${doc[1]}`;
  return `#/${view}`;
}

type Gate = "checking" | "error" | "locked" | "open";

/** Asks the server whether a password is set before any data is loaded. */
export default function App() {
  const [gate, setGate] = useState<Gate>("checking");
  const [authEnabled, setAuthEnabled] = useState(false);
  const [session, setSession] = useState(0);
  useLang(); // re-render the gate screens when the language changes

  const check = useCallback(() => {
    setGate("checking");
    api<AuthStatus>("/api/v1/auth/status")
      .then((status) => {
        setAuthEnabled(status.enabled);
        setGate(status.enabled && !status.unlocked ? "locked" : "open");
      })
      .catch(() => setGate("error"));
  }, []);

  useEffect(() => {
    check();
  }, [check]);

  // Any request the server refuses for a missing or stale token drops back here.
  useEffect(() => {
    const onLocked = () => setGate("locked");
    window.addEventListener(LOCKED_EVENT, onLocked);
    return () => window.removeEventListener(LOCKED_EVENT, onLocked);
  }, []);

  if (gate === "locked") {
    return (
      <LockScreen
        onUnlocked={() => {
          setSession((n) => n + 1);
          setGate("open");
        }}
      />
    );
  }
  if (gate === "open") {
    return (
      <Workspace
        key={session}
        authEnabled={authEnabled}
        setAuthEnabled={setAuthEnabled}
        onLock={() => {
          setToken("");
          setGate("locked");
        }}
      />
    );
  }
  return (
    <div className="grid min-h-screen place-items-center px-4 text-center text-sm text-zinc-500 dark:text-zinc-400">
      {gate === "error" ? (
        <div className="space-y-3">
          <p>{t("lock.statusFailed")}</p>
          <Button variant="secondary" onClick={check}>
            {t("lock.retry")}
          </Button>
        </div>
      ) : (
        <p>{t("lock.checking")}</p>
      )}
    </div>
  );
}

function Workspace({
  authEnabled,
  setAuthEnabled,
  onLock,
}: {
  authEnabled: boolean;
  setAuthEnabled: (enabled: boolean) => void;
  onLock: () => void;
}) {
  const [lang, setLang] = useLang();
  const [platforms, setPlatforms] = useState<Platform[]>([]);
  const [kinds, setKinds] = useState<KindInfo[]>([]);
  const [stats, setStats] = useState<LibraryStats | null>(null);
  const [initialRoute] = useState(readRouteFromHash);
  const [focusTarget, setFocusTarget] = useState(initialRoute.target);
  const [bootError, setBootError] = useState<unknown>(null);
  const [health, setHealth] = useState<Health | null>(null);
  const [healthError, setHealthError] = useState<unknown>(null);
  const [view, setView] = useState<ViewName>(initialRoute.view);
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const [knowledgeVersion, setKnowledgeVersion] = useState(0);
  const toastSeq = useRef(0);

  const notify = useCallback((toast: ToastInput) => {
    const id = ++toastSeq.current;
    setToasts((items) => [...items.slice(-3), { ...toast, id }]);
    window.setTimeout(() => setToasts((items) => items.filter((item) => item.id !== id)), toast.kind === "error" ? 8000 : 4500);
  }, []);

  const loadHealth = useCallback(() => {
    api<Health>("/health/ready")
      .then((data) => {
        setHealth(data);
        setHealthError(null);
      })
      .catch((error) => setHealthError(error));
  }, []);

  useEffect(() => {
    loadHealth();
    const timer = window.setInterval(loadHealth, 30000);
    return () => window.clearInterval(timer);
  }, [loadHealth]);

  const reloadStats = useCallback(async () => {
    const data = await api<LibraryStats>("/api/v1/stats");
    setStats(data);
  }, []);

  const reloadPlatforms = useCallback(async () => {
    const platformList = await api<Platform[]>("/api/v1/platforms");
    const known = (Array.isArray(platformList) ? platformList : []).filter((item) => item && typeof item.id === "string");
    const rank = (id: string) => {
      const index = PLATFORM_NAV.findIndex((entry) => entry.id === id);
      return index < 0 ? 99 : index;
    };
    setPlatforms([...known].sort((a, b) => rank(a.id) - rank(b.id)));
  }, []);

  const reloadKinds = useCallback(async () => {
    const list = await api<KindInfo[]>("/api/v1/kinds");
    setKinds((Array.isArray(list) ? list : []).filter((item) => item && typeof item.id === "string"));
  }, []);

  useEffect(() => {
    Promise.all([reloadKinds(), reloadPlatforms(), reloadStats()]).catch((error) => setBootError(error));
  }, [reloadKinds, reloadPlatforms, reloadStats]);

  // While extraction runs in the background, the header counts keep moving so
  // importing feels finished before the knowledge has landed. `pending` is the
  // resting "archived, not extracted" count and must not drive polling.
  const processing = stats?.processing ?? 0;
  useEffect(() => {
    const interval = processing > 0 ? 4000 : 30000;
    const timer = window.setInterval(() => {
      reloadStats().catch(() => undefined);
    }, interval);
    return () => window.clearInterval(timer);
  }, [processing, reloadStats]);

  useEffect(() => {
    if (!window.location.hash) {
      window.history.replaceState(null, "", hashFor(initialRoute.view, initialRoute.target));
    }
    // Both events are needed: the browser back button fires popstate, while a
    // hand-edited hash only fires hashchange. Neither may clobber focusTarget,
    // so both re-read the whole route from the URL.
    const sync = () => {
      const route = readRouteFromHash();
      setView(route.view);
      setFocusTarget(route.target);
    };
    window.addEventListener("popstate", sync);
    window.addEventListener("hashchange", sync);
    return () => {
      window.removeEventListener("popstate", sync);
      window.removeEventListener("hashchange", sync);
    };
  }, [initialRoute]);

  // Every navigation is a real history entry, so the browser back button walks
  // back through the screens the owner actually visited instead of leaving the
  // app. A plain menu click clears the highlight; jumpTo passes a target.
  const navigate = useCallback((next: ViewName, target = "") => {
    setFocusTarget(target);
    setView(next);
    const hash = hashFor(next, target);
    if (window.location.hash !== hash) window.history.pushState(null, "", hash);
    window.scrollTo({ top: 0 });
  }, []);

  const jumpTo = useCallback(
    (next: ViewName, target = "") => {
      navigate(next, target);
    },
    [navigate],
  );

  const value = useMemo<AppContextValue>(
    () => ({
      platforms,
      kinds,
      reloadKinds,
      reloadPlatforms,
      stats,
      reloadStats,
      focusTarget,
      jumpTo,
      health,
      navigate,
      notify,
      knowledgeVersion,
      bumpKnowledge: () => {
        setKnowledgeVersion((current) => current + 1);
        reloadStats().catch(() => undefined);
      },
      lang,
      setLang,
      authEnabled,
      setAuthEnabled,
      lockApp: onLock,
    }),
    [platforms, kinds, reloadKinds, reloadPlatforms, stats, reloadStats, focusTarget, jumpTo, health, navigate, notify, knowledgeVersion, lang, setLang, authEnabled, setAuthEnabled, onLock],
  );

  // Set only on the platform-scoped screens (配置 / 创作 / 列表 / 详情), which are
  // the same components every time, parameterised by platform.
  const scope = platformOf(view);

  return (
    <div className="min-h-screen lg:flex">
      <Sidebar view={view === "documentDetail" ? "documents" : view} navigate={navigate} />
      <div className="min-w-0 flex-1">
        <TopBar health={health} healthError={healthError} stats={stats} onRefreshHealth={loadHealth} locked={authEnabled} onLock={onLock} />
        <MobileNav view={view === "documentDetail" ? "documents" : view} navigate={navigate} />
        <main className="mx-auto w-full max-w-7xl px-4 py-4 sm:px-5 lg:px-8 lg:py-5">
          {health && !health.ark_configured ? (
            <div className="mb-6 flex flex-wrap items-center gap-x-3 gap-y-1 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
              <TriangleAlert className="size-4 shrink-0" />
              <span className="font-medium">{t("app.arkMissingTitle")}</span>
              <span className="text-amber-800/80 dark:text-amber-200/80">{t("app.arkMissingBody")}</span>
            </div>
          ) : null}
          {bootError ? (
            <ErrorNotice error={bootError} />
          ) : (
            <AppContext.Provider value={value}>
              {view === "upload" && <ImportView />}
              {view === "documents" && <MaterialsListView />}
              {view === "documentDetail" && <DocumentDetailView />}
              {view === "knowledge" && <KnowledgeView />}
              {view === "agent" && <AgentView />}
              {scope?.config === view && <StylesView platform={scope.id} />}
              {scope?.compose === view && <DraftComposeView platform={scope.id} />}
              {scope?.list === view && <DraftsView platform={scope.id} />}
              {scope?.detail === view && <DraftDetailView platform={scope.id} />}
              {view === "vectors" && <VectorsView />}
              {view === "prompts" && <PromptsView />}
              {view === "security" && <SecurityView />}
            </AppContext.Provider>
          )}
        </main>
      </div>
      <Toasts items={toasts} onDismiss={(id) => setToasts((items) => items.filter((item) => item.id !== id))} />
    </div>
  );
}

/** Import: upload files, or pick a folder and scan it. */
function ImportView() {
  const [tab, setTab] = useState<"upload" | "folders">("upload");
  return (
    <>
      <div className="mb-4">
        <Segmented
          value={tab}
          onChange={setTab}
          options={[
            { value: "upload", label: t("import.tabUpload") },
            { value: "folders", label: t("import.tabFolders") },
          ]}
        />
      </div>
      {tab === "upload" ? <MaterialsUploadView /> : <MaterialRootsView />}
    </>
  );
}

function Brand() {
  return (
    <div className="flex items-center gap-2.5">
      <span className="grid size-9 place-items-center rounded-xl bg-gradient-to-br from-emerald-500 to-teal-600 text-white shadow-sm shadow-emerald-900/20">
        <Clover className="size-5" />
      </span>
      <div className="leading-tight">
        <p className="font-semibold tracking-tight">CloverGo</p>
        <p className="text-[11px] text-zinc-500 dark:text-zinc-400">{t("app.tagline")}</p>
      </div>
    </div>
  );
}

function NavButton({ item, active, onClick }: { item: NavItem; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-current={active ? "page" : undefined}
      title={t(item.hint)}
      className={cx(
        "group flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-[13px] transition",
        active
          ? "bg-emerald-50 text-emerald-900 dark:bg-emerald-500/10 dark:text-emerald-100"
          : "text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900 dark:text-zinc-400 dark:hover:bg-zinc-800/70 dark:hover:text-zinc-100",
      )}
    >
      <item.icon className={cx("size-4 shrink-0", active ? "text-emerald-600 dark:text-emerald-400" : "")} />
      <span className="min-w-0 flex-1 truncate font-medium">{t(item.label)}</span>
    </button>
  );
}

function Sidebar({ view, navigate }: { view: ViewName; navigate: (view: ViewName) => void }) {
  return (
    <aside className="sticky top-0 hidden h-screen w-56 shrink-0 flex-col border-r border-zinc-200 bg-white/70 px-3 py-4 backdrop-blur lg:flex dark:border-zinc-800 dark:bg-zinc-900/40">
      <Brand />
      <nav className="scrollbar-thin mt-4 mb-3 min-h-0 space-y-3 overflow-y-auto" aria-label={t("app.navLabel")}>
        {NAV_GROUPS.map((group, index) => (
          <div key={group.label}>
            <p
              className={cx(
                "mb-1 px-3 text-[11px] font-semibold tracking-wider",
                index === 0 ? "text-emerald-700 dark:text-emerald-400" : "text-zinc-400 dark:text-zinc-500",
              )}
            >
              {t(group.label)}
            </p>
            <div className="space-y-0.5">
              {group.items.map((item) => (
                <NavButton
                  key={item.view}
                  item={item}
                  active={item.view === view}
                  onClick={() => navigate(item.view)}
                />
              ))}
            </div>
          </div>
        ))}
      </nav>
    </aside>
  );
}

function MobileNav({ view, navigate }: { view: ViewName; navigate: (view: ViewName) => void }) {
  return (
    <nav
      className="scrollbar-thin flex gap-1 overflow-x-auto border-b border-zinc-200 bg-white/80 px-3 py-2 backdrop-blur lg:hidden dark:border-zinc-800 dark:bg-zinc-900/60"
      aria-label={t("app.navLabel")}
    >
      {NAV.map((item) => (
        <button
          key={item.view}
          type="button"
          onClick={() => navigate(item.view)}
          className={cx(
            "inline-flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm",
            item.view === view
              ? "bg-emerald-50 font-medium text-emerald-900 dark:bg-emerald-500/10 dark:text-emerald-100"
              : "text-zinc-600 dark:text-zinc-400",
          )}
        >
          <item.icon className="size-4" />
          {t(item.label)}
        </button>
      ))}
    </nav>
  );
}

function TopBar({
  health,
  healthError,
  stats,
  onRefreshHealth,
  locked,
  onLock,
}: {
  health: Health | null;
  healthError: unknown;
  stats: LibraryStats | null;
  onRefreshHealth: () => void;
  locked: boolean;
  onLock: () => void;
}) {
  return (
    <header className="sticky top-0 z-20 border-b border-zinc-200 bg-white/80 backdrop-blur-md dark:border-zinc-800 dark:bg-zinc-950/70">
      <div className="mx-auto flex w-full max-w-7xl flex-wrap items-center gap-3 px-4 py-2 sm:px-5 lg:px-8">
        <div className="lg:hidden">
          <Brand />
        </div>
        <div className="flex min-w-0 flex-1 items-center gap-2.5 overflow-hidden">
          {stats ? (
            <>
              <span className="hidden shrink-0 items-center gap-2 rounded-lg bg-emerald-50 px-2.5 py-1 text-xs font-semibold text-emerald-800 ring-1 ring-emerald-200 ring-inset lg:inline-flex dark:bg-emerald-500/10 dark:text-emerald-200 dark:ring-emerald-500/25">
                <span>
                  {t("app.statDocuments")} <span className="tabular-nums">{stats.documents}</span>
                </span>
                <span className="opacity-60">·</span>
                <span>
                  {t("app.statKnowledge")} <span className="tabular-nums">{stats.knowledge}</span>
                </span>
              </span>
              {stats.processing > 0 ? (
                <span className="hidden min-w-0 shrink items-center gap-1.5 rounded-lg bg-sky-50 px-2.5 py-1 text-xs font-medium text-sky-800 lg:inline-flex dark:bg-sky-500/10 dark:text-sky-200" title={t("app.extractingTitle")}>
                  <span className="size-1.5 animate-pulse rounded-full bg-sky-500" />
                  {t("app.extracting")} <span className="tabular-nums">{stats.processing}</span>
                </span>
              ) : null}
            </>
          ) : (
            <Skeleton className="h-7 w-36" />
          )}
        </div>
        {locked ? (
          <Button variant="ghost" size="sm" icon={LockKeyhole} onClick={onLock} title={t("lock.lock")}>
            {t("lock.lock")}
          </Button>
        ) : null}
        <LangToggle />
        <HealthPill health={health} error={healthError} onRefresh={onRefreshHealth} />
      </div>
    </header>
  );
}

function LangToggle() {
  const [lang, setLang] = useLang();
  return (
    <div
      role="group"
      aria-label={t("app.langLabel")}
      title={t("app.langLabel")}
      className="inline-flex shrink-0 rounded-full border border-zinc-200 bg-white p-0.5 text-xs dark:border-zinc-800 dark:bg-zinc-900"
    >
      {LANGS.map((option) => (
        <button
          key={option.id}
          type="button"
          lang={option.id === "zh" ? "zh-CN" : "en"}
          aria-pressed={lang === option.id}
          onClick={() => setLang(option.id)}
          className={cx(
            "rounded-full px-2.5 py-1 font-medium transition",
            lang === option.id
              ? "bg-emerald-50 text-emerald-800 dark:bg-emerald-500/15 dark:text-emerald-200"
              : "text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-200",
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

function HealthPill({ health, error, onRefresh }: { health: Health | null; error: unknown; onRefresh: () => void }) {
  const state = error ? "bad" : !health ? "loading" : health.ark_configured ? "ok" : "warn";
  const dot = {
    ok: "bg-emerald-500 shadow-[0_0_0_3px] shadow-emerald-500/20",
    warn: "bg-amber-500 shadow-[0_0_0_3px] shadow-amber-500/20",
    bad: "bg-rose-500 shadow-[0_0_0_3px] shadow-rose-500/20",
    loading: "bg-zinc-400 animate-pulse",
  }[state];
  const title = error
    ? errorText(error)
    : health
      ? t("health.tooltip", {
          status: health.status,
          fts: health.fts_mode,
          ark: health.ark_configured ? t("health.configured") : t("health.notConfigured"),
          model: health.ark_model || t("health.unset"),
          embedding: health.embedding_configured ? t("health.embeddingOn", { model: health.embedding_model ?? "" }) : t("health.embeddingOff"),
        })
      : t("health.connecting");
  return (
    <button
      type="button"
      onClick={onRefresh}
      title={t("health.clickRefresh", { title })}
      className="inline-flex max-w-full items-center gap-2 rounded-full border border-zinc-200 bg-white px-3 py-1.5 text-xs text-zinc-600 transition hover:border-zinc-300 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-300"
    >
      <span className={cx("size-2 shrink-0 rounded-full", dot)} />
      {error ? (
        <span>{t("health.unavailable")}</span>
      ) : health ? (
        <span className="font-medium">{health.ark_configured ? t("health.arkReady") : t("health.arkMissing")}</span>
      ) : (
        <span>{t("health.connectingShort")}</span>
      )}
    </button>
  );
}

function Toasts({ items, onDismiss }: { items: ToastItem[]; onDismiss: (id: number) => void }) {
  return (
    <div className="pointer-events-none fixed right-4 bottom-4 z-50 flex w-[min(24rem,calc(100vw-2rem))] flex-col gap-2" aria-live="polite">
      {items.map((item) => {
        const Icon = item.kind === "error" ? CircleX : item.kind === "success" ? CircleCheck : item.kind === "info" ? Info : TriangleAlert;
        return (
          <div
            key={item.id}
            role="status"
            className={cx(
              "animate-toast-in pointer-events-auto flex gap-3 rounded-xl border bg-white px-4 py-3 text-sm shadow-lg shadow-zinc-900/10 dark:bg-zinc-900",
              item.kind === "error"
                ? "border-rose-200 dark:border-rose-500/40"
                : item.kind === "success"
                  ? "border-emerald-200 dark:border-emerald-500/40"
                  : "border-zinc-200 dark:border-zinc-700",
            )}
          >
            <Icon
              className={cx(
                "mt-0.5 size-4 shrink-0",
                item.kind === "error" ? "text-rose-500" : item.kind === "success" ? "text-emerald-500" : "text-sky-500",
              )}
            />
            <div className="min-w-0 flex-1">
              <p className="font-medium break-words">{item.title}</p>
              {item.detail ? <p className="mt-0.5 text-xs break-words text-zinc-500 dark:text-zinc-400">{item.detail}</p> : null}
            </div>
            <Button variant="ghost" size="sm" className="h-6! px-1!" icon={X} aria-label={t("common.close")} onClick={() => onDismiss(item.id)} />
          </div>
        );
      })}
    </div>
  );
}
