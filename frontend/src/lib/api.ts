import { isMessageKey, t } from "./i18n";

export class ApiError extends Error {
  code: string;
  status: number;
  runID: string;
  requestID: string;

  constructor(message: string, code: string, status: number, runID = "", requestID = "") {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
    this.runID = runID;
    this.requestID = requestID;
  }
}

type Query = Record<string, string | number | undefined | null>;

export function withQuery(path: string, params: Query = {}): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== null && value !== "") query.set(key, String(value));
  }
  const suffix = query.toString();
  return suffix ? `${path}?${suffix}` : path;
}

const TOKEN_KEY = "clovergo.token";
const TOKEN_HEADER = "X-CloverGo-Token";
/** Fired when the server says the session is locked (token missing, stale or revoked). */
export const LOCKED_EVENT = "clovergo:locked";

export function getToken(): string {
  try {
    return window.localStorage.getItem(TOKEN_KEY) ?? "";
  } catch {
    return "";
  }
}

export function setToken(token: string): void {
  try {
    if (token) window.localStorage.setItem(TOKEN_KEY, token);
    else window.localStorage.removeItem(TOKEN_KEY);
  } catch {
    /* storage unavailable: the lock simply will not survive a reload */
  }
}

function withToken(init: RequestInit): RequestInit {
  const token = getToken();
  if (!token) return init;
  const headers = new Headers(init.headers);
  headers.set(TOKEN_HEADER, token);
  return { ...init, headers };
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  let response: Response;
  try {
    response = await fetch(path, { ...withToken(init), credentials: "same-origin" });
  } catch (cause) {
    throw new ApiError(cause instanceof Error ? cause.message : "network error", "NETWORK_ERROR", 0);
  }
  const payload = (await response.json().catch(() => ({}))) as {
    data?: T;
    error?: { code?: string; message?: string };
    request_id?: string;
    run_id?: string;
  };
  if (!response.ok) {
    if (response.status === 401 && payload?.error?.code === "AUTH_REQUIRED") {
      setToken("");
      window.dispatchEvent(new Event(LOCKED_EVENT));
    }
    throw new ApiError(
      payload?.error?.message || t("error.httpFailed", { status: response.status }),
      payload?.error?.code || "HTTP_ERROR",
      response.status,
      payload?.run_id || "",
      payload?.request_id || "",
    );
  }
  return payload.data as T;
}

export function postJSON<T>(path: string, body?: unknown): Promise<T> {
  return api<T>(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body ?? {}),
  });
}

export function putJSON<T>(path: string, body?: unknown): Promise<T> {
  return api<T>(path, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body ?? {}),
  });
}

export function deleteJSON<T>(path: string): Promise<T> {
  return api<T>(path, { method: "DELETE" });
}

/** Downloads a GET endpoint as a file. A plain link cannot carry the token header. */
export async function downloadFile(path: string, fallbackName: string): Promise<void> {
  let response: Response;
  try {
    response = await fetch(path, { ...withToken({}), credentials: "same-origin" });
  } catch (cause) {
    throw new ApiError(cause instanceof Error ? cause.message : "network error", "NETWORK_ERROR", 0);
  }
  if (!response.ok) {
    const payload = (await response.json().catch(() => ({}))) as { error?: { code?: string; message?: string }; request_id?: string };
    if (response.status === 401 && payload?.error?.code === "AUTH_REQUIRED") {
      setToken("");
      window.dispatchEvent(new Event(LOCKED_EVENT));
    }
    throw new ApiError(
      payload?.error?.message || t("error.httpFailed", { status: response.status }),
      payload?.error?.code || "HTTP_ERROR",
      response.status,
      "",
      payload?.request_id || "",
    );
  }
  const disposition = response.headers.get("Content-Disposition") ?? "";
  const match = /filename\*=UTF-8''([^;]+)|filename="?([^";]+)"?/i.exec(disposition);
  let name = fallbackName;
  if (match) {
    try {
      name = decodeURIComponent(match[1] ?? match[2]);
    } catch {
      name = match[2] ?? fallbackName;
    }
  }
  const url = URL.createObjectURL(await response.blob());
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = name;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

/** Friendly text for a known error code (i18n key `error.<CODE>`), or "". */
function friendly(code: string): string {
  const key = `error.${code}`;
  return isMessageKey(key) ? t(key) : "";
}

export interface DescribedError {
  code: string;
  title: string;
  detail: string;
  runID: string;
  requestID: string;
}

export function describeError(error: unknown): DescribedError {
  if (error instanceof ApiError) {
    const known = friendly(error.code);
    const title = known || error.message || t("common.unknownError");
    return {
      code: error.code,
      title,
      detail: known && error.message !== title ? error.message : "",
      runID: error.runID,
      requestID: error.requestID,
    };
  }
  const message = error instanceof Error ? error.message : String(error ?? t("common.unknownError"));
  return { code: "CLIENT_ERROR", title: message, detail: "", runID: "", requestID: "" };
}

export function errorText(error: unknown): string {
  const d = describeError(error);
  const meta = d.requestID ? t("common.requestMeta", { id: d.requestID }) : "";
  return meta ? t("common.withMeta", { title: d.title, meta }) : d.title;
}
