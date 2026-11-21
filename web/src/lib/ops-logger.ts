export interface FrontendOpsLogEntry {
  id: string;
  timestamp: string;
  source: "frontend";
  level: "error" | "warn" | "info" | "debug";
  module: string;
  message: string;
  detail?: string;
  page?: string;
}

const STORAGE_KEY = "ai_web_free_frontend_ops_logs";
const ADMIN_TOKEN_KEY = "ai_web_free_admin_token";
const MAX_LOCAL_LOGS = 300;

let initialized = false;
let seq = 0;
let localLogs: FrontendOpsLogEntry[] = loadStoredLogs();
const listeners = new Set<() => void>();

let rawFetch: typeof window.fetch | null = null;
let pendingShip: FrontendOpsLogEntry[] = [];
let shipTimer: number | null = null;

function loadStoredLogs(): FrontendOpsLogEntry[] {
  try {
    const raw = window.sessionStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed.slice(0, MAX_LOCAL_LOGS) : [];
  } catch {
    return [];
  }
}

function saveStoredLogs(list: FrontendOpsLogEntry[]) {
  try {
    window.sessionStorage.setItem(
      STORAGE_KEY,
      JSON.stringify(list.slice(0, MAX_LOCAL_LOGS))
    );
  } catch {
    // ignore quota / storage errors
  }
}

function notifyListeners() {
  listeners.forEach((fn) => {
    try {
      fn();
    } catch {
      // ignore subscriber errors
    }
  });
}

function formatArg(arg: unknown): string {
  if (arg instanceof Error) {
    return arg.stack || `${arg.name}: ${arg.message}`;
  }
  if (typeof arg === "string") return arg;
  try {
    return JSON.stringify(arg);
  } catch {
    return String(arg);
  }
}

function flushShipQueue() {
  shipTimer = null;
  if (!rawFetch || pendingShip.length === 0) return;
  const batch = pendingShip.splice(0, 20);
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  try {
    const token = window.sessionStorage.getItem(ADMIN_TOKEN_KEY) || "";
    if (token) headers["X-Admin-Token"] = token;
  } catch {
    // ignore
  }
  rawFetch("/admin/api/v1/ops-logs/frontend", {
    method: "POST",
    headers,
    body: JSON.stringify({ entries: batch }),
    keepalive: true,
  }).catch(() => {
    // ignore transport failure when shipping logs
  });
}

function scheduleShip(entry: FrontendOpsLogEntry) {
  if (entry.level !== "error" && entry.level !== "warn") return;
  pendingShip.push(entry);
  if (pendingShip.length > 50) {
    pendingShip = pendingShip.slice(-50);
  }
  if (shipTimer === null) {
    shipTimer = window.setTimeout(flushShipQueue, 400);
  }
}

export function recordFrontendOpsLog(input: {
  level?: "error" | "warn" | "info" | "debug";
  module: string;
  message: string;
  detail?: string;
  page?: string;
}): FrontendOpsLogEntry {
  seq += 1;
  const entry: FrontendOpsLogEntry = {
    id: `fe-${Date.now()}-${seq}`,
    timestamp: new Date().toISOString(),
    source: "frontend",
    level: input.level || "info",
    module: input.module || "app",
    message: (input.message || "").slice(0, 2000),
    detail: input.detail ? input.detail.slice(0, 8000) : undefined,
    page: input.page || window.location.hash || "#/",
  };
  localLogs = [entry, ...localLogs].slice(0, MAX_LOCAL_LOGS);
  saveStoredLogs(localLogs);
  notifyListeners();
  scheduleShip(entry);
  return entry;
}

export function getFrontendOpsLogs(): FrontendOpsLogEntry[] {
  return localLogs;
}

export function clearFrontendOpsLogs(): void {
  localLogs = [];
  saveStoredLogs([]);
  notifyListeners();
}

export function subscribeFrontendOpsLogs(cb: () => void): () => void {
  listeners.add(cb);
  return () => {
    listeners.delete(cb);
  };
}

export function initFrontendOpsLogger(): void {
  if (initialized || typeof window === "undefined") return;
  initialized = true;
  rawFetch = typeof window.fetch === "function" ? window.fetch.bind(window) : null;

  try {
    const origError = console.error.bind(console);
    console.error = (...args: unknown[]) => {
      origError(...args);
      try {
        const parts = args.map(formatArg);
        const msg = parts[0] || "console.error";
        const detail = parts.length > 1 ? parts.slice(1).join("\n") : undefined;
        recordFrontendOpsLog({
          level: "error",
          module: "console.error",
          message: msg,
          detail,
        });
      } catch {
        // ignore
      }
    };
  } catch {
    // ignore read-only console.error
  }

  try {
    const origWarn = console.warn.bind(console);
    console.warn = (...args: unknown[]) => {
      origWarn(...args);
      try {
        const parts = args.map(formatArg);
        const msg = parts[0] || "console.warn";
        const detail = parts.length > 1 ? parts.slice(1).join("\n") : undefined;
        recordFrontendOpsLog({
          level: "warn",
          module: "console.warn",
          message: msg,
          detail,
        });
      } catch {
        // ignore
      }
    };
  } catch {
    // ignore read-only console.warn
  }

  window.addEventListener("error", (event) => {
    try {
      const errObj = event.error;
      const detail =
        errObj instanceof Error
          ? errObj.stack
          : `${event.filename || ""}:${event.lineno || 0}:${event.colno || 0}`;
      recordFrontendOpsLog({
        level: "error",
        module: "window.onerror",
        message: event.message || "Uncaught runtime error",
        detail,
      });
    } catch {
      // ignore
    }
  });

  window.addEventListener("unhandledrejection", (event) => {
    try {
      const reason = event.reason;
      const message =
        reason instanceof Error
          ? reason.message
          : typeof reason === "string"
            ? reason
            : "Unhandled Promise rejection";
      const detail = reason instanceof Error ? reason.stack : formatArg(reason);
      recordFrontendOpsLog({
        level: "error",
        module: "unhandledrejection",
        message,
        detail,
      });
    } catch {
      // ignore
    }
  });

  const baseFetch = rawFetch;
  if (!baseFetch) return;

  const wrappedFetch: typeof window.fetch = async (
    input: RequestInfo | URL,
    init?: RequestInit
  ) => {
    const url =
      typeof input === "string"
        ? input
        : input instanceof URL
          ? input.toString()
          : input.url;
    const method = (
      init?.method ||
      (input instanceof Request ? input.method : "GET")
    ).toUpperCase();

    // 不拦截日志上报接口自身，避免死循环
    if (url.includes("/admin/api/v1/ops-logs/frontend")) {
      return baseFetch(input, init);
    }

    try {
      const res = await baseFetch(input, init);
      if (!res.ok) {
        let snippet = "";
        try {
          const text = await res.clone().text();
          snippet = text.slice(0, 1200);
        } catch {
          // ignore body read error
        }
        recordFrontendOpsLog({
          level: res.status >= 500 ? "error" : "warn",
          module: "fetch",
          message: `${method} ${url} -> HTTP ${res.status} ${res.statusText || ""}`.trim(),
          detail: snippet || undefined,
        });
      }
      return res;
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      const stack = err instanceof Error ? err.stack : undefined;
      recordFrontendOpsLog({
        level: "error",
        module: "fetch.network",
        message: `${method} ${url} -> ${msg}`,
        detail: stack,
      });
      throw err;
    }
  };

  try {
    const desc = Object.getOwnPropertyDescriptor(window, "fetch");
    if (!desc || desc.writable) {
      window.fetch = wrappedFetch;
    } else if (desc.configurable) {
      Object.defineProperty(window, "fetch", {
        configurable: true,
        enumerable: desc.enumerable ?? true,
        writable: true,
        value: wrappedFetch,
      });
    }
  } catch {
    // Sandboxed iframe environments may expose a getter-only window.fetch; skip fetch wrapping safely.
  }
}
