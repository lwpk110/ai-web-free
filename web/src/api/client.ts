const ADMIN_TOKEN_STORAGE_KEY = "aurora_admin_token";

export class AdminUnauthorizedError extends Error {
  constructor(message = "管理端已启用 ADMIN_TOKEN 保护，请先解锁") {
    super(message);
    this.name = "AdminUnauthorizedError";
  }
}

export function getStoredAdminToken(): string {
  try {
    return window.sessionStorage.getItem(ADMIN_TOKEN_STORAGE_KEY) || "";
  } catch {
    return "";
  }
}

export function setStoredAdminToken(token: string): void {
  try {
    const trimmed = token.trim();
    if (trimmed) {
      window.sessionStorage.setItem(ADMIN_TOKEN_STORAGE_KEY, trimmed);
    } else {
      window.sessionStorage.removeItem(ADMIN_TOKEN_STORAGE_KEY);
    }
  } catch {
    // ignore storage errors
  }
}

function buildAdminHeaders(extra?: HeadersInit): Headers {
  const headers = new Headers(extra);
  const token = getStoredAdminToken();
  if (token) {
    headers.set("X-Admin-Token", token);
  }
  return headers;
}

async function adminFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const res = await fetch(input, {
    ...init,
    headers: buildAdminHeaders(init?.headers),
  });
  if (res.status === 401) {
    window.dispatchEvent(new Event("admin-unauthorized"));
    throw new AdminUnauthorizedError();
  }
  return res;
}

export interface ProviderDescriptor {
  id: string;
  name: string;
  vendor: string;
  status: "enabled" | "planned" | string;
  version: string;
  description: string;
  supported_account_types: string[];
  credential_formats: string[];
  capabilities: string[];
  featured_models?: string[];
}

export interface PoolStats {
  total: number;
  active: number;
  pending: number;
  expired: number;
  rate_limited: number;
  image_limited: number;
  disabled: number;
  banned: number;
  free_count: number;
  noauth_count: number;
  puid_count: number;
  temp_count: number;
  total_calls: number;
  failed_calls: number;
}

export interface ProviderSummary {
  descriptor: ProviderDescriptor;
  stats: PoolStats;
}

export interface ProxySummary {
  total: number;
  active: number;
  cooling: number;
}

export interface ProxyNodeStatus {
  url: string;
  status: "active" | "cooling" | "unknown" | string;
  latency_ms: number;
  consecutive_errors: number;
  total_failures: number;
  last_checked?: string;
  last_error?: string;
  cooldown_until?: string;
  bound_accounts: number;
}

export interface KamiWorkerStats {
  running: boolean;
  running_email?: string;
  queue_length: number;
  total_succeeded: number;
  total_failed: number;
  last_run_at?: string;
  last_error?: string;
}

export interface AuditSummary {
  capacity: number;
  buffered_count: number;
  total_requests: number;
  failed_requests: number;
  failover_recovered: number;
  total_events: number;
  file_logging: boolean;
  log_file?: string;
}

export interface AuditEntry {
  id: string;
  timestamp: string;
  kind: "request" | "event" | string;
  method?: string;
  path?: string;
  model?: string;
  account_id?: string;
  account_email?: string;
  proxy?: string;
  status?: number;
  duration_ms?: number;
  failover_trace?: string;
  attempt_count?: number;
  error?: string;
  note?: string;
}

export interface OverviewResponse {
  service: string;
  version: string;
  uptime_seconds: number;
  started_at: string;
  auth_enabled: boolean;
  admin_auth_required?: boolean;
  external_token_enabled: boolean;
  stream_mode: boolean;
  proxies_count: number;
  proxies: string[];
  proxies_summary?: ProxySummary;
  kami_worker?: KamiWorkerStats;
  audit_summary?: AuditSummary;
  providers: ProviderSummary[];
}

export interface AccountView {
  id: string;
  email?: string;
  chatgpt_account_id?: string;
  type: string;
  status: string;
  plan_type?: string;
  token_preview?: string;
  has_refresh_token?: boolean;
  has_session_token?: boolean;
  has_kami_credentials?: boolean;
  is_temporary?: boolean;
  image_limited?: boolean;
  cooldown_until?: string;
  image_cooldown_until?: string;
  renew_state?: string;
  renew_cooldown_until?: string;
  last_renew_error?: string;
  proxy?: string;
  tls_profile?: string;
  total_calls?: number;
  failed_calls?: number;
  expires_at?: string;
  remaining_days?: number;
  last_used?: string;
  last_checked?: string;
  created_at?: string;
  last_error?: string;
  capabilities?: string[];
}

export async function fetchAuthStatus(
  overrideToken?: string
): Promise<{ auth_required: boolean; unlocked: boolean }> {
  const headers: Record<string, string> = {};
  const token = overrideToken ?? getStoredAdminToken();
  if (token) {
    headers["X-Admin-Token"] = token;
  }
  const res = await fetch("/admin/api/v1/auth/status", { headers });
  if (!res.ok) throw new Error(`Auth status failed: HTTP ${res.status}`);
  return res.json();
}

export async function fetchOverview(): Promise<OverviewResponse> {
  const res = await adminFetch("/admin/api/v1/overview");
  if (!res.ok) throw new Error(`Overview failed: HTTP ${res.status}`);
  return res.json();
}

export async function fetchAccounts(
  provider = "chatgpt",
  status = "",
  type = "",
  search = ""
): Promise<{ provider: string; accounts: AccountView[]; stats: PoolStats }> {
  const params = new URLSearchParams();
  if (status) params.set("status", status);
  if (type) params.set("type", type);
  if (search) params.set("q", search);
  const qs = params.toString();
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(provider)}/accounts${qs ? `?${qs}` : ""}`
  );
  if (!res.ok) throw new Error(`Fetch accounts failed: HTTP ${res.status}`);
  return res.json();
}

export type AccountActionType =
  | "probe"
  | "refresh"
  | "enable"
  | "disable"
  | "delete"
  | "totp"
  | "login";

export async function executeAccountAction(
  provider: string,
  accountId: string,
  action: AccountActionType
): Promise<{
  provider: string;
  stats: PoolStats;
  result: {
    id?: string;
    ok?: boolean;
    status?: string;
    latency_ms?: number;
    models_count?: number;
    error?: string;
    message?: string;
    email?: string;
    totp_code?: string;
    remaining_seconds?: number;
    expires_at?: string;
    deleted?: boolean;
  };
}> {
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(provider)}/accounts/${encodeURIComponent(accountId)}/action`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action }),
    }
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Action failed: HTTP ${res.status}`);
  }
  return data;
}

export async function probeAllAccounts(
  provider = "chatgpt"
): Promise<{
  provider: string;
  probed: number;
  passed: number;
  failed: number;
  stats: PoolStats;
}> {
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(provider)}/pool/probe-all`,
    { method: "POST" }
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Probe failed: HTTP ${res.status}`);
  }
  return data;
}

export async function loginKamiAccounts(
  provider = "chatgpt",
  headless = true,
  force = false
): Promise<{
  provider: string;
  stats: PoolStats;
  result?: {
    total: number;
    success: number;
    failed: number;
    message?: string;
  };
}> {
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(provider)}/accounts/kami-login`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ headless, force }),
    }
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Kami login failed: HTTP ${res.status}`);
  }
  return data;
}

export async function importAccounts(params: {
  provider: string;
  credentialType: string;
  rawText: string;
  persist?: boolean;
  autoLogin?: boolean;
  headless?: boolean;
  generateCount?: number;
}): Promise<{
  provider: string;
  stats: PoolStats;
  result: {
    added: number;
    skipped: number;
    failed: number;
    total_pool: number;
    persisted: boolean;
    messages?: string[];
  };
}> {
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(params.provider)}/accounts/import`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        credential_type: params.credentialType,
        raw_text: params.rawText,
        persist: params.persist ?? true,
        auto_login: params.autoLogin ?? false,
        headless: params.headless ?? true,
        generate_count: params.generateCount ?? 0,
      }),
    }
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Import failed: HTTP ${res.status}`);
  }
  return data;
}

export async function exportAccounts(params: {
  provider?: string;
  format: "sub2api_json" | "access_tokens" | "kami";
  includeSecrets?: boolean;
  download?: boolean;
}): Promise<{
  filename: string;
  content: string;
  contentType: string;
  redacted: boolean;
}> {
  const provider = params.provider || "chatgpt";
  const qs = new URLSearchParams({
    format: params.format,
    include_secrets: params.includeSecrets ? "true" : "false",
    download: params.download ? "true" : "false",
  });
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(provider)}/export?${qs.toString()}`
  );
  if (!res.ok) {
    const errText = await res.text();
    throw new Error(errText || `Export failed: HTTP ${res.status}`);
  }
  const contentType = res.headers.get("content-type") || "text/plain";
  const redacted = res.headers.get("X-Secrets-Redacted") === "true";
  const raw = await res.text();
  const content = contentType.includes("application/json")
    ? JSON.stringify(JSON.parse(raw), null, 2)
    : raw;
  const filename =
    params.format === "sub2api_json"
      ? "aurora_sub2api.json"
      : params.format === "access_tokens"
        ? "access_tokens.txt"
        : "kami_accounts.txt";
  return { filename, content, contentType, redacted };
}

export async function fetchProxies(): Promise<{
  summary: ProxySummary;
  nodes: ProxyNodeStatus[];
}> {
  const res = await adminFetch("/admin/api/v1/proxies");
  if (!res.ok) throw new Error(`Fetch proxies failed: HTTP ${res.status}`);
  return res.json();
}

export async function probeProxies(): Promise<{
  summary: ProxySummary;
  nodes: ProxyNodeStatus[];
}> {
  const res = await adminFetch("/admin/api/v1/proxies/probe", {
    method: "POST",
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Probe proxies failed: HTTP ${res.status}`);
  }
  return data;
}

export async function updateProxies(
  proxies: string[],
  persist = true
): Promise<{
  proxies_count: number;
  proxies: string[];
  migrated: number;
}> {
  const res = await adminFetch("/admin/api/v1/proxies", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ proxies, persist }),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Update proxies failed: HTTP ${res.status}`);
  }
  return data;
}

export async function fetchLogs(params?: {
  filter?: string;
  path?: string;
  limit?: number;
}): Promise<{
  summary: AuditSummary;
  entries: AuditEntry[];
}> {
  const qs = new URLSearchParams();
  if (params?.filter) qs.set("filter", params.filter);
  if (params?.path) qs.set("path", params.path);
  if (params?.limit) qs.set("limit", String(params.limit));
  const query = qs.toString();
  const res = await adminFetch(`/admin/api/v1/logs${query ? `?${query}` : ""}`);
  if (!res.ok) throw new Error(`Fetch logs failed: HTTP ${res.status}`);
  return res.json();
}

export async function fetchModelsCatalog(
  apiKey?: string
): Promise<Array<{ id: string; owned_by?: string }>> {
  const headers: Record<string, string> = {};
  if (apiKey?.trim()) {
    headers["Authorization"] = `Bearer ${apiKey.trim()}`;
  }
  const res = await fetch("/v1/models?include_builtins=true", { headers });
  if (!res.ok) {
    let errMsg = `HTTP ${res.status}`;
    try {
      const errBody = await res.json();
      errMsg =
        errBody?.error?.message ||
        (typeof errBody?.error === "string" ? errBody.error : errMsg);
    } catch {
      // ignore non-json error body
    }
    throw new Error(errMsg);
  }
  const data = await res.json();
  return Array.isArray(data?.data) ? data.data : [];
}

export async function generateImage(params: {
  prompt: string;
  model: string;
  size: string;
  quality: string;
  n?: number;
  apiKey?: string;
}): Promise<{ data: Array<{ url?: string; b64_json?: string; revised_prompt?: string }> }> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (params.apiKey) {
    headers["Authorization"] = `Bearer ${params.apiKey}`;
  }
  const res = await fetch("/v1/images/generations", {
    method: "POST",
    headers,
    body: JSON.stringify({
      prompt: params.prompt,
      model: params.model,
      size: params.size,
      quality: params.quality,
      n: params.n || 1,
    }),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || data?.error || `HTTP ${res.status}`);
  }
  return data;
}

export async function editImageWithReferences(params: {
  prompt: string;
  model: string;
  size: string;
  files: File[];
  apiKey?: string;
}): Promise<{ data: Array<{ url?: string; b64_json?: string }> }> {
  const form = new FormData();
  form.append("prompt", params.prompt);
  form.append("model", params.model);
  form.append("size", params.size);
  params.files.forEach((f) => form.append("image", f));

  const headers: Record<string, string> = {};
  if (params.apiKey) {
    headers["Authorization"] = `Bearer ${params.apiKey}`;
  }
  const res = await fetch("/v1/images/edits", {
    method: "POST",
    headers,
    body: form,
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || data?.error || `HTTP ${res.status}`);
  }
  return data;
}

export async function streamChatCompletion(params: {
  messages: Array<{ role: string; content: string }>;
  model: string;
  openuiMode: boolean;
  apiKey?: string;
  onDelta: (deltaText: string, fullText: string) => void;
}): Promise<string> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (params.apiKey) {
    headers["Authorization"] = `Bearer ${params.apiKey}`;
  }
  if (params.openuiMode) {
    headers["X-OpenUI-Lang"] = "true";
  }

  const res = await fetch("/v1/chat/completions", {
    method: "POST",
    headers,
    body: JSON.stringify({
      model: params.model,
      stream: true,
      messages: params.messages,
    }),
  });

  if (!res.ok) {
    const errText = await res.text();
    throw new Error(`HTTP ${res.status}: ${errText}`);
  }

  if (!res.body) {
    throw new Error("Empty response body");
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder("utf-8");
  let buffer = "";
  let accumulated = "";

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split("\n");
    buffer = lines.pop() || "";

    for (const rawLine of lines) {
      const line = rawLine.trim();
      if (!line.startsWith("data:")) continue;
      const payload = line.slice(5).trim();
      if (payload === "[DONE]") continue;
      try {
        const parsed = JSON.parse(payload);
        const delta = parsed?.choices?.[0]?.delta?.content;
        if (typeof delta === "string" && delta.length > 0) {
          accumulated += delta;
          params.onDelta(delta, accumulated);
        }
      } catch {
        // ignore partial JSON chunks
      }
    }
  }

  return accumulated;
}
