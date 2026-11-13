export interface ProviderDescriptor {
  id: string;
  name: string;
  icon: string;
  description: string;
  enabled: boolean;
  supported_account_types: string[];
  credential_formats: string[];
  capabilities: string[];
}

export interface PoolStats {
  provider: string;
  total: number;
  active: number;
  rate_limited: number;
  expired: number;
  disabled: number;
  banned: number;
  by_type: Record<string, number>;
  proxy_count: number;
  updated_at: string;
}

export interface AccountView {
  id: string;
  email?: string;
  chatgpt_account_id?: string;
  type: string;
  status: string;
  proxy?: string;
  expires_at?: string;
  remaining_days?: number;
  has_refresh?: boolean;
  has_refresh_token?: boolean;
  has_session_token?: boolean;
  has_kami_credentials?: boolean;
  token_masked?: string;
  token_preview?: string;
  last_used_at?: string;
  fail_count?: number;
  failed_calls?: number;
  last_error?: string;
}

export async function fetchOverview(): Promise<{
  default_provider: string;
  providers: ProviderDescriptor[];
  stats: Record<string, PoolStats>;
  proxy_count: number;
  proxies: string[];
}> {
  const res = await fetch("/admin/api/v1/overview");
  if (!res.ok) throw new Error(`Overview failed: ${res.status}`);
  return res.json();
}

export async function fetchAccounts(
  provider = "chatgpt",
  status = "",
  type = "",
  search = ""
): Promise<{ accounts: AccountView[]; stats: PoolStats }> {
  const params = new URLSearchParams();
  if (status) params.set("status", status);
  if (type) params.set("type", type);
  if (search) params.set("q", search);
  const res = await fetch(
    `/admin/api/v1/providers/${provider}/accounts?${params.toString()}`
  );
  if (!res.ok) throw new Error(`Fetch accounts failed: ${res.status}`);
  return res.json();
}

export async function executeAccountAction(
  provider: string,
  accountId: string,
  action: "probe" | "refresh" | "enable" | "disable" | "delete" | "totp" | "login"
): Promise<{
  ok?: boolean;
  message?: string;
  error?: { message?: string };
  result?: { message?: string; totp_code?: string; remaining_seconds?: number };
  account?: AccountView;
}> {
  const res = await fetch(
    `/admin/api/v1/providers/${provider}/accounts/${encodeURIComponent(accountId)}/action`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action }),
    }
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Action failed: ${res.status}`);
  }
  return data;
}

export async function probeAllAccounts(
  provider = "chatgpt"
): Promise<{ probed?: number; passed?: number; failed?: number; total?: number; active?: number }> {
  const res = await fetch(
    `/admin/api/v1/providers/${provider}/pool/probe-all`,
    { method: "POST" }
  );
  return res.json();
}

export async function loginKamiAccounts(
  provider = "chatgpt",
  headless = true,
  force = false
): Promise<{
  result?: { total: number; success: number; failed: number; message?: string };
  error?: { message?: string };
}> {
  const res = await fetch(
    `/admin/api/v1/providers/${provider}/accounts/kami-login`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ headless, force }),
    }
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Kami login failed: ${res.status}`);
  }
  return data;
}

export async function importAccounts(
  provider: string,
  format: string,
  rawText: string,
  persist = true,
  autoLogin = false
): Promise<{
  result?: {
    added: number;
    skipped: number;
    failed: number;
    total_pool: number;
    persisted: boolean;
    messages?: string[];
  };
  error?: { message?: string };
}> {
  const res = await fetch(
    `/admin/api/v1/providers/${provider}/accounts/import`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        credential_type: format,
        raw_text: rawText,
        persist,
        auto_login: autoLogin,
        headless: true,
      }),
    }
  );
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data?.error?.message || `Import failed: ${res.status}`);
  }
  return data;
}

export async function updateProxies(
  proxies: string[]
): Promise<{ ok: boolean; count: number }> {
  const res = await fetch("/admin/api/v1/proxies", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ proxies }),
  });
  return res.json();
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
