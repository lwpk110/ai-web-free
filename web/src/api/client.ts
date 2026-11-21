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
  image_models?: string[];
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
  locale?: string;
  sample_text: string;
  tags: string[];
  preview_url?: string;
  available?: boolean;
  cached_local?: boolean;
  status_reason?: string;
}

export interface TTSProviderSummary {
  available: boolean;
  configured?: boolean;
  endpoint?: string;
  healthy?: boolean;
  zero_config?: boolean;
  active?: number;
  retired?: boolean;
  last_error?: string;
  reason?: string;
}

export interface VoicesCatalogResponse {
  voices: VoiceDescriptor[];
  total: number;
  available_total?: number;
  cached_local_total?: number;
  providers?: Record<string, TTSProviderSummary>;
}

export interface SynthesizeSpeechMeta {
  blob: Blob;
  provider: string;
  voice: string;
  fallback: boolean;
  fallbackReason?: string;
}

export interface PullLocalVoicesResult {
  catalog_saved: boolean;
  catalog_total: number;
  available_total: number;
  edge_live_synced: number;
  pulled_samples: number;
  already_cached: number;
  failed_samples: number;
  cached_total: number;
  errors?: string[];
  providers?: Record<string, TTSProviderSummary>;
}

export interface VideoTask {
  task_id: string;
  vid_id?: string;
  account_email?: string;
  provider: string;
  model: string;
  prompt: string;
  image_url?: string;
  audio_url?: string;
  duration: number;
  status: "PENDING" | "RUNNING" | "SUCCEEDED" | "FAILED";
  progress: number;
  video_url?: string;
  local_url?: string;
  error_msg?: string;
  upstream_session_id?: string;
  upstream_msg_id?: string;
  upstream_seq?: number;
  upstream_asset_path?: string;
  upstream_asset_url?: string;
  expected_bytes?: number;
  upstream_meta?: string;
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
  standby?: number;
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

export interface ProxyProfileSummary {
  profile: string;
  label: string;
  total: number;
  clash_count: number;
}

export interface ClashMappedPort {
  port: number;
  proxy_url: string;
  node_name: string;
  region: string;
  protocol: string;
  server?: string;
}

export interface ClashSyncResult {
  target_profile: string;
  total_parsed: number;
  filtered_out: number;
  mapped_count: number;
  start_port: number;
  end_port: number;
  mapped_nodes: ClashMappedPort[];
  config_path?: string;
  config_written: boolean;
  mihomo_reloaded: boolean;
  reload_method?: string;
  reload_warning?: string;
  generated_config?: string;
  synced_at: string;
}

export interface ClashConfigSummary {
  sub_configured: boolean;
  sub_url_masked?: string;
  target_profile: string;
  listen_host: string;
  start_port: number;
  max_ports: number;
  exclude_regex: string;
  priority_regions: string;
  config_path: string;
  mihomo_api_url: string;
  last_sync?: ClashSyncResult | null;
}

export interface ProxyNodeStatus {
  url: string;
  profile?: string;
  node_name?: string;
  region?: string;
  source?: string;
  port?: number;
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

export interface BulkActionTaskItemResult {
  id: string;
  email?: string;
  ok: boolean;
  error?: string;
  message?: string;
}

export interface BulkActionTask {
  id: string;
  provider: string;
  action: string;
  action_label?: string;
  status: "running" | "completed" | "cancelled" | string;
  total: number;
  completed: number;
  success: number;
  failed: number;
  cancelled?: number;
  current_account_id?: string;
  current_email?: string;
  current_phase_label?: string;
  current_started_at?: string;
  queued_account_ids?: string[];
  results: BulkActionTaskItemResult[];
  created_at: string;
  updated_at: string;
  dismissed?: boolean;
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

export interface ServerHealthSummary {
  status: "healthy" | "degraded" | "unhealthy" | string;
  services_total: number;
  services_running: number;
  services_disabled: number;
  services_stopped: number;
  subsystems_total: number;
  subsystems_healthy: number;
  subsystems_degraded: number;
  goroutines: number;
  mem_alloc_mb: number;
}

export interface BackgroundServiceStatus {
  name: string;
  label: string;
  category: "worker" | "scheduler" | string;
  enabled: boolean;
  running: boolean;
  status: "running" | "disabled" | "stopped" | string;
  interval?: string;
  started_at?: string;
  last_tick_at?: string;
  detail?: string;
}

export interface SubsystemHealth {
  name: string;
  label: string;
  status: "healthy" | "degraded" | "disabled" | "unhealthy" | string;
  detail: string;
  meta?: Record<string, unknown>;
}

export interface RuntimeMetrics {
  pid: number;
  go_version: string;
  os: string;
  arch: string;
  num_cpu: number;
  goroutines: number;
  mem_alloc_bytes: number;
  mem_alloc_mb: number;
  mem_sys_bytes: number;
  mem_sys_mb: number;
  heap_inuse_bytes: number;
  heap_objects: number;
  num_gc: number;
  last_gc_at?: string;
}

export interface ServerStatusReport {
  status: "healthy" | "degraded" | "unhealthy" | string;
  service: string;
  version: string;
  uptime_seconds: number;
  started_at: string;
  checked_at: string;
  summary: ServerHealthSummary;
  runtime: RuntimeMetrics;
  background_services: BackgroundServiceStatus[];
  subsystems: SubsystemHealth[];
}

export interface TableSyncStatus {
  table_name: string;
  last_pull_at?: string;
  initial_reconciled: boolean;
  pending_outbox: number;
  pushed_rows: number;
  pulled_rows: number;
  deleted_rows: number;
  has_blob_data?: boolean;
}

export interface DBSyncStatus {
  enabled: boolean;
  mode: "materialized" | "direct" | "local_only" | string;
  state:
    | "idle"
    | "connecting"
    | "hydrating"
    | "syncing"
    | "degraded"
    | "disabled"
    | "stopped"
    | string;
  local_path?: string;
  cloud_target?: string;
  cloud_connected: boolean;
  initial_hydrated: boolean;
  outbox_pending: number;
  last_push_at?: string;
  last_pull_at?: string;
  last_sync_latency_ms: number;
  total_pushed: number;
  total_pulled: number;
  total_deleted: number;
  consecutive_failures: number;
  last_error?: string;
  next_retry_at?: string;
  push_interval?: string;
  pull_interval?: string;
  tables?: TableSyncStatus[];
}

export interface TriggerDBSyncResponse {
  ok: boolean;
  direction: "both" | "push" | "pull" | string;
  error?: string;
  status: DBSyncStatus;
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
  bulk_task?: BulkActionTask | null;
  audit_summary?: AuditSummary;
  server_health?: ServerHealthSummary;
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
  has_totp_secret?: boolean;
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

export async function fetchProviderDescriptors(): Promise<ProviderDescriptor[]> {
  const res = await adminFetch("/admin/api/v1/providers");
  if (!res.ok) throw new Error(`Providers failed: HTTP ${res.status}`);
  const data = await res.json();
  return Array.isArray(data?.providers) ? data.providers : [];
}

export async function fetchServerStatus(): Promise<ServerStatusReport> {
  const res = await adminFetch("/admin/api/v1/system/status");
  if (!res.ok) throw new Error(`Server status failed: HTTP ${res.status}`);
  return res.json();
}

export async function fetchDBSyncStatus(): Promise<DBSyncStatus> {
  const res = await adminFetch("/admin/api/v1/db/sync/status");
  if (!res.ok) throw new Error(`DB sync status failed: HTTP ${res.status}`);
  return res.json();
}

export async function triggerDBSync(
  direction: "both" | "push" | "pull" = "both"
): Promise<TriggerDBSyncResponse> {
  const res = await adminFetch("/admin/api/v1/db/sync/trigger", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ direction }),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data?.error || `Trigger DB sync failed: HTTP ${res.status}`);
  }
  return data;
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
  bulk_task?: BulkActionTask | null;
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

export async function bulkAccountAction(
  provider: string,
  accountIds: string[],
  action: AccountActionType,
  options?: { headless?: boolean; async?: boolean }
): Promise<{
  provider: string;
  action: string;
  task?: BulkActionTask;
  result?: {
    total: number;
    success: number;
    failed: number;
    results: { id: string; ok: boolean; error?: string; message?: string }[];
  };
  stats?: PoolStats;
}> {
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(provider)}/accounts/bulk-action`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        account_ids: accountIds,
        action,
        headless: options?.headless,
        async: options?.async,
      }),
    }
  );
  if (!res.ok) {
    let errMessage = `HTTP ${res.status}`;
    try {
      const data = await res.json();
      errMessage = data?.error?.message || errMessage;
    } catch {
      const text = await res.text().catch(() => "");
      if (text) {
        errMessage = text.length > 120 ? text.slice(0, 120) + "..." : text;
      }
    }
    const err = new Error(errMessage) as any;
    err.status = res.status;
    throw err;
  }
  return res.json();
}

export async function cancelBulkActionTask(
  provider = "chatgpt",
  taskId?: string
): Promise<{ ok: boolean }> {
  const res = await adminFetch(
    `/admin/api/v1/providers/${encodeURIComponent(provider)}/accounts/bulk-action/cancel`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ task_id: taskId }),
    }
  );
  if (!res.ok) throw new Error(`Cancel bulk task failed: HTTP ${res.status}`);
  return res.json();
}

export type AccountActionType =
  | "probe"
  | "refresh"
  | "enable"
  | "disable"
  | "standby"
  | "activate"
  | "delete"
  | "totp"
  | "sync_chrome"
  | "extract_chrome"
  | "browser_login"
  | "get_cookie"
  | "update_cookie"
  | "login";

export async function executeAccountAction(
  provider: string,
  accountId: string,
  action: AccountActionType,
  options?: { headless?: boolean; cookie?: string }
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
  if (typeof options?.cookie === "string") {
    payload.cookie = options.cookie;
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
  payload: {
    task_id?: string;
    account_id?: string;
    account_ids?: string[];
    cancel_all?: boolean;
    queue?: "login" | "renew" | "all";
  }
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
  payload: { kind?: "login" | "probe" | "bulk"; task_id?: string }
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
  headless = false,
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
    login_task?: KamiLoginTask;
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
        headless: params.headless ?? false,
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

export async function fetchProxies(profile?: string): Promise<{
  active_profile?: string;
  env_profile?: string;
  view_profile?: string;
  profiles?: ProxyProfileSummary[];
  clash_config?: ClashConfigSummary;
  summary: ProxySummary;
  nodes: ProxyNodeStatus[];
}> {
  const qs = profile ? `?profile=${encodeURIComponent(profile)}` : "";
  const res = await adminFetch(`/admin/api/v1/proxies${qs}`);
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
  persist = true,
  profile?: string
): Promise<{
  profile?: string;
  active_profile?: string;
  proxies_count: number;
  proxies: string[];
  migrated: number;
  profiles?: ProxyProfileSummary[];
}> {
  const res = await adminFetch("/admin/api/v1/proxies", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ proxies, persist, profile }),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Update proxies failed: HTTP ${res.status}`);
  }
  return data;
}

export async function switchProxyProfile(profile: string): Promise<{
  active_profile: string;
  env_profile: string;
  migrated: number;
  summary: ProxySummary;
  nodes: ProxyNodeStatus[];
  profiles: ProxyProfileSummary[];
}> {
  const res = await adminFetch("/admin/api/v1/proxies/profile", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ profile }),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Switch profile failed: HTTP ${res.status}`);
  }
  return data;
}

export async function syncClashProxies(params: {
  sub_url?: string;
  raw_subscription?: string;
  target_profile?: string;
  listen_host?: string;
  start_port?: number;
  max_ports?: number;
  exclude_regex?: string;
  priority_regions?: string;
  config_path?: string;
  write_config?: boolean;
  reload_mihomo?: boolean;
  activate_profile?: boolean;
}): Promise<{
  result: ClashSyncResult;
  active_profile: string;
  migrated: number;
  profiles: ProxyProfileSummary[];
}> {
  const res = await adminFetch("/admin/api/v1/proxies/clash-sync", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(params),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Clash sync failed: HTTP ${res.status}`);
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
  const useCacheParam = options?.refresh ? "" : "?use_cache=true";
  if (apiKey) {
    const res = await fetch(`/v1/models${useCacheParam}`, {
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

  const refreshParam = options?.refresh ? "?refresh=true" : "";
  try {
    const res = await adminFetch(
      `/admin/api/v1/models${refreshParam}`
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

  const res = await fetch(`/v1/models${useCacheParam}`);
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

export type OpenAIChatContentPart =
  | { type: "text"; text: string }
  | { type: "image_url"; image_url: { url: string } }
  | { type: "file"; file: { file_id: string } };

export async function uploadOpenAIFile(
  file: File,
  apiKey?: string,
  signal?: AbortSignal
): Promise<{ id: string; filename: string; bytes: number; purpose?: string }> {
  const form = new FormData();
  form.append("file", file);
  form.append("purpose", "assistants");
  const headers: Record<string, string> = {};
  if (apiKey) {
    headers["Authorization"] = `Bearer ${apiKey}`;
  }
  const res = await fetch("/v1/files", {
    method: "POST",
    headers,
    body: form,
    signal,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data?.error?.message || data?.error || `File upload failed: HTTP ${res.status}`);
  }
  return data;
}

export async function streamChatCompletion(params: {
  messages: Array<{ role: string; content: string | OpenAIChatContentPart[] }>;
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

export interface VideoPromptExpandRequest {
  prompt: string;
  action?: "expand" | "polish";
  camera_style?: string;
  model?: string;
  duration?: number;
  has_image?: boolean;
}

export interface VideoPromptExpandResponse {
  title_zh: string;
  camera_move: string;
  prompt_zh: string;
  prompt_en: string;
  action: "expand" | "polish" | string;
  source: string;
}

export async function expandVideoPrompt(
  req: VideoPromptExpandRequest,
  signal?: AbortSignal,
): Promise<VideoPromptExpandResponse> {
  const res = await adminFetch("/admin/api/v1/prompts/video-expand", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
    signal,
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Video prompt expand failed: HTTP ${res.status}`);
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
  category?: "general" | "character_portrait" | "scene_anchor" | "storyboard_frame" | "reference";
  source?: string;
  project_id?: string;
  project_title?: string;
  role_name?: string;
  shot_index?: number;
  tags?: string[];
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
  category?: string;
  source?: string;
  projectId?: string;
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
  if (options?.category && options.category !== "all") params.set("category", options.category);
  if (options?.source && options.source !== "all") params.set("source", options.source);
  if (options?.projectId) params.set("project_id", options.projectId);

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
  category?: string;
  source?: string;
  project_id?: string;
  project_title?: string;
  role_name?: string;
  shot_index?: number;
  tags?: string[];
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
  category?: string;
  source?: string;
  project_id?: string;
  project_title?: string;
  role_name?: string;
  shot_index?: number;
  tags?: string[];
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

export async function updateServerGalleryMetadata(
  id: string,
  metadata: { category?: string; tags?: string[] },
): Promise<ServerGalleryWork> {
  const res = await adminFetch(`/admin/api/v1/gallery/${encodeURIComponent(id)}/metadata`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(metadata),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data?.error || `Update gallery metadata failed: HTTP ${res.status}`);
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
  parent_id?: string;
  session_key?: string;
  role: "user" | "assistant" | "system";
  content: string;
  parts_json?: string;
  attachments_json?: string;
  metadata_json?: string;
  status_json?: string;
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
  head_message_id?: string;
  custom_json?: string;
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
  head_message_id?: string;
  custom_json?: string;
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
    head_message_id?: string;
    custom_json?: string;
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

export interface ServerChatMessagePayload {
  id?: string;
  parentId?: string;
  role: string;
  content: string;
  partsJson?: string;
  attachmentsJson?: string;
  metadataJson?: string;
  statusJson?: string;
  model?: string;
  openuiMode?: boolean;
  skill?: string;
  tokensPrompt?: number;
  tokensComp?: number;
  latencyMs?: number;
  citations?: string;
  timestamp?: number;
}

export async function appendServerConversationMessage(
  id: string,
  message: ServerChatMessagePayload,
  moveHead = true
): Promise<{ success: boolean; conversation_id: string; message: ServerChatMessage }> {
  const res = await adminFetch(`/admin/api/v1/chat/conversations/${encodeURIComponent(id)}/messages/append`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ message, move_head: moveHead }),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data?.error || `Append message failed: HTTP ${res.status}`);
  }
  return data;
}

export async function saveServerConversationMessages(
  id: string,
  messages: ServerChatMessagePayload[],
  headMessageId?: string
): Promise<void> {
  const res = await adminFetch(`/admin/api/v1/chat/conversations/${encodeURIComponent(id)}/messages`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ messages, head_message_id: headMessageId }),
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

export async function fetchVoices(): Promise<VoicesCatalogResponse> {
  const res = await adminFetch("/admin/api/v1/audio/voices");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Fetch voices failed: HTTP ${res.status}`);
  }
  return data;
}

export async function synthesizeSpeechWithMeta(
  input: string,
  voice = "zh-CN-XiaoxiaoNeural",
  speed = 1.0,
  model = "tts-1",
  signal?: AbortSignal,
  options?: { allowFallback?: boolean; preferProvider?: string },
): Promise<SynthesizeSpeechMeta> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (options?.allowFallback) {
    headers["X-Allow-Fallback"] = "true";
  }
  if (options?.preferProvider) {
    headers["X-Prefer-Provider"] = options.preferProvider;
  }
  const res = await adminFetch("/admin/api/v1/audio/speech", {
    method: "POST",
    headers,
    signal,
    body: JSON.stringify({
      model: model || "tts-1",
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
  const blob = await res.blob();
  return {
    blob,
    provider: res.headers.get("X-TTS-Provider") || "",
    voice: res.headers.get("X-TTS-Voice") || voice,
    fallback: res.headers.get("X-TTS-Fallback") === "true",
    fallbackReason: res.headers.get("X-TTS-Fallback-Reason") || undefined,
  };
}

export async function synthesizeSpeech(
  input: string,
  voice = "zh-CN-XiaoxiaoNeural",
  speed = 1.0,
  model = "tts-1",
  signal?: AbortSignal,
): Promise<Blob> {
  const result = await synthesizeSpeechWithMeta(input, voice, speed, model, signal);
  return result.blob;
}

export function getVoicePreviewUrl(voiceId: string): string {
  const trimmed = voiceId.trim();
  if (trimmed.includes("/")) {
    return `/admin/api/v1/audio/voice-preview?id=${encodeURIComponent(trimmed)}`;
  }
  return `/admin/api/v1/audio/voices/${encodeURIComponent(trimmed)}/preview`;
}

export async function fetchVoicePreview(
  voiceId: string,
  previewUrl?: string,
  signal?: AbortSignal,
): Promise<{ blob: Blob; cacheStatus: string; provider: string; voice: string }> {
  let targetUrl = previewUrl || getVoicePreviewUrl(voiceId);
  if (targetUrl.startsWith("/v1/audio/")) {
    targetUrl = `/admin/api/v1/audio/${targetUrl.slice("/v1/audio/".length)}`;
  }
  const isAdminRoute = targetUrl.startsWith("/admin/api/v1/");
  const res = isAdminRoute ? await adminFetch(targetUrl, { signal }) : await fetch(targetUrl, { signal });
  if (!res.ok) {
    let errMsg = `试听请求失败: HTTP ${res.status}`;
    try {
      const errJson = await res.json();
      errMsg = errJson?.error?.message || errJson?.error || errMsg;
    } catch {
      // ignore
    }
    throw new Error(errMsg);
  }
  return {
    blob: await res.blob(),
    cacheStatus: res.headers.get("X-Voice-Cache") || "miss",
    provider: res.headers.get("X-TTS-Provider") || "",
    voice: res.headers.get("X-TTS-Voice") || voiceId,
  };
}

export async function pullLocalVoices(options?: {
  voices?: string[];
  include_samples?: boolean;
  refresh_live?: boolean;
  force?: boolean;
  limit?: number;
}): Promise<PullLocalVoicesResult> {
  const res = await adminFetch("/admin/api/v1/audio/voices/pull-local", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      include_samples: options?.include_samples ?? true,
      refresh_live: options?.refresh_live ?? true,
      force: options?.force ?? false,
      limit: options?.limit ?? 24,
      ...(options?.voices ? { voices: options.voices } : {}),
    }),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || data?.error || `Pull local voices failed: HTTP ${res.status}`);
  }
  return data;
}

export async function voiceDesign(
  voicePrompt: string,
  previewText?: string,
  preferredName?: string,
): Promise<{ voice_id: string; preview_url: string }> {
  const res = await adminFetch("/admin/api/v1/audio/voices/design", {
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
  const res = await adminFetch("/admin/api/v1/audio/voices/clone", {
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
  const res = await adminFetch("/admin/api/v1/videos/models");
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
  options?: { noMockFallback?: boolean; aspectRatio?: string },
): Promise<{ task_id: string; status: string; provider?: string; local_url?: string }> {
  const res = await adminFetch("/admin/api/v1/videos/generations", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      model,
      prompt,
      image_url: imageUrl,
      audio_url: audioUrl,
      duration,
      aspect_ratio: options?.aspectRatio,
      no_mock_fallback: Boolean(options?.noMockFallback),
      wait: false,
    }),
  });
  const data = await res.json();
  if (!res.ok) {
    const err = new Error(data?.error?.message || `Submit video failed: HTTP ${res.status}`) as Error & {
      code?: string;
      status?: number;
      provider?: string;
    };
    err.code = data?.error?.code;
    err.status = res.status;
    err.provider = data?.error?.provider;
    throw err;
  }
  return data;
}

export async function getVideoTask(taskId: string): Promise<VideoTask> {
  const res = await adminFetch(`/admin/api/v1/videos/tasks/${encodeURIComponent(taskId)}`);
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

export async function deleteVideoTask(taskId: string): Promise<void> {
  const res = await adminFetch(`/admin/api/v1/multimodal/video-tasks/${encodeURIComponent(taskId)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data?.error || `Delete video task failed: HTTP ${res.status}`);
  }
}

export async function clearFailedVideoTasks(): Promise<{ deleted_count: number }> {
  const res = await adminFetch("/admin/api/v1/multimodal/video-tasks?status=FAILED", {
    method: "DELETE",
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data?.error || `Clear failed video tasks failed: HTTP ${res.status}`);
  }
  return data;
}

export async function rescueVideoTask(
  taskId: string
): Promise<{ task: VideoTask; message?: string }> {
  const res = await adminFetch(
    `/admin/api/v1/multimodal/video-tasks/${encodeURIComponent(taskId)}/rescue`,
    { method: "POST" }
  );
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data?.error || `Rescue video task failed: HTTP ${res.status}`);
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
  const res = await adminFetch("/admin/api/v1/audio/history");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch audio history failed: HTTP ${res.status}`);
  }
  return data;
}

export interface OpsLogEntry {
  id: string;
  timestamp: string;
  source: "backend" | "frontend";
  level: "error" | "warn" | "info" | "debug";
  module?: string;
  message: string;
  detail?: string;
  page?: string;
}

export interface OpsLogSummary {
  capacity: number;
  buffered_count: number;
  total_backend: number;
  total_frontend: number;
  error_count: number;
  warn_count: number;
}

export interface OpsLogsResponse {
  summary: OpsLogSummary;
  entries: OpsLogEntry[];
}

export async function fetchOpsLogs(params?: {
  source?: string;
  level?: string;
  q?: string;
  limit?: number;
}): Promise<OpsLogsResponse> {
  const sp = new URLSearchParams();
  if (params?.source) sp.set("source", params.source);
  if (params?.level) sp.set("level", params.level);
  if (params?.q) sp.set("q", params.q);
  if (params?.limit) sp.set("limit", String(params.limit));
  const qs = sp.toString();
  const res = await adminFetch(`/admin/api/v1/ops-logs${qs ? `?${qs}` : ""}`);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Fetch ops logs failed: HTTP ${res.status}`);
  }
  return data;
}

export async function clearOpsLogs(): Promise<void> {
  const res = await adminFetch("/admin/api/v1/ops-logs", { method: "DELETE" });
  if (!res.ok) {
    throw new Error(`Clear ops logs failed: HTTP ${res.status}`);
  }
}

export type DramaRoutingPolicy = "free_first" | "quality_first" | "fast";

export interface DramaRecommendedPlan {
  llm_model: string;
  image_model: string;
  tts_voice: string;
  video_model: string;
  fallback_mode: "ken_burns" | string;
}

export interface DramaModelProvider {
  id?: string;
  name?: string;
  status?: string;
  available: boolean;
  active: number;
  models: string[];
  reason?: string;
}

export interface DramaCapabilities {
  llm: {
    available: boolean;
    active: number;
    models: string[];
    providers?: Record<string, DramaModelProvider>;
    reason?: string;
  };
  image: {
    available: boolean;
    edit_available: boolean;
    default_model: string;
    reason?: string;
    providers: Record<string, DramaModelProvider>;
  };
  tts: {
    available: boolean;
    voice_design_available: boolean;
    voice_clone_available: boolean;
    providers: Record<string, { available: boolean; active?: number; zero_config?: boolean }>;
  };
  stt: { available: boolean; active: number; models: string[]; reason?: string };
  video: {
    available: boolean;
    lipsync_available: boolean;
    reason?: string;
    providers: Record<
      string,
      { available: boolean; active: number; models: string[]; supports_i2v: boolean; supports_a2v: boolean }
    >;
  };
  recommended: Record<DramaRoutingPolicy, DramaRecommendedPlan>;
}

export type DramaNodeStatus = "pending" | "running" | "ready" | "stale" | "failed" | "degraded";

export interface DramaNodeStateSchema {
  status: DramaNodeStatus;
  input_hash?: string;
  output?: string;
  locked: boolean;
  attempts: number;
  error?: string;
  status_note?: string;
  task_id?: string;
  model_used?: string;
  updated_at?: string;
}

export interface DramaAssetSchema {
  id: string;
  kind: "image" | "audio" | "video";
  url: string;
  sha256?: string;
  model?: string;
  prompt?: string;
  created_at?: string;
}

export interface DramaProjectSchema {
  id: string;
  schema_version: number;
  revision: number;
  script: {
    title: string;
    outline: string;
    genre: string;
    aspect_ratio: string;
    style: string;
    target_duration_sec?: number;
    routing_policy: string;
    script_model_override?: string;
    image_model_override?: string;
    video_model_override?: string;
    allowed_image_providers?: string[];
    allowed_video_providers?: string[];
    allowed_tts_providers?: string[];
    default_tts_engine?: string;
    default_tts_voice_id?: string;
    default_tts_voice_name?: string;
    image_concurrency?: number;
    auto_fallback_ken_burns?: boolean;
    bgm_url?: string;
    bgm_volume?: number;
    show_subtitles?: boolean;
  };
  analysis?: {
    beats?: Array<{ id: string; summary: string; conflict?: string; hook_score?: number }>;
    highlight_beat_ids?: string[];
  };
  characters: Array<{
    id: string;
    name: string;
    role: string;
    avatar_color?: string;
    appearance_bible: string;
    portrait_prompt?: string;
    voice: { engine: string; id: string; name?: string; speed?: number; pitch?: number };
    portrait?: string;
  }>;
  scenes: Array<{
    id: string;
    name: string;
    environment: string;
    lighting: string;
    anchor?: string;
  }>;
  shots: Array<{
    id: string;
    index: number;
    beat_id?: string;
    scene_id?: string;
    speaker_id?: string;
    continuous?: boolean;
    shot_type: string;
    camera_move: string;
    action: string;
    dialogue: string;
    continuity_notes?: string;
    polished_video_prompt?: string;
    transition?: string;
    duration_sec: number;
    actual_audio_duration?: number;
    keyframe?: string;
    audio?: string;
    video?: string;
  }>;
  assets: Record<string, DramaAssetSchema>;
  nodes: Record<string, DramaNodeStateSchema>;
  timeline?: { tracks?: unknown[]; srt?: string };
}

export interface DramaLayoutData {
  positions?: Record<string, { x: number; y: number }>;
  checkpoint_after_cast?: boolean;
  checkpoint_after_keyframes?: boolean;
}

export interface DramaRunLogEntry {
  timestamp: string;
  level: "info" | "success" | "warn" | "error";
  node_key?: string;
  message: string;
}

export interface DramaRunSummary {
  total: number;
  ready: number;
  degraded: number;
  skipped: number;
  running: number;
  failed: number;
  pending: number;
  duration_ms: number;
}

export interface DramaRunRecord {
  id: string;
  project_id: string;
  status: "running" | "completed" | "partial" | "failed" | "cancelled";
  error?: string;
  logs?: DramaRunLogEntry[];
  summary?: DramaRunSummary;
  created_at?: string;
  updated_at?: string;
  finished_at?: string;
}

export interface DramaRunEvent {
  type: "node.update" | "run.update" | "run.log";
  run_id: string;
  project_id: string;
  node_key?: string;
  node_state?: DramaNodeStateSchema;
  status?: string;
  message?: string;
  log?: DramaRunLogEntry;
  summary?: DramaRunSummary;
  revision?: number;
  timestamp: string;
}

export interface DramaProjectRecord {
  id: string;
  title: string;
  genre: string;
  aspect_ratio: string;
  style_preset: string;
  routing_policy: DramaRoutingPolicy;
  outline: string;
  graph_json: string;
  status: string;
  shot_count: number;
  schema_version?: number;
  revision?: number;
  project?: DramaProjectSchema;
  layout?: DramaLayoutData;
  active_run_id?: string;
  latest_run?: DramaRunRecord;
  created_at?: string;
  updated_at?: string;
}

export async function fetchDramaCapabilities(): Promise<DramaCapabilities> {
  const res = await adminFetch("/admin/api/v1/drama/capabilities");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch drama capabilities failed: HTTP ${res.status}`);
  }
  return data;
}

export async function fetchDramaProjects(): Promise<DramaProjectRecord[]> {
  const res = await adminFetch("/admin/api/v1/drama/projects");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch drama projects failed: HTTP ${res.status}`);
  }
  return data.data || [];
}

export async function fetchDramaProject(id: string): Promise<DramaProjectRecord> {
  const res = await adminFetch(`/admin/api/v1/drama/projects/${encodeURIComponent(id)}`);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch drama project failed: HTTP ${res.status}`);
  }
  return data;
}

export async function saveDramaProject(
  payload: Partial<DramaProjectRecord> & { id: string; title: string },
): Promise<DramaProjectRecord> {
  const res = await adminFetch(`/admin/api/v1/drama/projects/${encodeURIComponent(payload.id)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Save drama project failed: HTTP ${res.status}`);
  }
  return data;
}

export async function deleteDramaProject(id: string): Promise<void> {
  const res = await adminFetch(`/admin/api/v1/drama/projects/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    throw new Error(`Delete drama project failed: HTTP ${res.status}`);
  }
}

export async function cacheDramaMedia(payload: {
  url?: string;
  data_url?: string;
  category?: "images" | "videos" | "audio";
  id?: string;
  mime_type?: string;
}): Promise<{ local_url: string; rel_path?: string; mime_type?: string; cached: boolean }> {
  const res = await adminFetch("/admin/api/v1/drama/media/cache", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Cache drama media failed: HTTP ${res.status}`);
  }
  return data;
}

export async function startDramaRun(
  projectId: string,
  opts?: { targets?: string[]; force?: string[]; until?: string },
): Promise<{ run: DramaRunRecord; already_running: boolean }> {
  const res = await adminFetch(`/admin/api/v1/drama/projects/${encodeURIComponent(projectId)}/runs`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(opts || {}),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Start drama run failed: HTTP ${res.status}`);
  }
  return data;
}

export async function fetchDramaRuns(projectId: string): Promise<DramaRunRecord[]> {
  const res = await adminFetch(`/admin/api/v1/drama/projects/${encodeURIComponent(projectId)}/runs`);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch drama runs failed: HTTP ${res.status}`);
  }
  return data.data || [];
}

export async function fetchDramaRun(projectId: string, runId: string): Promise<DramaRunRecord> {
  const res = await adminFetch(
    `/admin/api/v1/drama/projects/${encodeURIComponent(projectId)}/runs/${encodeURIComponent(runId)}`,
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `Fetch drama run failed: HTTP ${res.status}`);
  }
  return data;
}

export async function lockDramaNode(projectId: string, nodeKey: string): Promise<{ revision: number; project: DramaProjectSchema }> {
  const res = await adminFetch(
    `/admin/api/v1/drama/projects/${encodeURIComponent(projectId)}/nodes/${encodeURIComponent(nodeKey)}/lock`,
    { method: "POST" },
  );
  const data = await res.json();
  if (!res.ok) throw new Error(data?.error || `Lock node failed: HTTP ${res.status}`);
  return data;
}

export async function unlockDramaNode(projectId: string, nodeKey: string): Promise<{ revision: number; project: DramaProjectSchema }> {
  const res = await adminFetch(
    `/admin/api/v1/drama/projects/${encodeURIComponent(projectId)}/nodes/${encodeURIComponent(nodeKey)}/unlock`,
    { method: "POST" },
  );
  const data = await res.json();
  if (!res.ok) throw new Error(data?.error || `Unlock node failed: HTTP ${res.status}`);
  return data;
}

export async function retryDramaNode(projectId: string, nodeKey: string): Promise<{ run: DramaRunRecord; already_running: boolean }> {
  const res = await adminFetch(
    `/admin/api/v1/drama/projects/${encodeURIComponent(projectId)}/nodes/${encodeURIComponent(nodeKey)}/retry`,
    { method: "POST" },
  );
  const data = await res.json();
  if (!res.ok) throw new Error(data?.error || `Retry node failed: HTTP ${res.status}`);
  return data;
}

export function subscribeDramaRunEvents(
  projectId: string,
  runId: string,
  handlers: {
    onEvent: (ev: DramaRunEvent) => void;
    onError?: (err: Error) => void;
    onDone?: () => void;
  },
): () => void {
  const controller = new AbortController();
  const url = `/admin/api/v1/drama/projects/${encodeURIComponent(projectId)}/runs/${encodeURIComponent(runId)}/events`;

  (async () => {
    try {
      const res = await adminFetch(url, { signal: controller.signal });
      if (!res.ok || !res.body) {
        throw new Error(`SSE stream failed: HTTP ${res.status}`);
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buf = "";
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        const blocks = buf.split("\n\n");
        buf = blocks.pop() || "";
        for (const block of blocks) {
          const lines = block.split("\n");
          for (const line of lines) {
            if (line.startsWith("data:")) {
              const raw = line.slice(5).trim();
              if (!raw) continue;
              try {
                const ev = JSON.parse(raw) as DramaRunEvent;
                handlers.onEvent(ev);
              } catch {
                // ignore malformed chunk
              }
            }
          }
        }
      }
      handlers.onDone?.();
    } catch (err) {
      if ((err as Error).name !== "AbortError") {
        handlers.onError?.(err instanceof Error ? err : new Error(String(err)));
      }
    }
  })();

  return () => controller.abort();
}

export async function transcribeAudio(
  file: Blob,
  options?: { filename?: string; language?: string; prompt?: string },
): Promise<{ text: string }> {
  const form = new FormData();
  form.append("file", file, options?.filename || "audio.mp3");
  form.append("model", "whisper-1");
  if (options?.language) form.append("language", options.language);
  if (options?.prompt) form.append("prompt", options.prompt);
  const res = await adminFetch("/admin/api/v1/audio/transcriptions", {
    method: "POST",
    body: form,
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Transcribe audio failed: HTTP ${res.status}`);
  }
  return data;
}

// ─── 网页媒体爬虫与跨工坊系统资产库复用 API ─────────────────────────────

export type CrawlerMediaKind = "auto" | "voice" | "image" | "video";

export interface CrawledMediaCandidate {
  id: string;
  media_kind: "voice" | "image" | "video";
  title: string;
  media_url: string;
  source_page?: string;
  source_domain?: string;
  mime_type?: string;
  size_bytes?: number;
  transcript?: string;
  gender?: string;
  locale?: string;
  suggested_role?: string;
  tags?: string[];
}

export interface UnifiedMediaCrawlResponse {
  query: string;
  target_url?: string;
  media_kind: CrawlerMediaKind;
  existing_voices?: VoiceDescriptor[];
  existing_images?: ServerGalleryWork[];
  existing_videos?: VideoTask[];
  candidates: CrawledMediaCandidate[];
  warnings?: string[];
}

export interface UnifiedMediaImportResponse {
  media_kind: "voice" | "image" | "video";
  id: string;
  local_url: string;
  status: string;
  target_view?: string;
  voice?: VoiceDescriptor;
  custom_voice?: {
    id: string;
    name: string;
    role?: string;
    clone_status: string;
    clone_engine?: string;
    cloned_voice_id?: string;
    error?: string;
  };
  work?: ServerGalleryWork;
  task?: VideoTask;
}

export interface SystemAssetsResponse {
  query?: string;
  kind?: string;
  custom_voices?: VoiceDescriptor[];
  short_drama_voices?: VoiceDescriptor[];
  images?: ServerGalleryWork[];
  videos?: VideoTask[];
}

export async function crawlMedia(params: {
  query?: string;
  url?: string;
  media_kind?: CrawlerMediaKind;
  limit?: number;
}): Promise<UnifiedMediaCrawlResponse> {
  const res = await adminFetch("/admin/api/v1/media/crawl", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      query: params.query || "",
      url: params.url || "",
      media_kind: params.media_kind || "auto",
      limit: params.limit ?? 12,
    }),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Media crawl failed: HTTP ${res.status}`);
  }
  return data;
}

export async function importCrawledMedia(params: {
  media_kind: "voice" | "image" | "video";
  url: string;
  source_page?: string;
  title?: string;
  role?: string;
  transcript?: string;
  gender?: string;
  locale?: string;
  tags?: string[];
  clone_engine?: "auto" | "siliconflow" | "dashscope" | "cache_only";
}): Promise<UnifiedMediaImportResponse> {
  const res = await adminFetch("/admin/api/v1/media/crawl-import", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(params),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Import crawled media failed: HTTP ${res.status}`);
  }
  return data;
}

export async function fetchSystemMediaAssets(
  kind: CrawlerMediaKind = "auto",
  query = "",
): Promise<SystemAssetsResponse> {
  const sp = new URLSearchParams({ kind });
  if (query.trim()) sp.set("q", query.trim());
  const res = await adminFetch(`/admin/api/v1/media/assets?${sp.toString()}`);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Fetch system assets failed: HTTP ${res.status}`);
  }
  return data;
}

export async function cloneCustomVoice(
  id: string,
  params?: { clone_engine?: "auto" | "siliconflow" | "dashscope"; transcript?: string },
): Promise<{ status: string; voice: VoiceDescriptor; custom_voice?: { error?: string } }> {
  const res = await adminFetch(`/admin/api/v1/audio/voices/custom/${encodeURIComponent(id)}/clone`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(params || {}),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Clone custom voice failed: HTTP ${res.status}`);
  }
  return data;
}

export async function deleteCustomVoice(id: string): Promise<void> {
  const res = await adminFetch(`/admin/api/v1/audio/voices/custom/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Delete custom voice failed: HTTP ${res.status}`);
  }
}



// ─── 网关路由分组与 API Key 类型与接口 ────────────────────────

export interface GatewayGroup {
  id: string;
  name: string;
  provider: string;
  account_types: string[];
  account_ids: string[];
  model_allowlist: string[];
  model_aliases: Record<string, string>;
  fallback_group_id?: string;
  enabled: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface GatewayKey {
  id: string;
  name: string;
  prefix: string;
  group_id: string;
  rpm: number;
  max_concurrent: number;
  enabled: boolean;
  total_requests: number;
  failed_requests: number;
  last_used_at?: string;
  created_at?: string;
  updated_at?: string;
}

export async function fetchGatewayGroups(): Promise<{ groups: GatewayGroup[] }> {
  const res = await adminFetch("/admin/api/v1/gateway/groups");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `获取网关分组失败: HTTP ${res.status}`);
  }
  return data;
}

export async function createGatewayGroup(
  group: Partial<GatewayGroup>
): Promise<GatewayGroup> {
  const res = await adminFetch("/admin/api/v1/gateway/groups", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(group),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `创建网关分组失败: HTTP ${res.status}`);
  }
  return data;
}

export async function updateGatewayGroup(
  id: string,
  group: Partial<GatewayGroup>
): Promise<GatewayGroup> {
  const res = await adminFetch(`/admin/api/v1/gateway/groups/${encodeURIComponent(id)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(group),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `更新网关分组失败: HTTP ${res.status}`);
  }
  return data;
}

export async function deleteGatewayGroup(id: string): Promise<void> {
  const res = await adminFetch(`/admin/api/v1/gateway/groups/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `删除网关分组失败: HTTP ${res.status}`);
  }
}

export async function fetchGatewayKeys(): Promise<{ keys: GatewayKey[] }> {
  const res = await adminFetch("/admin/api/v1/gateway/keys");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `获取网关 Key 列表失败: HTTP ${res.status}`);
  }
  return data;
}

export async function createGatewayKey(params: {
  name: string;
  group_id: string;
  rpm?: number;
  max_concurrent?: number;
}): Promise<{ key: string; record: GatewayKey }> {
  const res = await adminFetch("/admin/api/v1/gateway/keys", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(params),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `创建网关 Key 失败: HTTP ${res.status}`);
  }
  return data;
}

export async function updateGatewayKey(
  id: string,
  params: {
    name?: string;
    group_id?: string;
    rpm?: number;
    max_concurrent?: number;
    enabled?: boolean;
  }
): Promise<GatewayKey> {
  const res = await adminFetch(`/admin/api/v1/gateway/keys/${encodeURIComponent(id)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(params),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `更新网关 Key 失败: HTTP ${res.status}`);
  }
  return data;
}

export async function deleteGatewayKey(id: string): Promise<void> {
  const res = await adminFetch(`/admin/api/v1/gateway/keys/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `删除网关 Key 失败: HTTP ${res.status}`);
  }
}

export async function revealGatewayKeySecret(id: string): Promise<string> {
  const res = await adminFetch(`/admin/api/v1/gateway/keys/${encodeURIComponent(id)}/secret`);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `获取 Key 明文失败: HTTP ${res.status}`);
  }
  return data?.key || "";
}

export interface DealItem {
  id: string;
  title: string;
  summary: string;
  url: string;
  source: string;
  category: string;
  author?: string;
  likes: number;
  replies: number;
  views: number;
  score: number;
  is_hot: boolean;
  tags?: string[];
  published_at: string;
  crawled_at: string;
}

export interface DealsStatus {
  total_count: number;
  hot_count: number;
  last_update_at: string;
  next_update_at: string;
  interval: string;
  is_running: boolean;
  sources: string[];
}

export async function fetchDeals(params?: {
  sort?: string;
  hot_only?: boolean;
  source?: string;
  category?: string;
  keyword?: string;
  limit?: number;
  offset?: number;
}): Promise<{ items: DealItem[]; total: number }> {
  const query = new URLSearchParams();
  if (params?.sort) query.set("sort", params.sort);
  if (params?.hot_only) query.set("hot_only", "true");
  if (params?.source) query.set("source", params.source);
  if (params?.category) query.set("category", params.category);
  if (params?.keyword) query.set("keyword", params.keyword);
  if (params?.limit) query.set("limit", String(params.limit));
  if (params?.offset) query.set("offset", String(params.offset));

  const qs = query.toString();
  const url = `/admin/api/v1/deals${qs ? `?${qs}` : ""}`;
  const res = await adminFetch(url);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `获取羊毛资讯失败: HTTP ${res.status}`);
  }
  return { items: data.items || [], total: data.total || 0 };
}

export async function postDealsChat(params: {
  messages: Array<{ role: "user" | "assistant"; content: string }>;
  web_search: boolean;
}): Promise<{ answer: string }> {
  const res = await adminFetch("/admin/api/v1/deals/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(params),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `线报助手请求失败: HTTP ${res.status}`);
  }
  return { answer: data.answer || "" };
}

export async function refreshDeals(): Promise<{ message: string; updated: number; status: DealsStatus }> {
  const res = await adminFetch("/admin/api/v1/deals/refresh", {
    method: "POST",
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `刷新羊毛资讯失败: HTTP ${res.status}`);
  }
  return data;
}

export async function fetchDealsStatus(): Promise<DealsStatus> {
  const res = await adminFetch("/admin/api/v1/deals/status");
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error || `获取羊毛调度状态失败: HTTP ${res.status}`);
  }
  return data;
}

// ─── 系统配置中心 (System Config Center, ADR-0019) ───────────────────────

export interface SystemSettingItem {
  key: string;
  label: string;
  description: string;
  group_title: string;
  placeholder?: string;
  category: "multimodal" | "proxy" | "pool" | "gateway" | "storage" | "bootstrap" | string;
  type: "string" | "int" | "float" | "bool" | "duration" | string;
  default_value: string;
  is_secret: boolean;
  is_featured: boolean;
  profile_overridable: boolean;
  read_only: boolean;
  effective_value: string;
  scope_value: string;
  global_value: string;
  source: "profile" | "global" | "env" | "default" | string;
  has_scope_override: boolean;
  has_global_value: boolean;
  is_configured: boolean;
  updated_at?: string;
  updated_by?: string;
}

export interface SystemSettingsResponse {
  scope: string;
  active_profile: string;
  available_scopes: string[];
  items: SystemSettingItem[];
  bootstrap_env_template: string;
}

export async function fetchSystemSettings(scope = "global"): Promise<SystemSettingsResponse> {
  const params = new URLSearchParams();
  if (scope) params.set("scope", scope);
  const res = await adminFetch(`/admin/api/v1/settings?${params.toString()}`);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `加载系统配置失败: HTTP ${res.status}`);
  }
  return data;
}

export async function updateSystemSettings(payload: {
  scope: string;
  values?: Record<string, string>;
  dotenv?: string;
}): Promise<SystemSettingsResponse> {
  const res = await adminFetch("/admin/api/v1/settings", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `保存系统配置失败: HTTP ${res.status}`);
  }
  return data;
}

export async function deleteSystemSettingOverride(
  scope: string,
  key: string
): Promise<SystemSettingsResponse> {
  const params = new URLSearchParams({ scope, key });
  const res = await adminFetch(`/admin/api/v1/settings?${params.toString()}`, {
    method: "DELETE",
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `恢复默认配置失败: HTTP ${res.status}`);
  }
  return data;
}

export async function revealSystemSetting(
  scope: string,
  key: string
): Promise<{ key: string; scope: string; value: string; found: boolean }> {
  const res = await adminFetch("/admin/api/v1/settings/reveal", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ scope, key }),
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `读取明文失败: HTTP ${res.status}`);
  }
  return data;
}

export async function exportSystemSettings(params: {
  scope: string;
  format: "dotenv" | "json";
  includeSecrets?: boolean;
}): Promise<{ scope: string; format: string; content: string }> {
  const qs = new URLSearchParams({
    scope: params.scope,
    format: params.format,
    include_secrets: params.includeSecrets ? "true" : "false",
  });
  const res = await adminFetch(`/admin/api/v1/settings/export?${qs.toString()}`);
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `导出配置失败: HTTP ${res.status}`);
  }
  return data;
}
