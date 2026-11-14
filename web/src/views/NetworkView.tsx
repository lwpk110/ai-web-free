import React, { useEffect, useMemo, useState } from "react";
import {
  Check,
  Copy,
  Globe,
  RefreshCw,
  Save,
  Zap,
} from "lucide-react";
import {
  fetchProxies,
  probeProxies,
  updateProxies,
  type ProxyNodeStatus,
  type ProxySummary,
} from "../api/client";
import {
  formatClockTime,
  formatRemainingCountdown,
  NoticeBanner,
  StatusBadge,
} from "../components/ui";
import {
  classifyModel,
  useModelsCatalog,
  type ModelCategory,
} from "../lib/models";

export const NetworkView: React.FC<{
  autoProbeTrigger?: number;
}> = ({ autoProbeTrigger = 0 }) => {
  const [summary, setSummary] = useState<ProxySummary>({
    total: 0,
    active: 0,
    cooling: 0,
  });
  const [nodes, setNodes] = useState<ProxyNodeStatus[]>([]);
  const [proxyText, setProxyText] = useState("");
  const [probing, setProbing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState<{
    type: "info" | "success" | "error";
    text: string;
  } | null>(null);

  const {
    catalog,
    chatModels,
    imageModels,
    source: modelsSource,
    cachedAt: modelsCachedAt,
    warning: modelsWarning,
    loading: refreshingModels,
    error: modelsError,
    refresh: refreshCatalog,
  } = useModelsCatalog();
  const [modelFilter, setModelFilter] = useState<"all" | ModelCategory>("all");
  const [copiedModelId, setCopiedModelId] = useState<string | null>(null);
  const [selectedSdkModel, setSelectedSdkModel] = useState<string>("");

  const [apiKey, setApiKey] = useState("");
  const [sdkTab, setSdkTab] = useState<"curl" | "python" | "ts">("curl");
  const [copiedCode, setCopiedCode] = useState(false);
  const [nowMs, setNowMs] = useState(() => Date.now());

  const loadProxies = async () => {
    try {
      const res = await fetchProxies();
      setSummary(res.summary || { total: 0, active: 0, cooling: 0 });
      setNodes(res.nodes || []);
      setProxyText((res.nodes || []).map((n) => n.url).join("\n"));
    } catch (err: any) {
      setNotice({
        type: "error",
        text: err?.message || "加载代理列表失败",
      });
    }
  };

  const handleRefreshModels = async () => {
    const nextState = await refreshCatalog({
      forceRefresh: true,
      apiKey: apiKey.trim() || undefined,
    });
    if (nextState.error) {
      setNotice({
        type: "error",
        text: `实时拉取模型目录失败（已保留当前参考目录）：${nextState.error}`,
      });
      return;
    }
    if (nextState.source === "fallback") {
      setNotice({
        type: "info",
        text: nextState.warning || "当前无可用登录态账号，已使用内置参考目录",
      });
      return;
    }
    const upstreamN = nextState.catalog.filter(
      (m) => m.source === "upstream"
    ).length;
    const builtinN = nextState.catalog.filter(
      (m) => m.source === "builtin"
    ).length;
    setNotice({
      type: "success",
      text: `已实时拉取并缓存 ${nextState.catalog.length} 个模型（上游 ${upstreamN} + 内置生图/语音 ${builtinN}）`,
    });
  };

  useEffect(() => {
    loadProxies();
    const timer = window.setInterval(() => setNowMs(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const handleProbeAll = async () => {
    setProbing(true);
    setNotice({
      type: "info",
      text: "正在并发探测所有代理节点连通性与延迟...",
    });
    try {
      const res = await probeProxies();
      setSummary(res.summary || { total: 0, active: 0, cooling: 0 });
      setNodes(res.nodes || []);
      setNotice({
        type: (res.summary?.cooling ?? 0) > 0 ? "info" : "success",
        text: `探测完成：总计 ${res.summary?.total ?? 0} 个，可用 ${res.summary?.active ?? 0} 个，熔断冷却 ${res.summary?.cooling ?? 0} 个`,
      });
    } catch (err: any) {
      setNotice({
        type: "error",
        text: `代理探测失败: ${err?.message || "未知错误"}`,
      });
    } finally {
      setProbing(false);
    }
  };

  useEffect(() => {
    if (autoProbeTrigger > 0) {
      handleProbeAll();
    }
  }, [autoProbeTrigger]);

  const handleSaveProxies = async () => {
    setSaving(true);
    setNotice(null);
    try {
      const list = proxyText
        .split("\n")
        .map((s) => s.trim())
        .filter(Boolean);
      const res = await updateProxies(list, true);
      setNotice({
        type: "success",
        text: `已保存并重载 ${res.proxies_count} 条代理（自动校准迁移 ${res.migrated} 个绑定账号）`,
      });
      await loadProxies();
      window.dispatchEvent(new Event("pool-updated"));
    } catch (err: any) {
      setNotice({
        type: "error",
        text: `保存代理失败: ${err?.message || "未知错误"}`,
      });
    } finally {
      setSaving(false);
    }
  };

  const counts = useMemo(
    () => ({
      all: catalog.length,
      chat: catalog.filter((m) => m.category === "chat").length,
      image: catalog.filter((m) => m.category === "image").length,
      audio: catalog.filter((m) => m.category === "audio").length,
      upstream: catalog.filter((m) => m.source === "upstream").length,
      builtin: catalog.filter((m) => m.source === "builtin").length,
    }),
    [catalog]
  );

  const visibleModels = useMemo(() => {
    if (modelFilter === "all") return catalog;
    return catalog.filter((m) => m.category === modelFilter);
  }, [catalog, modelFilter]);

  const handleCopyModel = async (id: string) => {
    setSelectedSdkModel(id);
    try {
      await navigator.clipboard.writeText(id);
      setCopiedModelId(id);
      window.setTimeout(() => setCopiedModelId(null), 1400);
    } catch {
      // ignore
    }
  };

  const activeSdkModel = useMemo(() => {
    if (selectedSdkModel && visibleModels.some((m) => m.id === selectedSdkModel)) {
      return selectedSdkModel;
    }
    if (modelFilter === "chat") {
      return chatModels[0]?.id || "gpt-5-6";
    }
    if (modelFilter === "image") {
      return (
        imageModels.find((m) => m.id === "gpt-image-2.5-sunburst")?.id ||
        imageModels[0]?.id ||
        "gpt-image-2.5-sunburst"
      );
    }
    if (modelFilter === "audio") {
      return visibleModels[0]?.id || "gpt-4o-mini-tts";
    }
    return (
      imageModels.find((m) => m.id === "gpt-image-2.5-sunburst")?.id ||
      visibleModels[0]?.id ||
      chatModels[0]?.id ||
      "gpt-5-6"
    );
  }, [selectedSdkModel, visibleModels, modelFilter, chatModels, imageModels]);

  const baseURL = `${window.location.origin}/v1`;
  const sdkCode = useMemo(() => {
    const keyVal = apiKey.trim() || "free";
    const meta = classifyModel(activeSdkModel);
    if (meta.category === "chat") {
      if (sdkTab === "curl") {
        return `curl -X POST ${baseURL}/chat/completions \\\n  -H "Content-Type: application/json" \\\n  -H "Authorization: Bearer ${keyVal}" \\\n  -d '{"model":"${activeSdkModel}","messages":[{"role":"user","content":"Hello!"}]}'`;
      }
      if (sdkTab === "python") {
        return `from openai import OpenAI\n\nclient = OpenAI(base_url="${baseURL}", api_key="${keyVal}")\nresp = client.chat.completions.create(\n    model="${activeSdkModel}",\n    messages=[{"role": "user", "content": "Hello!"}]\n)\nprint(resp.choices[0].message.content)`;
      }
      return `import OpenAI from "openai";\n\nconst client = new OpenAI({\n  baseURL: "${baseURL}",\n  apiKey: "${keyVal}",\n});\nconst resp = await client.chat.completions.create({\n  model: "${activeSdkModel}",\n  messages: [{ role: "user", content: "Hello!" }],\n});`;
    }
    if (meta.category === "audio") {
      if (sdkTab === "curl") {
        return `curl -X POST ${baseURL}/audio/speech \\\n  -H "Content-Type: application/json" \\\n  -H "Authorization: Bearer ${keyVal}" \\\n  -d '{"model":"${activeSdkModel}","input":"Hello from AI-Web-Free","voice":"alloy"}' --output speech.mp3`;
      }
      if (sdkTab === "python") {
        return `from openai import OpenAI\n\nclient = OpenAI(base_url="${baseURL}", api_key="${keyVal}")\nresp = client.audio.speech.create(\n    model="${activeSdkModel}",\n    voice="alloy",\n    input="Hello from AI-Web-Free"\n)\nresp.stream_to_file("speech.mp3")`;
      }
      return `import OpenAI from "openai";\n\nconst client = new OpenAI({\n  baseURL: "${baseURL}",\n  apiKey: "${keyVal}",\n});\nconst resp = await client.audio.speech.create({\n  model: "${activeSdkModel}",\n  voice: "alloy",\n  input: "Hello from AI-Web-Free",\n});`;
    }
    if (sdkTab === "curl") {
      return `curl -X POST ${baseURL}/images/generations \\\n  -H "Content-Type: application/json" \\\n  -H "Authorization: Bearer ${keyVal}" \\\n  -d '{"model":"${activeSdkModel}","prompt":"Minimalist glass pavilion at sunrise","size":"1024x1024"}'`;
    }
    if (sdkTab === "python") {
      return `from openai import OpenAI\n\nclient = OpenAI(base_url="${baseURL}", api_key="${keyVal}")\nimg = client.images.generate(\n    model="${activeSdkModel}",\n    prompt="Minimalist glass pavilion at sunrise",\n    size="1024x1024"\n)\nprint(img.data[0].url)`;
    }
    return `import OpenAI from "openai";\n\nconst client = new OpenAI({\n  baseURL: "${baseURL}",\n  apiKey: "${keyVal}",\n});\nconst img = await client.images.generate({\n  model: "${activeSdkModel}",\n  prompt: "Minimalist glass pavilion at sunrise",\n});`;
  }, [apiKey, baseURL, sdkTab, activeSdkModel]);

  const handleCopyCode = async () => {
    try {
      await navigator.clipboard.writeText(sdkCode);
      setCopiedCode(true);
      window.setTimeout(() => setCopiedCode(false), 1600);
    } catch {
      // ignore
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">
            代理健康矩阵与模型目录
          </h1>
          <p className="text-xs z-text-muted mt-1">
            粘性出站代理探测与熔断校准；右侧通过{" "}
            <code className="font-mono-num">/v1/models?include_builtins=true</code>{" "}
            真实聚合上游对话、生图与语音全部可用模型。
          </p>
        </div>
        <button
          type="button"
          onClick={handleProbeAll}
          disabled={probing}
          className="inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl z-primary-btn text-xs font-medium cursor-pointer"
        >
          <Zap size={13} />
          <span>{probing ? "探测测活中..." : "并发探活全部代理"}</span>
        </button>
      </div>

      {notice && (
        <NoticeBanner
          type={notice.type}
          text={notice.text}
          onClose={() => setNotice(null)}
        />
      )}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Left: Outbound Proxy Pool */}
        <div className="p-card rounded-2xl p-4 space-y-4 text-xs flex flex-col">
          <div className="flex items-center justify-between gap-2">
            <h2 className="text-sm font-semibold flex items-center gap-1.5">
              <Globe size={14} />
              <span>
                出站代理池 ({summary.active}/{summary.total} 健康
                {summary.cooling > 0 ? ` · ${summary.cooling} 冷却` : ""})
              </span>
            </h2>
            <span className="z-text-muted">连续错误 ≥ 3 次自动熔断 5m</span>
          </div>

          <div className="space-y-2">
            {nodes.length === 0 && (
              <div
                className="p-4 rounded-xl text-center z-text-muted"
                style={{
                  background: "var(--bg-input)",
                  border: "1px solid var(--hairline)",
                }}
              >
                当前未配置代理，系统将按直连方式请求上游（可在下方添加代理列表并保存）。
              </div>
            )}
            {nodes.map((node) => {
              const coolStr = formatRemainingCountdown(
                node.cooldown_until,
                nowMs
              );
              const healthy = node.status !== "cooling";
              return (
                <div
                  key={node.url}
                  className="p-3 rounded-xl space-y-1.5"
                  style={{
                    background: "var(--bg-input)",
                    border: "1px solid var(--hairline)",
                  }}
                >
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2 min-w-0">
                      <span
                        className={
                          healthy ? "status-dot-green" : "status-dot-amber"
                        }
                      />
                      <span className="font-mono-num font-medium truncate">
                        {node.url}
                      </span>
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      {node.latency_ms > 0 && (
                        <span className="px-2 py-0.5 rounded-full border z-pill-success font-mono-num">
                          {node.latency_ms}ms
                        </span>
                      )}
                      <StatusBadge status={node.status} />
                      {coolStr && (
                        <span className="font-mono-num z-text-muted">
                          {coolStr}
                        </span>
                      )}
                    </div>
                  </div>
                  <div className="flex items-center justify-between z-text-muted font-mono-num">
                    <span>粘性绑定：{node.bound_accounts} 个账号</span>
                    <span>
                      连续失败：{node.consecutive_errors} · 累计：{node.total_failures}
                    </span>
                  </div>
                  {node.last_error && (
                    <div
                      className="truncate"
                      style={{ color: "var(--pill-danger-text)" }}
                      title={node.last_error}
                    >
                      {node.last_error}
                    </div>
                  )}
                </div>
              );
            })}
          </div>

          <div className="space-y-2 mt-auto pt-2 border-t z-border">
            <label className="font-medium z-text-sec block">
              编辑代理列表（每行一条 http:// 或 socks5://，保存自动持久化
              proxies.txt 并校准绑定）
            </label>
            <textarea
              rows={4}
              value={proxyText}
              onChange={(e) => setProxyText(e.target.value)}
              placeholder={"http://127.0.0.1:7890\nsocks5://127.0.0.1:10808"}
              className="w-full p-3 rounded-xl p-code-box font-mono-num resize-y focus:outline-none"
            />
            <div className="flex justify-end">
              <button
                type="button"
                onClick={handleSaveProxies}
                disabled={saving}
                className="inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
              >
                <Save size={13} />
                <span>{saving ? "保存校准中..." : "保存并校准绑定"}</span>
              </button>
            </div>
          </div>
        </div>

        {/* Right: Categorized Real Model Directory & Clean SDK Snippet */}
        <div className="p-card rounded-2xl p-4 space-y-3.5 text-xs flex flex-col">
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div>
              <h2 className="text-sm font-semibold">
                /v1/models 完整模型能力目录
              </h2>
              <div className="z-text-muted mt-0.5">
                {modelsSource === "live" || modelsSource === "cache" ? (
                  <span style={{ color: "var(--pill-success-text)" }}>
                    {modelsSource === "cache"
                      ? "已缓存复用 /v1/models 目录"
                      : "实时拉取自 /v1/models 目录"}
                    （上游 {counts.upstream} + 内置生图/语音 {counts.builtin}
                    {modelsCachedAt
                      ? ` · ${formatClockTime(modelsCachedAt)}`
                      : ""}
                    ）
                  </span>
                ) : (
                  <span>
                    内置参考目录
                    {modelsError || modelsWarning
                      ? ` · ${modelsError || modelsWarning}`
                      : ""}
                  </span>
                )}
              </div>
            </div>

            <button
              type="button"
              disabled={refreshingModels}
              onClick={handleRefreshModels}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer shrink-0"
            >
              <RefreshCw
                size={12}
                className={refreshingModels ? "animate-spin" : ""}
              />
              <span>{refreshingModels ? "拉取中..." : "刷新实时目录"}</span>
            </button>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <input
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder="可选网关 Bearer Authorization Key（留空直连）"
              className="flex-1 min-w-[200px] px-3 py-1.5 rounded-xl p-input font-mono-num"
            />
          </div>

          {/* Model Category Tabs */}
          <div className="flex flex-wrap gap-1.5">
            {(
              [
                { k: "all", label: `全部模型 (${counts.all})` },
                { k: "chat", label: `对话与推理 (${counts.chat})` },
                { k: "image", label: `图像创作 · DALL-E (${counts.image})` },
                { k: "audio", label: `语音 TTS / Whisper (${counts.audio})` },
              ] as const
            ).map((tab) => (
              <button
                key={tab.k}
                type="button"
                onClick={() => setModelFilter(tab.k)}
                className={`px-2.5 py-1.5 rounded-xl border cursor-pointer transition-colors ${
                  modelFilter === tab.k
                    ? "z-primary-btn border-transparent font-medium"
                    : "z-border z-surface z-ghost-btn z-text-sec"
                }`}
              >
                {tab.label}
              </button>
            ))}
          </div>

          {/* Clickable 2-Col Model Cards */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 max-h-68 overflow-y-auto pr-1">
            {visibleModels.map((m) => {
              const badgeCls =
                m.source === "upstream"
                  ? "z-pill-success"
                  : m.category === "image"
                    ? "z-pill-warn"
                    : "z-pill-info";
              const isCopied = copiedModelId === m.id;
              return (
                <button
                  key={m.id}
                  type="button"
                  onClick={() => handleCopyModel(m.id)}
                  className="p-2.5 rounded-xl flex items-center justify-between gap-2 text-left cursor-pointer z-row-hover"
                  style={{
                    background: "var(--bg-input)",
                    border: "1px solid var(--hairline)",
                  }}
                  title="点击复制模型 ID"
                >
                  <div className="min-w-0">
                    <div className="font-mono-num font-semibold truncate">
                      {m.id}
                    </div>
                    <div className="z-text-muted truncate mt-0.5">
                      {isCopied ? "已复制到剪贴板" : m.subtitle}
                    </div>
                  </div>
                  <span
                    className={`px-2 py-0.5 rounded-md border font-mono-num shrink-0 ${badgeCls}`}
                  >
                    {isCopied
                      ? "Copied"
                      : m.source === "upstream"
                        ? "上游实时"
                        : "Built-in"}
                  </span>
                </button>
              );
            })}
          </div>

          {/* SDK Snippet Card with Dedicated Copy Header */}
          <div className="rounded-xl overflow-hidden p-code-box mt-auto">
            <div className="px-3 py-2 border-b z-border flex items-center justify-between gap-2">
              <div className="inline-flex gap-1">
                {(
                  [
                    ["curl", "cURL"],
                    ["python", "Python"],
                    ["ts", "TypeScript"],
                  ] as const
                ).map(([k, lbl]) => (
                  <button
                    key={k}
                    type="button"
                    onClick={() => setSdkTab(k)}
                    className={`px-2.5 py-1 rounded-lg cursor-pointer transition-colors ${
                      sdkTab === k
                        ? "z-surface border z-border font-medium"
                        : "z-text-muted z-ghost-btn"
                    }`}
                  >
                    {lbl}
                  </button>
                ))}
              </div>
              <button
                type="button"
                onClick={handleCopyCode}
                className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg border z-border z-surface z-ghost-btn cursor-pointer"
              >
                {copiedCode ? <Check size={12} /> : <Copy size={12} />}
                <span>{copiedCode ? "已复制" : "复制代码"}</span>
              </button>
            </div>
            <pre className="p-3 font-mono-num text-xs leading-relaxed overflow-x-auto m-0">
              {sdkCode}
            </pre>
          </div>
        </div>
      </div>
    </div>
  );
};
