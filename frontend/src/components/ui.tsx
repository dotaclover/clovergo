import { useState, type ButtonHTMLAttributes, type ReactNode } from "react";
import { Check, CircleCheck, Copy, Info, LoaderCircle, TriangleAlert, type LucideIcon } from "lucide-react";
import { describeError } from "../lib/api";
import { formatElapsed, useElapsed } from "../lib/hooks";
import { t } from "../lib/i18n";

export function cx(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(" ");
}

type ButtonVariant = "primary" | "secondary" | "ghost" | "subtle";

const buttonVariants: Record<ButtonVariant, string> = {
  primary:
    "bg-emerald-600 text-white shadow-sm shadow-emerald-900/10 hover:bg-emerald-700 active:bg-emerald-800 dark:bg-emerald-500 dark:text-emerald-950 dark:hover:bg-emerald-400",
  secondary:
    "border border-zinc-300 bg-white text-zinc-800 shadow-xs hover:bg-zinc-50 hover:border-zinc-400 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:bg-zinc-800",
  ghost: "text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900 dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-100",
  subtle:
    "bg-emerald-50 text-emerald-800 hover:bg-emerald-100 dark:bg-emerald-500/10 dark:text-emerald-300 dark:hover:bg-emerald-500/20",
};

export function Button({
  variant = "primary",
  size = "md",
  loading,
  icon: Icon,
  children,
  className,
  disabled,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant;
  size?: "sm" | "md";
  loading?: boolean;
  icon?: LucideIcon;
}) {
  return (
    <button
      type="button"
      {...rest}
      disabled={disabled || loading}
      className={cx(
        "inline-flex shrink-0 items-center justify-center gap-1.5 rounded-lg font-medium whitespace-nowrap transition disabled:cursor-not-allowed disabled:opacity-55",
        size === "sm" ? "h-7.5 px-2.5 text-xs" : "h-9 px-3.5 text-[13px]",
        buttonVariants[variant],
        className,
      )}
    >
      {loading ? <LoaderCircle className="size-4 animate-spin" /> : Icon ? <Icon className="size-4" /> : null}
      {children}
    </button>
  );
}

export function Card({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <section
      className={cx(
        "rounded-xl border border-zinc-200/80 bg-white shadow-xs dark:border-zinc-800 dark:bg-zinc-900/60",
        className,
      )}
    >
      {children}
    </section>
  );
}

export function CardHeader({
  title,
  description,
  icon: Icon,
  actions,
}: {
  title: ReactNode;
  description?: ReactNode;
  icon?: LucideIcon;
  actions?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3 border-b border-zinc-100 px-4 py-3 dark:border-zinc-800">
      <div className="flex min-w-0 items-start gap-2.5">
        {Icon ? (
          <span className="mt-0.5 grid size-7 shrink-0 place-items-center rounded-md bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300">
            <Icon className="size-3.5" />
          </span>
        ) : null}
        <div className="min-w-0">
          <h2 className="text-sm font-semibold tracking-tight">{title}</h2>
          {description ? <p className="mt-0.5 text-xs text-zinc-500 dark:text-zinc-400">{description}</p> : null}
        </div>
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  );
}

export function PageHeader({ title, description, actions }: { title: string; description?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
      <div className="min-w-0">
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
        {description ? <p className="mt-1 max-w-3xl text-[13px] text-zinc-500 dark:text-zinc-400">{description}</p> : null}
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  );
}

type Tone = "neutral" | "green" | "amber" | "red" | "blue" | "violet";

const toneClasses: Record<Tone, string> = {
  neutral: "bg-zinc-100 text-zinc-700 ring-zinc-200 dark:bg-zinc-800 dark:text-zinc-300 dark:ring-zinc-700",
  green: "bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-500/10 dark:text-emerald-300 dark:ring-emerald-500/25",
  amber: "bg-amber-50 text-amber-800 ring-amber-200 dark:bg-amber-500/10 dark:text-amber-300 dark:ring-amber-500/25",
  red: "bg-rose-50 text-rose-700 ring-rose-200 dark:bg-rose-500/10 dark:text-rose-300 dark:ring-rose-500/25",
  blue: "bg-sky-50 text-sky-700 ring-sky-200 dark:bg-sky-500/10 dark:text-sky-300 dark:ring-sky-500/25",
  violet: "bg-violet-50 text-violet-700 ring-violet-200 dark:bg-violet-500/10 dark:text-violet-300 dark:ring-violet-500/25",
};

export function Badge({ tone = "neutral", children, className }: { tone?: Tone; children: ReactNode; className?: string }) {
  return (
    <span
      className={cx(
        "inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-xs font-medium whitespace-nowrap ring-1 ring-inset",
        toneClasses[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}

export function statusTone(status?: string): Tone {
  switch (status) {
    case "published":
    case "succeeded":
    case "generated":
      return "green";
    case "processing":
    case "running":
      return "blue";
    case "processed_empty":
    case "ready":
      return "amber";
    case "analysis_failed":
    case "failed":
      return "red";
    default:
      return "neutral";
  }
}

export function Empty({
  icon: Icon,
  title,
  description,
  action,
  frame = true,
}: {
  icon: LucideIcon;
  title: string;
  description?: ReactNode;
  action?: ReactNode;
  /** Turn the dashed frame off when the empty state already sits in a Card. */
  frame?: boolean;
}) {
  return (
    <div
      className={cx(
        "flex flex-col items-center justify-center rounded-2xl px-6 py-12 text-center",
        frame && "border border-dashed border-zinc-300 dark:border-zinc-700",
      )}
    >
      <span className="grid size-11 place-items-center rounded-full bg-zinc-100 text-zinc-500 dark:bg-zinc-800 dark:text-zinc-400">
        <Icon className="size-5" />
      </span>
      <p className="mt-3 text-sm font-medium">{title}</p>
      {description ? <p className="mt-1 max-w-sm text-sm text-zinc-500 dark:text-zinc-400">{description}</p> : null}
      {action ? <div className="mt-4">{action}</div> : null}
    </div>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={cx("animate-pulse rounded-md bg-zinc-200/70 dark:bg-zinc-800", className)} />;
}

export function ListSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div className="space-y-3">
      {Array.from({ length: rows }, (_, index) => (
        <div key={index} className="rounded-xl border border-zinc-200 p-4 dark:border-zinc-800">
          <Skeleton className="h-3 w-24" />
          <Skeleton className="mt-3 h-4 w-2/3" />
          <Skeleton className="mt-2 h-3 w-full" />
          <Skeleton className="mt-1.5 h-3 w-5/6" />
        </div>
      ))}
    </div>
  );
}

export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-sm font-medium text-zinc-700 dark:text-zinc-300">{label}</span>
      {children}
      {hint ? <span className="mt-1 block text-xs text-zinc-500 dark:text-zinc-400">{hint}</span> : null}
    </label>
  );
}

export function ErrorNotice({ error, className }: { error: unknown; className?: string }) {
  if (!error) return null;
  const described = describeError(error);
  return (
    <div
      role="alert"
      className={cx(
        "flex gap-3 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-900 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-200",
        className,
      )}
    >
      <TriangleAlert className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0 space-y-1">
        <p className="font-medium">{described.title}</p>
        {described.detail ? <p className="break-words text-rose-800/80 dark:text-rose-200/80">{described.detail}</p> : null}
        <p className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-rose-800/70 dark:text-rose-200/70">
          <span className="font-mono">{described.code}</span>
          {described.requestID ? <span className="font-mono">{t("common.requestMeta", { id: described.requestID })}</span> : null}
        </p>
      </div>
    </div>
  );
}

export function Notice({ tone = "info", children }: { tone?: "info" | "success" | "warning"; children: ReactNode }) {
  const styles = {
    info: "border-sky-200 bg-sky-50 text-sky-900 dark:border-sky-500/30 dark:bg-sky-500/10 dark:text-sky-200",
    success:
      "border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-200",
    warning: "border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200",
  }[tone];
  const Icon = tone === "success" ? CircleCheck : tone === "warning" ? TriangleAlert : Info;
  return (
    <div className={cx("flex gap-3 rounded-xl border px-4 py-3 text-sm", styles)}>
      <Icon className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0">{children}</div>
    </div>
  );
}

export function LongTask({ active, title, hint }: { active: boolean; title: string; hint?: string }) {
  const elapsed = useElapsed(active);
  if (!active) return null;
  return (
    <div
      role="status"
      className="relative overflow-hidden rounded-xl border border-emerald-200 bg-emerald-50/70 px-4 py-3 text-sm text-emerald-900 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-100"
    >
      <div className="flex items-center gap-3">
        <LoaderCircle className="size-4 shrink-0 animate-spin" />
        <div className="min-w-0 flex-1">
          <p className="font-medium">{title}</p>
          {hint ? <p className="text-xs text-emerald-800/80 dark:text-emerald-200/70">{hint}</p> : null}
        </div>
        <span className="font-mono text-xs tabular-nums">{formatElapsed(elapsed)}</span>
      </div>
      <div className="absolute inset-x-0 bottom-0 h-0.5 overflow-hidden bg-emerald-100 dark:bg-emerald-900/50">
        <div className="h-full w-1/3 animate-progress bg-emerald-500" />
      </div>
    </div>
  );
}

export function Segmented<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T;
  options: Array<{ value: T; label: ReactNode }>;
  onChange: (value: T) => void;
}) {
  return (
    <div className="inline-flex rounded-lg bg-zinc-100 p-0.5 dark:bg-zinc-800/80" role="tablist">
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          role="tab"
          aria-selected={option.value === value}
          onClick={() => onChange(option.value)}
          className={cx(
            "inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium transition",
            option.value === value
              ? "bg-white text-zinc-900 shadow-xs dark:bg-zinc-700 dark:text-white"
              : "text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-200",
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

export function CopyButton({ text, label = t("common.copy") }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      variant="ghost"
      size="sm"
      icon={copied ? Check : Copy}
      onClick={() => {
        void navigator.clipboard
          ?.writeText(text)
          .then(() => {
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1500);
          })
          .catch(() => undefined);
      }}
    >
      {copied ? t("common.copied") : label}
    </Button>
  );
}

/**
 * Prev/next pager for a server-paged list. It renders nothing when everything
 * fits on one page.
 */
export function Pager({
  page,
  pageSize,
  total,
  onPage,
}: {
  page: number;
  pageSize: number;
  total: number;
  onPage: (page: number) => void;
}) {
  const pages = Math.max(1, Math.ceil(total / pageSize));
  if (pages <= 1) return null;
  const current = Math.min(Math.max(page, 1), pages);
  return (
    <div className="mt-3 flex items-center justify-end gap-2 text-xs text-zinc-500 dark:text-zinc-400">
      <Button variant="secondary" size="sm" disabled={current <= 1} onClick={() => onPage(current - 1)}>
        {t("common.prevPage")}
      </Button>
      <span className="tabular-nums">{t("common.pageOf", { page: current, pages })}</span>
      <Button variant="secondary" size="sm" disabled={current >= pages} onClick={() => onPage(current + 1)}>
        {t("common.nextPage")}
      </Button>
    </div>
  );
}

/** Character counter that warns once the value approaches its limit. */
export function Counter({ text, max }: { text: string; max: number }) {
  const length = [...(text ?? "")].length;
  return <span className={cx("shrink-0 tabular-nums", length > max * 0.9 && "text-amber-600")}>{length}/{max}</span>;
}
