const ADMIN_TOKEN_STORAGE_KEY = "ai_web_free_admin_token";

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

export interface ChannelTag {
  key: string;
  label: string;
  category: "cost" | "auth" | "feature" | "reliability";
  variant: "default" | "success" | "warning" | "purple" | "cyan";
  icon?: string;
  tooltip?: string;
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
  tags?: ChannelTag[];
  cost_type?: string;
  auth_type?: string;
}

export interface VoiceDescriptor {
  id: string;
  name: string;
  provider: string;
  category: string;
  gender: "female" | "male";
  sample_text: string;
  tags: string[];
  preview_url?: string;
}

export interface VideoTask {
  task_id: string;
  provider: string;
  model: string;
  prompt: string;
  image_url?: string;
  audio_url?: string;
  duration: number;
  status: "PENDING" | "RUNNING" | "SUCCEEDED" | "FAILED";
  progress: number;
  video_url?: string;
  error_msg?: string;
  created_at: string;
  completed_at?: string;
}

export interface VideoModelInfo {
  id: string;
  name: string;
  provider: string;
  description: string;
  max_duration: number;
  resolutions: string[];
  supports_i2v: boolean;
  supports_a2v: boolean;
  is_free_tier: boolean;
  tag: string;
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
  running_account_id?: string;
  running_email?: string;
  running_phase?: string;
  running_phase_label?: string;
  running_started_at?: string;
  queue_length: number;
  queued_account_ids?: string[];
  total_succeeded: number;
  total_failed: number;
  last_run_at?: string;
  last_error?: string;
}

export interface KamiLoginTaskResult {
  account_id: string;
  email: string;
  ok: boolean;
  cancelled?: boolean;
  error?: string;
}

export interface KamiLoginTask {
  id: string;
  status: "queued" | "running" | "completed" | "failed" | string;
  total: number;
  completed: number;
  success: number;
  failed: number;
  cancelled?: number;
  current_account_id?: string;
  current_email?: string;
  current_phase?: string;
  current_phase_label?: string;
  current_started_at?: string;
  queued_account_ids?: string[];
  dismissed?: boolean;
  results: KamiLoginTaskResult[];
  created_at: string;
  updated_at: string;
}

export interface ProbeTask {
  id: string;
  status: "running" | "completed" | string;
  total: number;
  probed: number;
  passed: number;
  failed: number;
  dismissed?: boolean;
  results?: Array<{
    id?: string;
    email?: string;
    ok?: boolean;
    latency_ms?: number;
    error?: string;
  }>;
  created_at: string;
  updated_at: string;
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
  login_task?: KamiLoginTask | null;
  probe_task?: ProbeTask | null;
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
  active_action?: string;
  action_phase?: string;
  action_phase_label?: string;
  action_queue_pos?: number;
  action_task_id?: string;
  action_started_at?: string;
  last_action?: string;
  last_action_ok?: boolean;
  last_action_summary?: string;
  last_action_at?: string;
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
  models?: string[];
  models_count?: number;
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
): Promise<{
  provider: string;
  accounts: AccountView[];
  stats: PoolStats;
  login_task?: KamiLoginTask | null;
  probe_task?: ProbeTask | null;
  kami_worker?: KamiWorkerStats;
}> {
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

export async function bulkUpdateAccountProxy(
  provider: string,
  accountIds: string[],
  proxyUrl: string
): Promise<{
  provider: string;
  result: {
    total: number;
    updated: number;
    failed: number;
    results: { id: string; email: string; ok: boolean; error?: string }[];
  };
}> {
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(provider)}/accounts/bulk-proxy`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ account_ids: accountIds, proxy_url: proxyUrl }),
    }
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Bulk proxy update failed: HTTP ${res.status}`);
  }
  return data;
}

export type AccountActionType =
  | "probe"
  | "refresh"
  | "enable"
  | "disable"
  | "delete"
  | "totp"
  | "sync_chrome"
  | "extract_chrome"
  | "login";

export async function executeAccountAction(
  provider: string,
  accountId: string,
  action: AccountActionType,
  options?: { headless?: boolean }
): Promise<{
  provider: string;
  stats: PoolStats;
  result: {
    id?: string;
    ok?: boolean;
    status?: string;
    latency_ms?: number;
    models_count?: number;
    models?: string[];
    error?: string;
    message?: string;
    email?: string;
    totp_code?: string;
    remaining_seconds?: number;
    expires_at?: string;
    deleted?: boolean;
    queued?: boolean;
    task_id?: string;
    task?: KamiLoginTask;
    cookie?: string;
    count?: number;
  };
}> {
  const payload: Record<string, unknown> = { action };
  if (typeof options?.headless === "boolean") {
    payload.headless = options.headless;
  }
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(provider)}/accounts/${encodeURIComponent(accountId)}/action`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    }
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Action failed: HTTP ${res.status}`);
  }
  return data;
}

export async function probeAllAccounts(
  provider = "chatgpt",
  asyncMode = true
): Promise<{
  provider: string;
  task?: ProbeTask;
  probed: number;
  passed: number;
  failed: number;
  stats: PoolStats;
}> {
  const qs = asyncMode ? "?async=true" : "";
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(provider)}/pool/probe-all${qs}`,
    { method: "POST" }
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Probe failed: HTTP ${res.status}`);
  }
  return data;
}

export async function cancelKamiLogin(
  provider = "chatgpt",
  payload: { task_id?: string; account_id?: string; cancel_all?: boolean }
): Promise<{
  provider: string;
  cancelled: number;
  task?: KamiLoginTask | null;
  stats: PoolStats;
}> {
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(provider)}/accounts/kami-login/cancel`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    }
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Cancel failed: HTTP ${res.status}`);
  }
  return data;
}

export async function dismissAdminTask(
  provider = "chatgpt",
  payload: { kind?: "login" | "probe"; task_id?: string }
): Promise<{ ok: boolean; dismissed: boolean }> {
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(provider)}/tasks/dismiss`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    }
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Dismiss failed: HTTP ${res.status}`);
  }
  return data;
}

export async function loginKamiAccounts(
  provider = "chatgpt",
  headless = true,
  force = false,
  accountIds: string[] = []
): Promise<{
  provider: string;
  task: KamiLoginTask;
}> {
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(provider)}/accounts/kami-login`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ headless, force, account_ids: accountIds }),
    }
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Kami login failed: HTTP ${res.status}`);
  }
  return data;
}

export async function fetchKamiLoginTask(
  provider: string,
  taskId: string
): Promise<{ provider: string; task: KamiLoginTask }> {
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(provider)}/accounts/kami-login/tasks/${encodeURIComponent(taskId)}`
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Fetch login task failed: HTTP ${res.status}`);
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
      ? "ai_web_free_sub2api.json"
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

export interface ModelsCatalogItem {
  id: string;
  owned_by?: string;
  source?: "upstream" | "builtin" | string;
}

export interface ModelsCatalogResult {
  data: ModelsCatalogItem[];
  source: "live" | "cache" | "fallback";
  cached_at?: string;
  upstream_count?: number;
  warning?: string;
}

async function parseCatalogError(res: Response): Promise<string> {
  let errMsg = `HTTP ${res.status}`;
  try {
    const errBody = await res.json();
    errMsg =
      errBody?.error?.message ||
      (typeof errBody?.error === "string" ? errBody.error : errMsg);
  } catch {
    // ignore non-json error body
  }
  return errMsg;
}

export async function fetchAdminModelsCatalog(options?: {
  refresh?: boolean;
  apiKey?: string;
}): Promise<ModelsCatalogResult> {
  const apiKey = options?.apiKey?.trim();
  const useCacheParam = options?.refresh ? "" : "&use_cache=true";
  if (apiKey) {
    const res = await fetch(`/v1/models?include_builtins=true${useCacheParam}`, {
      headers: { Authorization: `Bearer ${apiKey}` },
    });
    if (!res.ok) {
      throw new Error(await parseCatalogError(res));
    }
    const data = await res.json();
    return {
      data: Array.isArray(data?.data) ? data.data : [],
      source: "live",
      cached_at: new Date().toISOString(),
    };
  }

  const refreshParam = options?.refresh ? "&refresh=true" : "";
  try {
    const res = await adminFetch(
      `/admin/api/v1/models?include_builtins=true${refreshParam}`
    );
    if (res.ok) {
      const data = await res.json();
      return {
        data: Array.isArray(data?.data) ? data.data : [],
        source: data?.source || "live",
        cached_at: data?.cached_at,
        upstream_count: data?.upstream_count,
        warning: data?.warning,
      };
    }
  } catch (err) {
    if (err instanceof AdminUnauthorizedError) {
      throw err;
    }
  }

  const res = await fetch(`/v1/models?include_builtins=true${useCacheParam}`);
  if (!res.ok) {
    throw new Error(await parseCatalogError(res));
  }
  const data = await res.json();
  return {
    data: Array.isArray(data?.data) ? data.data : [],
    source: "live",
    cached_at: new Date().toISOString(),
  };
}

export async function fetchModelsCatalog(
  apiKey?: string
): Promise<ModelsCatalogItem[]> {
  const res = await fetchAdminModelsCatalog({ apiKey });
  return res.data;
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
  signal?: AbortSignal;
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
    signal: params.signal,
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
        if (parsed?.error?.message && accumulated.length === 0) {
          throw new Error(parsed.error.message);
        }
        const delta = parsed?.choices?.[0]?.delta?.content;
        if (typeof delta === "string" && delta.length > 0) {
          accumulated += delta;
          params.onDelta(delta, accumulated);
        }
      } catch (e) {
        if (e instanceof Error && e.message && accumulated.length === 0 && payload.includes('"error"')) {
          throw e;
        }
        // ignore partial JSON chunks
      }
    }
  }

  return accumulated;
}

export interface DiagnosticRootCauseItem {
  category: "banned" | "waf_proxy_block" | "token_expired" | "rate_limit" | "proxy_breaker" | "other" | string;
  severity: "critical" | "warning" | "info" | string;
  title: string;
  count: number;
  summary: string;
  affected_ids?: string[];
  account_emails?: string[];
  proxies?: string[];
  suggested_fixes?: string[];
}

export interface DiagnosticRepairOption {
  action: string;
  label: string;
  description: string;
  destructive: boolean;
  recommended: boolean;
  target_count: number;
}

export interface DiagnosticAccountBrief {
  id: string;
  email: string;
  type: string;
  status: string;
  reason: string;
  proxy: string;
  last_error?: string;
  renew_state?: string;
  has_refresh: boolean;
  has_session: boolean;
  has_kami: boolean;
}

export interface DiagnosticsReport {
  generated_at: string;
  health_score: number;
  overall_severity: "healthy" | "degraded" | "critical" | string;
  headline: string;
  pool_stats: PoolStats;
  proxy_summary: ProxySummary;
  kami_worker?: KamiWorkerStats;
  audit_summary: AuditSummary;
  root_causes: DiagnosticRootCauseItem[];
  flagged_accounts: DiagnosticAccountBrief[];
  tripped_proxies: ProxyNodeStatus[];
  repair_options: DiagnosticRepairOption[];
  markdown_report: string;
}

export interface DiagnosticRepairRequest {
  action:
    | "smart_heal"
    | "rebind_proxies"
    | "renew_expired"
    | "probe_all"
    | "topup_free_uuid"
    | "disable_banned"
    | "delete_banned"
    | string;
  provider?: string;
  confirm?: boolean;
  count?: number;
}

export interface DiagnosticRepairResponse {
  ok: boolean;
  action: string;
  summary: string;
  details: Record<string, number | string | boolean>;
  diagnostics: DiagnosticsReport;
}

export async function fetchDiagnostics(): Promise<DiagnosticsReport> {
  const res = await adminFetch("/admin/api/v1/diagnostics");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `HTTP ${res.status}`);
  }
  return data;
}

export async function executeDiagnosticRepair(
  req: DiagnosticRepairRequest
): Promise<DiagnosticRepairResponse> {
  const res = await adminFetch("/admin/api/v1/diagnostics/repair", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `HTTP ${res.status}`);
  }
  return data;
}

export interface PromptVariable {
  key: string;
  label: string;
  default_value: string;
  options?: string[];
}

export interface PromptStats {
  likes: number;
  hearts: number;
  score: number;
}

export interface PromptPlazaItem {
  id: string;
  title_zh: string;
  category: string;
  prompt_en: string;
  prompt_zh: string;
  preview_url?: string;
  recommended_mode: "generations" | "edits" | string;
  recommended_model: string;
  recommended_size: string;
  variables?: PromptVariable[];
  source: "seed" | "civitai" | "lexica" | "reddit" | "huggingface" | "ai_search" | string;
  stats: PromptStats;
  updated_at: string;
}

export interface PromptSyncStatus {
  state: "idle" | "running" | "last_error" | string;
  running: boolean;
  last_synced_at?: string;
  last_error?: string;
  active_sources?: string[];
  source_counts?: Record<string, number>;
  total_count: number;
}

export interface PromptListResponse {
  items: PromptPlazaItem[];
  categories: Record<string, number>;
  sources: Record<string, number>;
  last_synced_at?: string;
  sync_status: PromptSyncStatus;
}

export interface PromptExpandRequest {
  idea: string;
  mode?: string;
  size?: string;
}

export interface PromptExpandResponse {
  title_zh: string;
  category: string;
  prompt_en: string;
  prompt_zh: string;
  recommended_mode: "generations" | "edits" | string;
  recommended_model: string;
  recommended_size: string;
  variables?: PromptVariable[];
  source: string;
}

export async function fetchPromptPlaza(params?: {
  category?: string;
  mode?: string;
  source?: string;
  q?: string;
  sort?: string;
}): Promise<PromptListResponse> {
  const sp = new URLSearchParams();
  if (params?.category && params.category !== "all") sp.set("category", params.category);
  if (params?.mode && params.mode !== "all") sp.set("mode", params.mode);
  if (params?.source && params.source !== "all") sp.set("source", params.source);
  if (params?.q?.trim()) sp.set("q", params.q.trim());
  if (params?.sort) sp.set("sort", params.sort);
  const qs = sp.toString();
  const res = await adminFetch(`/admin/api/v1/prompts${qs ? `?${qs}` : ""}`);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Fetch prompts failed: HTTP ${res.status}`);
  }
  return data;
}

export async function triggerPromptPlazaSync(): Promise<{
  ok: boolean;
  sync_status: PromptSyncStatus;
}> {
  const res = await adminFetch("/admin/api/v1/prompts/sync", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ wait: false }),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Prompt sync failed: HTTP ${res.status}`);
  }
  return data;
}

export async function expandImagePrompt(
  req: PromptExpandRequest
): Promise<PromptExpandResponse> {
  const res = await adminFetch("/admin/api/v1/prompts/expand", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Prompt expand failed: HTTP ${res.status}`);
  }
  return data;
}

// ─── 持久化画廊 API (Server-side Gallery) ──────────────────────────

export interface ServerGalleryWork {
  id: string;
  prompt: string;
  revised_prompt?: string;
  model: string;
  size: string;
  quality?: string;
  mode?: "generations" | "edits";
  storage_type: "local" | "s3" | "blob" | "remote";
  file_path?: string;
  url: string;
  file_size?: number;
  mime_type?: string;
  latency_ms?: number;
  is_favorite: boolean;
  client_ip?: string;
  created_at: string;
  updated_at?: string;
}

export interface FetchGalleryOptions {
  page?: number;
  pageSize?: number;
  model?: string;
  favorite?: boolean;
  q?: string;
}

export async function fetchServerGallery(
  options?: FetchGalleryOptions
): Promise<{ items: ServerGalleryWork[]; total: number; page: number; page_size: number }> {
  const params = new URLSearchParams();
  if (options?.page) params.set("page", String(options.page));
  if (options?.pageSize) params.set("page_size", String(options.pageSize));
  if (options?.model && options.model !== "all") params.set("model", options.model);
  if (options?.favorite) params.set("favorite", "true");
  if (options?.q?.trim()) params.set("q", options.q.trim());

  const query = params.toString();
  const res = await adminFetch(`/admin/api/v1/gallery${query ? `?${query}` : ""}`);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch gallery failed: HTTP ${res.status}`);
  }
  return data;
}

export async function saveServerGalleryWork(work: {
  id?: string;
  prompt: string;
  revised_prompt?: string;
  model: string;
  size: string;
  quality?: string;
  mode?: "generations" | "edits";
  b64_json?: string;
  url?: string;
  latency_ms?: number;
  is_favorite?: boolean;
  timestamp?: number;
}): Promise<ServerGalleryWork> {
  const res = await adminFetch("/admin/api/v1/gallery", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(work),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Save work failed: HTTP ${res.status}`);
  }
  return data;
}

export async function batchMigrateServerGallery(items: Array<{
  id?: string;
  prompt: string;
  revised_prompt?: string;
  model: string;
  size: string;
  quality?: string;
  mode?: "generations" | "edits";
  b64_json?: string;
  url?: string;
  latency_ms?: number;
  is_favorite?: boolean;
  timestamp?: number;
}>): Promise<{ migrated: number; skipped: number; total: number }> {
  const res = await adminFetch("/admin/api/v1/gallery/batch-migrate", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ items }),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Batch migrate failed: HTTP ${res.status}`);
  }
  return data;
}

export async function toggleServerGalleryFavorite(
  id: string,
  isFavorite?: boolean
): Promise<ServerGalleryWork> {
  const res = await adminFetch(`/admin/api/v1/gallery/${encodeURIComponent(id)}/favorite`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(typeof isFavorite === "boolean" ? { is_favorite: isFavorite } : {}),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Toggle favorite failed: HTTP ${res.status}`);
  }
  return data;
}

export async function deleteServerGalleryWork(id: string): Promise<void> {
  const res = await adminFetch(`/admin/api/v1/gallery/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data?.error || `Delete work failed: HTTP ${res.status}`);
  }
}

export async function clearServerGallery(): Promise<number> {
  const res = await adminFetch("/admin/api/v1/gallery", {
    method: "DELETE",
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Clear gallery failed: HTTP ${res.status}`);
  }
  return data?.deleted_count || 0;
}

// ─── 持久化会话历史 API (Server-side Chat) ──────────────────────────

export interface ServerChatMessage {
  id: string;
  conversation_id?: string;
  session_key?: string;
  role: "user" | "assistant" | "system";
  content: string;
  model?: string;
  openui_mode?: boolean;
  skill?: string;
  tokens_prompt?: number;
  tokens_comp?: number;
  latency_ms?: number;
  citations?: string;
  created_at?: string;
}

export interface ServerConversation {
  id: string;
  title: string;
  model: string;
  system_prompt?: string;
  preset_id?: string;
  is_pinned: boolean;
  is_archived: boolean;
  tags?: string;
  message_count: number;
  created_at: string;
  updated_at: string;
}

export interface ServerChatPreset {
  id: string;
  name: string;
  description: string;
  icon: string;
  system_prompt: string;
  default_model?: string;
  is_builtin: boolean;
  sort_order: number;
  created_at?: string;
  updated_at?: string;
}

export interface ServerSearchHit {
  conversation_id: string;
  conversation_title: string;
  message_id: string;
  role: string;
  snippet: string;
  created_at: string;
}

// ─── 专业 AI 对话工作台 API ──────────────────────────────────────────

export async function fetchServerConversations(
  page = 1,
  limit = 30,
  q = "",
  archived = false
): Promise<{ conversations: ServerConversation[]; total: number; page: number; limit: number }> {
  const params = new URLSearchParams({
    page: String(page),
    limit: String(limit),
  });
  if (q.trim()) params.set("q", q.trim());
  if (archived) params.set("archived", "1");

  const res = await adminFetch(`/admin/api/v1/chat/conversations?${params.toString()}`);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch conversations failed: HTTP ${res.status}`);
  }
  return data;
}

export async function createServerConversation(payload: {
  id?: string;
  title?: string;
  model?: string;
  system_prompt?: string;
  preset_id?: string;
  is_pinned?: boolean;
}): Promise<ServerConversation> {
  const res = await adminFetch("/admin/api/v1/chat/conversations", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Create conversation failed: HTTP ${res.status}`);
  }
  return data;
}

export async function fetchServerConversationDetail(
  id: string
): Promise<{ conversation: ServerConversation; messages: ServerChatMessage[] }> {
  const res = await adminFetch(`/admin/api/v1/chat/conversations/${encodeURIComponent(id)}`);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch conversation failed: HTTP ${res.status}`);
  }
  return data;
}

export async function updateServerConversation(
  id: string,
  updates: {
    title?: string;
    model?: string;
    system_prompt?: string;
    preset_id?: string;
    is_pinned?: boolean;
    is_archived?: boolean;
    tags?: string;
  }
): Promise<ServerConversation> {
  const res = await adminFetch(`/admin/api/v1/chat/conversations/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(updates),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Update conversation failed: HTTP ${res.status}`);
  }
  return data;
}

export async function deleteServerConversation(id: string): Promise<void> {
  const res = await adminFetch(`/admin/api/v1/chat/conversations/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data?.error || `Delete conversation failed: HTTP ${res.status}`);
  }
}

export async function saveServerConversationMessages(
  id: string,
  messages: Array<{
    id?: string;
    role: string;
    content: string;
    model?: string;
    openuiMode?: boolean;
    skill?: string;
    tokensPrompt?: number;
    tokensComp?: number;
    latencyMs?: number;
    citations?: string;
    timestamp?: number;
  }>
): Promise<void> {
  const res = await adminFetch(`/admin/api/v1/chat/conversations/${encodeURIComponent(id)}/messages`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ messages }),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data?.error || `Save messages failed: HTTP ${res.status}`);
  }
}

export async function deleteServerConversationMessage(convId: string, msgId: string): Promise<void> {
  const res = await adminFetch(
    `/admin/api/v1/chat/conversations/${encodeURIComponent(convId)}/messages/${encodeURIComponent(msgId)}`,
    { method: "DELETE" }
  );
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data?.error || `Delete message failed: HTTP ${res.status}`);
  }
}

export async function searchServerChat(
  q: string,
  limit = 20
): Promise<{ query: string; hits: ServerSearchHit[]; count: number }> {
  const res = await adminFetch(`/admin/api/v1/chat/search?q=${encodeURIComponent(q)}&limit=${limit}`);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Search chat failed: HTTP ${res.status}`);
  }
  return data;
}

export async function fetchServerChatPresets(): Promise<ServerChatPreset[]> {
  const res = await adminFetch("/admin/api/v1/chat/presets");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch presets failed: HTTP ${res.status}`);
  }
  return data?.presets || [];
}

export async function saveServerChatPreset(preset: Partial<ServerChatPreset>): Promise<ServerChatPreset> {
  const res = await adminFetch("/admin/api/v1/chat/presets", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(preset),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Save preset failed: HTTP ${res.status}`);
  }
  return data;
}

export async function deleteServerChatPreset(id: string): Promise<void> {
  const res = await adminFetch(`/admin/api/v1/chat/presets/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data?.error || `Delete preset failed: HTTP ${res.status}`);
  }
}

export async function exportServerConversationMarkdown(id: string): Promise<string> {
  const res = await adminFetch(`/admin/api/v1/chat/conversations/${encodeURIComponent(id)}/export?format=markdown`);
  if (!res.ok) {
    throw new Error(`Export markdown failed: HTTP ${res.status}`);
  }
  return res.text();
}

export async function importServerConversations(payload: any): Promise<{ success: boolean; imported_count: number }> {
  const res = await adminFetch("/admin/api/v1/chat/conversations/import", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Import conversations failed: HTTP ${res.status}`);
  }
  return data;
}

export async function fetchServerChatMessages(
  sessionKey = "default"
): Promise<ServerChatMessage[]> {
  const res = await adminFetch(`/admin/api/v1/chat/messages?session_key=${encodeURIComponent(sessionKey)}`);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch chat failed: HTTP ${res.status}`);
  }
  return data?.messages || [];
}

export async function saveServerChatMessages(
  messages: Array<{
    id?: string;
    role: string;
    content: string;
    openuiMode?: boolean;
    skill?: string;
    timestamp?: number;
  }>,
  sessionKey = "default"
): Promise<void> {
  const res = await adminFetch("/admin/api/v1/chat/messages", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      session_key: sessionKey,
      messages,
    }),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data?.error || `Save chat failed: HTTP ${res.status}`);
  }
}

export async function clearServerChatMessages(sessionKey = "default"): Promise<void> {
  const res = await adminFetch(`/admin/api/v1/chat/messages?session_key=${encodeURIComponent(sessionKey)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data?.error || `Clear chat failed: HTTP ${res.status}`);
  }
}

// ─── 持久化提示词广场收藏 API (Server-side Prompt Favorites) ─────────

export async function fetchServerPromptFavorites(): Promise<string[]> {
  const res = await adminFetch("/admin/api/v1/prompts/favorites");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch prompt favorites failed: HTTP ${res.status}`);
  }
  return data?.favorite_ids || [];
}

export async function toggleServerPromptFavorite(id: string): Promise<string[]> {
  const res = await adminFetch("/admin/api/v1/prompts/favorites/toggle", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ id }),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Toggle prompt favorite failed: HTTP ${res.status}`);
  }
  return data?.favorite_ids || [];
}

export async function syncServerPromptFavorites(ids: string[]): Promise<string[]> {
  const res = await adminFetch("/admin/api/v1/prompts/favorites/sync", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ids }),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Sync prompt favorites failed: HTTP ${res.status}`);
  }
  return data?.favorite_ids || [];
}

// ─── 免费多模态 API (TTS / 声音克隆 / 声音设计 / AI 视频生成) ─────────────

export async function fetchVoices(): Promise<{ voices: VoiceDescriptor[]; total: number }> {
  const res = await fetch("/v1/audio/voices");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Fetch voices failed: HTTP ${res.status}`);
  }
  return data;
}

export async function synthesizeSpeech(input: string, voice = "zh_female_shuangkuai", speed = 1.0): Promise<Blob> {
  const res = await fetch("/v1/audio/speech", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Prefer-Tag": "permanent-free",
    },
    body: JSON.stringify({
      model: "tts-1",
      input,
      voice,
      response_format: "mp3",
      speed,
    }),
  });
  if (!res.ok) {
    let errMsg = `TTS synthesis failed: HTTP ${res.status}`;
    try {
      const errJson = await res.json();
      errMsg = errJson?.error?.message || errJson?.error || errMsg;
    } catch {
      // ignore
    }
    throw new Error(errMsg);
  }
  return await res.blob();
}

export async function voiceDesign(
  voicePrompt: string,
  previewText?: string,
  preferredName?: string,
): Promise<{ voice_id: string; preview_url: string }> {
  const res = await fetch("/v1/audio/voices/design", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      voice_prompt: voicePrompt,
      preview_text: previewText || "各位听众朋友，大家好，这是我的定制音色测试。",
      preferred_name: preferredName || "custom_voice",
    }),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Voice design failed: HTTP ${res.status}`);
  }
  return data;
}

export async function voiceClone(
  audioUrl: string,
  prefix?: string,
): Promise<{ voice_id: string }> {
  const res = await fetch("/v1/audio/voices/clone", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      audio_url: audioUrl,
      prefix: prefix || "clone_voice",
    }),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Voice clone failed: HTTP ${res.status}`);
  }
  return data;
}

export async function fetchVideoModels(): Promise<VideoModelInfo[]> {
  const res = await fetch("/v1/videos/models");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Fetch video models failed: HTTP ${res.status}`);
  }
  return data.data || [];
}

export async function submitVideo(
  prompt: string,
  imageUrl?: string,
  duration = 5,
  model = "THUDM/CogVideoX-5b",
  audioUrl?: string,
): Promise<{ task_id: string; status: string; provider?: string }> {
  const res = await fetch("/v1/videos/generations", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      model,
      prompt,
      image_url: imageUrl,
      audio_url: audioUrl,
      duration,
      wait: false,
    }),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Submit video failed: HTTP ${res.status}`);
  }
  return data;
}

export async function getVideoTask(taskId: string): Promise<VideoTask> {
  const res = await fetch(`/v1/videos/tasks/${taskId}`);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Get video task failed: HTTP ${res.status}`);
  }
  return data;
}

export async function fetchVideoTasks(): Promise<{ tasks: VideoTask[]; total: number }> {
  const res = await adminFetch("/admin/api/v1/multimodal/video-tasks");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch video tasks failed: HTTP ${res.status}`);
  }
  return data;
}

export interface ProviderChannel {
  id: string;
  provider_id: string;
  channel_name: string;
  auth_type: string;
  credential: string;
  status: string;
  is_free_tier: boolean;
  capabilities: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export async function fetchProviderChannels(): Promise<{ channels: ProviderChannel[]; total: number }> {
  const res = await adminFetch("/admin/api/v1/providers/channels");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch provider channels failed: HTTP ${res.status}`);
  }
  return data;
}

export interface AudioHistoryItem {
  id: string;
  voice_id: string;
  voice_name: string;
  provider: string;
  input_text: string;
  speed: number;
  format: string;
  audio_url: string;
  duration: number;
  created_at: string;
}

export async function fetchAudioHistory(): Promise<{ records: AudioHistoryItem[]; total: number }> {
  const res = await fetch("/v1/audio/history");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch audio history failed: HTTP ${res.status}`);
  }
  return data;
}

