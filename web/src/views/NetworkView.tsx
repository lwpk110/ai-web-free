import React, { useEffect, useMemo, useState } from "react";
import {
  Check,
  ChevronDown,
  ChevronUp,
  Copy,
  FileCode2,
  Globe,
  Layers,
  Plus,
  RefreshCw,
  Save,
  Server,
  Sparkles,
  Zap,
} from "lucide-react";
import {
  fetchProxies,
  probeProxies,
  switchProxyProfile,
  syncClashProxies,
  updateProxies,
  type ClashConfigSummary,
  type ClashSyncResult,
  type ProxyNodeStatus,
  type ProxyProfileSummary,
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

const PROFILE_PRESET_EXAMPLES: Record<
  string,
  { title: string; subtitle: string; example: string }
> = {
  home: {
    title: "家里开发环境 (home)",
    subtitle: "Windows 本地 Clash Verge 默认端口 127.0.0.1:10809",
    example: "http://127.0.0.1:10809",
  },
  office: {
    title: "公司办公环境 (office)",
    subtitle: "局域网共享代理 192.168.2.12:10809",
    example: "http://192.168.2.12:10809",
  },
  vps: {
    title: "生产 VPS 多端口池 (vps)",
    subtitle: "Mihomo listeners 多端口映射 127.0.0.1:20001..20040",
    example: "http://127.0.0.1:20001\nhttp://127.0.0.1:20002\nhttp://127.0.0.1:20003",
  },
};

export const NetworkView: React.FC<{
  autoProbeTrigger?: number;
}> = ({ autoProbeTrigger = 0 }) => {
  const [summary, setSummary] = useState<ProxySummary>({
    total: 0,
    active: 0,
    cooling: 0,
  });
  const [activeProfile, setActiveProfile] = useState<string>("home");
  const [envProfile, setEnvProfile] = useState<string>("home");
  const [viewProfile, setViewProfile] = useState<string>("home");
  const [profiles, setProfiles] = useState<ProxyProfileSummary[]>([
    { profile: "home", label: "家里开发环境 (home)", total: 0, clash_count: 0 },
    { profile: "office", label: "公司办公环境 (office)", total: 0, clash_count: 0 },
    { profile: "vps", label: "生产 VPS 多端口 (vps)", total: 0, clash_count: 0 },
  ]);
  const [clashConfig, setClashConfig] = useState<ClashConfigSummary | null>(null);
  const [showClashPanel, setShowClashPanel] = useState<boolean>(false);
  const [subUrlInput, setSubUrlInput] = useState<string>("");
  const [rawSubInput, setRawSubInput] = useState<string>("");
  const [clashTargetProfile, setClashTargetProfile] = useState<string>("vps");
  const [clashListenHost, setClashListenHost] = useState<string>("127.0.0.1");
  const [clashStartPort, setClashStartPort] = useState<number>(20001);
  const [clashMaxPorts, setClashMaxPorts] = useState<number>(40);
  const [clashPriorityRegions, setClashPriorityRegions] = useState<string>(
    "US,JP,SG,TW,KR,DE,UK,NL,CA,AU"
  );
  const [clashExcludeRegex, setClashExcludeRegex] = useState<string>(
    "(?i)(剩余|流量|到期|官网|重置|群|套餐|网址|失联|维护|测试|CN|中国|HK|香港|MO|澳门|RU|俄罗斯|BY|白俄罗斯|IR|伊朗|KP|朝鲜)"
  );
  const [clashReloadMihomo, setClashReloadMihomo] = useState<boolean>(true);
  const [clashActivateAfterSync, setClashActivateAfterSync] = useState<boolean>(false);
  const [syncingClash, setSyncingClash] = useState<boolean>(false);
  const [lastClashResult, setLastClashResult] = useState<ClashSyncResult | null>(
    null
  );
  const [showGeneratedYaml, setShowGeneratedYaml] = useState<boolean>(false);
  const [copiedYaml, setCopiedYaml] = useState<boolean>(false);
  const [switchingProfile, setSwitchingProfile] = useState<boolean>(false);
  const [showCreateProfile, setShowCreateProfile] = useState<boolean>(false);
  const [newProfileName, setNewProfileName] = useState<string>("");
  const [newProfileMode, setNewProfileMode] = useState<"manual" | "clash">("manual");
  const [newProfileProxy, setNewProfileProxy] = useState<string>("");
  const [newProfileClashSubUrl, setNewProfileClashSubUrl] = useState<string>("");
  const [newProfileClashStartPort, setNewProfileClashStartPort] = useState<number>(20001);
  const [newProfileClashMaxPorts, setNewProfileClashMaxPorts] = useState<number>(40);
  const [newProfileClashReload, setNewProfileClashReload] = useState<boolean>(true);
  const [creatingProfile, setCreatingProfile] = useState<boolean>(false);

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

  const loadProxies = async (targetViewProfile?: string) => {
    try {
      const res = await fetchProxies(targetViewProfile);
      const actProf = res.active_profile || "home";
      const vProf = res.view_profile || targetViewProfile || actProf;
      setActiveProfile(actProf);
      setEnvProfile(res.env_profile || actProf);
      setViewProfile(vProf);
      if (Array.isArray(res.profiles) && res.profiles.length > 0) {
        setProfiles(res.profiles);
      }
      if (res.clash_config) {
        setClashConfig(res.clash_config);
        setClashTargetProfile(res.clash_config.target_profile || "vps");
        setClashListenHost(res.clash_config.listen_host || "127.0.0.1");
        setClashStartPort(res.clash_config.start_port || 20001);
        setClashMaxPorts(res.clash_config.max_ports || 40);
        if (res.clash_config.priority_regions) {
          setClashPriorityRegions(res.clash_config.priority_regions);
        }
        if (res.clash_config.exclude_regex) {
          setClashExcludeRegex(res.clash_config.exclude_regex);
        }
        if (res.clash_config.last_sync) {
          setLastClashResult(res.clash_config.last_sync);
        }
      }
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

  const handleSelectViewProfile = async (prof: string) => {
    setViewProfile(prof);
    await loadProxies(prof);
  };

  const handleActivateProfile = async (prof: string) => {
    setSwitchingProfile(true);
    setNotice(null);
    try {
      const res = await switchProxyProfile(prof);
      setActiveProfile(res.active_profile);
      setViewProfile(res.active_profile);
      setSummary(res.summary || { total: 0, active: 0, cooling: 0 });
      setNodes(res.nodes || []);
      setProxyText((res.nodes || []).map((n) => n.url).join("\n"));
      if (Array.isArray(res.profiles)) {
        setProfiles(res.profiles);
      }
      setNotice({
        type: "success",
        text: `已热切换当前运行环境至 Profile [${res.active_profile}]，自动按该环境专属粘性映射重绑 ${res.migrated} 个号池账号`,
      });
      window.dispatchEvent(new Event("pool-updated"));
    } catch (err: any) {
      setNotice({
        type: "error",
        text: `切换运行 Profile 失败: ${err?.message || "未知错误"}`,
      });
    } finally {
      setSwitchingProfile(false);
    }
  };

  const handleFillPresetExample = (prof: string) => {
    const preset = PROFILE_PRESET_EXAMPLES[prof];
    if (!preset) return;
    setProxyText(preset.example);
    setNotice({
      type: "info",
      text: `已填入 [${prof}] 环境常用代理示例，点击「保存到 [${prof}] Profile」即可持久化至数据库`,
    });
  };

  const handleCreateCustomProfile = async () => {
    const cleaned = newProfileName.trim().toLowerCase().replace(/[^a-z0-9_-]/g, "");
    if (!cleaned) {
      setNotice({
        type: "error",
        text: "请输入有效的 Profile 名称（仅支持小写字母、数字、下划线或短横线，例如 laptop、hk-vps）",
      });
      return;
    }

    setCreatingProfile(true);
    setNotice(null);
    try {
      if (newProfileMode === "clash") {
        const subUrl = newProfileClashSubUrl.trim();
        if (!subUrl) {
          setNotice({
            type: "error",
            text: "已选择「Clash 机场订阅转多端口」模式，请填写机场订阅链接",
          });
          setCreatingProfile(false);
          return;
        }
        const res = await syncClashProxies({
          sub_url: subUrl,
          target_profile: cleaned,
          listen_host: clashListenHost.trim() || "127.0.0.1",
          start_port: Number(newProfileClashStartPort) || 20001,
          max_ports: Number(newProfileClashMaxPorts) || 40,
          exclude_regex: clashExcludeRegex.trim(),
          priority_regions: clashPriorityRegions.trim(),
          write_config: true,
          reload_mihomo: newProfileClashReload,
          activate_profile: false,
        });
        setLastClashResult(res.result);
        await loadProxies(cleaned);
        setNotice({
          type: "success",
          text: `已创建 Profile [${cleaned}] 并同步 ${res.result.mapped_count} 个 Clash 多端口节点 (${res.result.start_port}..${res.result.end_port})`,
        });
      } else {
        const initProxies = newProfileProxy
          .split("\n")
          .map((s) => s.trim())
          .filter(Boolean);
        if (initProxies.length > 0) {
          await updateProxies(initProxies, true, cleaned);
          await loadProxies(cleaned);
          setNotice({
            type: "success",
            text: `已创建 Profile [${cleaned}] 并写入 ${initProxies.length} 条代理节点到数据库`,
          });
        } else {
          setProfiles((prev) =>
            prev.some((p) => p.profile === cleaned)
              ? prev
              : [...prev, { profile: cleaned, label: cleaned, total: 0, clash_count: 0 }]
          );
          setViewProfile(cleaned);
          setNodes([]);
          setSummary({ total: 0, active: 0, cooling: 0 });
          setProxyText("");
          setNotice({
            type: "info",
            text: `已新建 Profile [${cleaned}] 视图，请在下方填入代理并点击「保存到 [${cleaned}] Profile」或使用 Clash 订阅同步写入`,
          });
        }
      }
      setNewProfileName("");
      setNewProfileProxy("");
      setNewProfileClashSubUrl("");
      setNewProfileMode("manual");
      setShowCreateProfile(false);
    } catch (err: any) {
      setNotice({
        type: "error",
        text: `创建 Profile 失败: ${err?.message || "未知错误"}`,
      });
    } finally {
      setCreatingProfile(false);
    }
  };

  const handleSyncClash = async () => {
    setSyncingClash(true);
    setNotice({
      type: "info",
      text: "正在拉取/解析机场订阅并生成 Mihomo 多端口代理池...",
    });
    try {
      const res = await syncClashProxies({
        sub_url: subUrlInput.trim() || undefined,
        raw_subscription: rawSubInput.trim() || undefined,
        target_profile: clashTargetProfile,
        listen_host: clashListenHost.trim() || "127.0.0.1",
        start_port: Number(clashStartPort) || 20001,
        max_ports: Number(clashMaxPorts) || 40,
        exclude_regex: clashExcludeRegex.trim(),
        priority_regions: clashPriorityRegions.trim(),
        write_config: true,
        reload_mihomo: clashReloadMihomo,
        activate_profile: clashActivateAfterSync,
      });
      setLastClashResult(res.result);
      if (Array.isArray(res.profiles)) {
        setProfiles(res.profiles);
      }
      setActiveProfile(res.active_profile);
      setViewProfile(res.result.target_profile);
      await loadProxies(res.result.target_profile);
      window.dispatchEvent(new Event("pool-updated"));

      const warnSuffix = res.result.reload_warning
        ? `（提示：${res.result.reload_warning}）`
        : res.result.mihomo_reloaded
          ? `（已通过 ${res.result.reload_method} 热重载 Mihomo）`
          : "";
      setNotice({
        type: "success",
        text: `机场订阅转换完成：共解析 ${res.result.total_parsed} 个节点，智能过滤剔除 ${res.result.filtered_out} 个，成功映射 ${res.result.mapped_count} 个多端口节点 (${res.result.start_port}..${res.result.end_port}) 写入 Profile [${res.result.target_profile}] ${warnSuffix}`,
      });
    } catch (err: any) {
      setNotice({
        type: "error",
        text: `机场订阅同步失败: ${err?.message || "未知错误"}`,
      });
    } finally {
      setSyncingClash(false);
    }
  };

  const handleCopyYaml = async () => {
    if (!lastClashResult?.generated_config) return;
    try {
      await navigator.clipboard.writeText(lastClashResult.generated_config);
      setCopiedYaml(true);
      window.setTimeout(() => setCopiedYaml(false), 1600);
    } catch {
      // ignore
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
        text: `实时拉取模型目录失败（已保留当前结果）：${nextState.error}`,
      });
      return;
    }
    if (nextState.source === "fallback") {
      setNotice({
        type: "info",
        text: nextState.warning || "当前无可用登录态账号，模型目录为空",
      });
      return;
    }
    const upstreamN = nextState.catalog.filter(
      (m) => m.source === "upstream"
    ).length;
    setNotice({
      type: "success",
      text: `已实时拉取并缓存 ${nextState.catalog.length} 个模型（上游 ${upstreamN}）`,
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
      text: `正在并发探测当前激活 Profile [${activeProfile}] 下所有代理节点连通性与延迟...`,
    });
    try {
      const res = await probeProxies();
      setSummary(res.summary || { total: 0, active: 0, cooling: 0 });
      if (viewProfile === activeProfile) {
        setNodes(res.nodes || []);
      }
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
      const res = await updateProxies(list, true, viewProfile);
      setNotice({
        type: "success",
        text: `已保存 ${res.proxies_count} 条代理至数据库 Profile [${viewProfile}]${
          viewProfile === activeProfile
            ? `（已热重载内存池并校准迁移 ${res.migrated} 个账号）`
            : "（切换至该 Profile 时将自动生效）"
        }`,
      });
      await loadProxies(viewProfile);
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
            多环境 Profile 代理矩阵与模型目录
          </h1>
          <p className="text-xs z-text-muted mt-1">
            基于本地 <code className="font-mono-num">.env (APP_PROFILE={envProfile})</code>{" "}
            隔离家里 / 公司 / 生产 VPS 的代理池与账号粘性绑定，支持 Clash Verge 机场订阅一键转 Mihomo 多端口代理池。
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => setShowClashPanel((v) => !v)}
            className="inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn text-xs font-medium cursor-pointer"
          >
            <Sparkles size={13} />
            <span>Clash 机场订阅转多端口</span>
            {showClashPanel ? <ChevronUp size={13} /> : <ChevronDown size={13} />}
          </button>
          <button
            type="button"
            onClick={handleProbeAll}
            disabled={probing}
            className="inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl z-primary-btn text-xs font-medium cursor-pointer"
          >
            <Zap size={13} />
            <span>
              {probing
                ? "探测测活中..."
                : `并发探活当前环境 (${activeProfile})`}
            </span>
          </button>
        </div>
      </div>

      {notice && (
        <NoticeBanner
          type={notice.type}
          text={notice.text}
          onClose={() => setNotice(null)}
        />
      )}

      {/* Runtime Profile Switcher Bar */}
      <div className="p-card rounded-2xl p-3.5 space-y-3 text-xs">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <Layers size={14} />
            <span className="font-semibold text-sm">
              运行时环境 Profile 代理隔离库
            </span>
            <span className="px-2 py-0.5 rounded-full border z-pill-success font-mono-num">
              当前激活: {activeProfile}
            </span>
            <span className="px-2 py-0.5 rounded-full border z-pill-info font-mono-num">
              .env 默认: APP_PROFILE={envProfile}
            </span>
          </div>
          <div className="flex items-center gap-2">
            {viewProfile !== activeProfile && (
              <button
                type="button"
                disabled={switchingProfile}
                onClick={() => handleActivateProfile(viewProfile)}
                className="inline-flex items-center gap-1.5 px-3 py-1 rounded-xl z-primary-btn font-medium cursor-pointer"
              >
                <Server size={12} />
                <span>
                  {switchingProfile
                    ? "正在热切换..."
                    : `将 [${viewProfile}] 设为当前运行环境`}
                </span>
              </button>
            )}
            {PROFILE_PRESET_EXAMPLES[viewProfile] && (
              <button
                type="button"
                onClick={() => handleFillPresetExample(viewProfile)}
                className="inline-flex items-center gap-1 px-2.5 py-1 rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
              >
                <span>填入 {viewProfile} 默认代理模板</span>
              </button>
            )}
            <button
              type="button"
              onClick={() => setShowCreateProfile((v) => !v)}
              className="inline-flex items-center gap-1 px-2.5 py-1 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
            >
              <Plus size={12} />
              <span>新建 Profile</span>
            </button>
          </div>
        </div>

        {showCreateProfile && (
          <div
            className="p-3 rounded-xl space-y-2.5"
            style={{
              background: "var(--bg-input)",
              border: "1px solid var(--hairline)",
            }}
          >
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="font-semibold">
                创建自定义 Runtime Profile（与本地 .env 的 APP_PROFILE 对应）
              </span>
              <span className="z-text-muted">
                名称仅支持 a-z、0-9、-、_，例如 laptop / staging / hk_vps
              </span>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-2.5">
              <div className="space-y-1">
                <label className="z-text-sec block">Profile 名称</label>
                <input
                  type="text"
                  value={newProfileName}
                  onChange={(e) => setNewProfileName(e.target.value)}
                  placeholder="例如 laptop / hk_vps"
                  className="w-full px-3 py-1.5 rounded-xl p-input font-mono-num"
                />
              </div>
              <div className="space-y-1">
                <label className="z-text-sec block">代理初始化方式</label>
                <select
                  value={newProfileMode}
                  onChange={(e) =>
                    setNewProfileMode(e.target.value as "manual" | "clash")
                  }
                  className="w-full px-3 py-1.5 rounded-xl p-input font-mono-num"
                >
                  <option value="manual">手动固定代理 (如 127.0.0.1:10809)</option>
                  <option value="clash">Clash 机场订阅转多端口</option>
                </select>
              </div>
              {newProfileMode === "manual" ? (
                <div className="space-y-1">
                  <label className="z-text-sec block">可选初始代理</label>
                  <input
                    type="text"
                    value={newProfileProxy}
                    onChange={(e) => setNewProfileProxy(e.target.value)}
                    placeholder="http://127.0.0.1:7890（留空稍后编辑）"
                    className="w-full px-3 py-1.5 rounded-xl p-input font-mono-num"
                  />
                </div>
              ) : (
                <div className="grid grid-cols-2 gap-2">
                  <div className="space-y-1">
                    <label className="z-text-sec block">起始端口</label>
                    <input
                      type="number"
                      value={newProfileClashStartPort}
                      onChange={(e) =>
                        setNewProfileClashStartPort(Number(e.target.value) || 20001)
                      }
                      className="w-full px-3 py-1.5 rounded-xl p-input font-mono-num"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="z-text-sec block">最大节点数</label>
                    <input
                      type="number"
                      value={newProfileClashMaxPorts}
                      onChange={(e) =>
                        setNewProfileClashMaxPorts(Number(e.target.value) || 40)
                      }
                      className="w-full px-3 py-1.5 rounded-xl p-input font-mono-num"
                    />
                  </div>
                </div>
              )}
            </div>
            {newProfileMode === "clash" && (
              <div className="grid grid-cols-1 md:grid-cols-3 gap-2.5 items-end">
                <div className="md:col-span-2 space-y-1">
                  <label className="z-text-sec block">
                    机场 Clash 订阅链接 (CLASH_SUB_URL)
                  </label>
                  <input
                    type="url"
                    value={newProfileClashSubUrl}
                    onChange={(e) => setNewProfileClashSubUrl(e.target.value)}
                    placeholder="https://your-airport.example.com/api/v1/client/subscribe?token=..."
                    className="w-full px-3 py-1.5 rounded-xl p-input font-mono-num"
                  />
                </div>
                <label className="inline-flex items-center gap-1.5 pb-1.5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={newProfileClashReload}
                    onChange={(e) => setNewProfileClashReload(e.target.checked)}
                  />
                  <span>生成配置并热重载 Mihomo</span>
                </label>
              </div>
            )}
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setShowCreateProfile(false)}
                className="px-3 py-1 rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
              >
                取消
              </button>
              <button
                type="button"
                disabled={creatingProfile}
                onClick={handleCreateCustomProfile}
                className="inline-flex items-center gap-1 px-3 py-1 rounded-xl z-primary-btn font-medium cursor-pointer"
              >
                <Plus size={12} />
                <span>{creatingProfile ? "创建中..." : "创建并切换视图"}</span>
              </button>
            </div>
          </div>
        )}

        <div className="grid grid-cols-1 sm:grid-cols-3 gap-2.5">
          {profiles.map((item) => {
            const isSelected = viewProfile === item.profile;
            const isActive = activeProfile === item.profile;
            const presetMeta = PROFILE_PRESET_EXAMPLES[item.profile];
            return (
              <div
                key={item.profile}
                onClick={() => handleSelectViewProfile(item.profile)}
                className={`p-3 rounded-xl cursor-pointer transition-all flex flex-col justify-between gap-1.5 ${
                  isSelected
                    ? "ring-2 ring-zinc-900 dark:ring-zinc-200"
                    : "z-row-hover"
                }`}
                style={{
                  background: "var(--bg-input)",
                  border: "1px solid var(--hairline)",
                }}
              >
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-1.5 font-semibold">
                    <span
                      className={
                        isActive ? "status-dot-green" : "status-dot-amber"
                      }
                    />
                    <span>{item.label}</span>
                  </div>
                  <div className="flex items-center gap-1">
                    {isActive && (
                      <span className="px-2 py-0.5 rounded-md border z-pill-success font-mono-num text-[11px]">
                        生效中
                      </span>
                    )}
                    <span className="px-2 py-0.5 rounded-md border z-border font-mono-num text-[11px]">
                      {item.total} 节点
                    </span>
                  </div>
                </div>
                <div className="z-text-muted text-[11px] flex items-center justify-between">
                  <span className="truncate">
                    {presetMeta?.subtitle || `Profile: ${item.profile}`}
                  </span>
                  {item.clash_count > 0 && (
                    <span className="font-mono-num shrink-0 ml-1">
                      Clash多端口: {item.clash_count}
                    </span>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Clash Verge Subscription to Multi-Port Generator Panel */}
      {showClashPanel && (
        <div className="p-card rounded-2xl p-4 space-y-3.5 text-xs">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <h2 className="text-sm font-semibold flex items-center gap-1.5">
                <Sparkles size={14} />
                <span>
                  Clash Verge 机场订阅转 Mihomo 多端口代理池（VPS 翻墙与多出口防风控）
                </span>
              </h2>
              <p className="z-text-muted mt-0.5">
                自动拉取机场订阅、剔除「剩余流量/到期/CN/HK/RU」等伪节点与高风控地区，按{" "}
                <code className="font-mono-num">US &gt; JP &gt; SG &gt; TW &gt; KR</code>{" "}
                优先级将每个节点映射为独立本地端口（如{" "}
                <code className="font-mono-num">127.0.0.1:20001..20040</code>）并写入数据库。
              </p>
            </div>
            {clashConfig?.sub_configured && (
              <span className="px-2.5 py-1 rounded-full border z-pill-success font-mono-num">
                .env 已配置订阅 ({clashConfig.sub_url_masked})
              </span>
            )}
          </div>

          <div className="grid grid-cols-1 md:grid-cols-4 gap-3">
            <div className="md:col-span-2 space-y-1">
              <label className="font-medium z-text-sec block">
                机场 Clash / Mihomo 订阅链接（留空则使用服务器 .env 的 CLASH_SUB_URL）
              </label>
              <input
                type="url"
                value={subUrlInput}
                onChange={(e) => setSubUrlInput(e.target.value)}
                placeholder={
                  clashConfig?.sub_configured
                    ? `已配置默认订阅 ${clashConfig.sub_url_masked}（可留空直接同步，或输入新链接覆盖）`
                    : "https://your-airport.example.com/api/v1/client/subscribe?token=..."
                }
                className="w-full px-3 py-1.5 rounded-xl p-input font-mono-num"
              />
            </div>

            <div className="space-y-1">
              <label className="font-medium z-text-sec block">
                写入目标 Profile
              </label>
              <select
                value={clashTargetProfile}
                onChange={(e) => setClashTargetProfile(e.target.value)}
                className="w-full px-3 py-1.5 rounded-xl p-input font-mono-num"
              >
                {profiles.map((p) => (
                  <option key={p.profile} value={p.profile}>
                    {p.label}
                  </option>
                ))}
              </select>
            </div>

            <div className="grid grid-cols-2 gap-2">
              <div className="space-y-1">
                <label className="font-medium z-text-sec block">起始端口</label>
                <input
                  type="number"
                  value={clashStartPort}
                  onChange={(e) =>
                    setClashStartPort(Number(e.target.value) || 20001)
                  }
                  className="w-full px-3 py-1.5 rounded-xl p-input font-mono-num"
                />
              </div>
              <div className="space-y-1">
                <label className="font-medium z-text-sec block">最大节点数</label>
                <input
                  type="number"
                  value={clashMaxPorts}
                  onChange={(e) =>
                    setClashMaxPorts(Number(e.target.value) || 40)
                  }
                  className="w-full px-3 py-1.5 rounded-xl p-input font-mono-num"
                />
              </div>
            </div>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
            <div className="space-y-1">
              <label className="font-medium z-text-sec block">
                地区优先级排序（逗号分隔）
              </label>
              <input
                type="text"
                value={clashPriorityRegions}
                onChange={(e) => setClashPriorityRegions(e.target.value)}
                className="w-full px-3 py-1.5 rounded-xl p-input font-mono-num"
              />
            </div>
            <div className="md:col-span-2 space-y-1">
              <label className="font-medium z-text-sec block">
                自动剔除伪节点与高风控地区正则（CLASH_EXCLUDE_REGEX）
              </label>
              <input
                type="text"
                value={clashExcludeRegex}
                onChange={(e) => setClashExcludeRegex(e.target.value)}
                className="w-full px-3 py-1.5 rounded-xl p-input font-mono-num"
              />
            </div>
          </div>

          <details className="rounded-xl p-2.5 border z-border">
            <summary className="cursor-pointer font-medium z-text-sec">
              可选：直接粘贴 Clash YAML 或 Base64 订阅内容（离线转换/无需服务器直连机场订阅域名）
            </summary>
            <textarea
              rows={3}
              value={rawSubInput}
              onChange={(e) => setRawSubInput(e.target.value)}
              placeholder="在此粘贴 Clash YAML (含 proxies:) 或 Base64 节点列表（留空则通过上方订阅 URL 拉取）"
              className="w-full mt-2 p-2.5 rounded-xl p-code-box font-mono-num resize-y"
            />
          </details>

          <div className="flex flex-wrap items-center justify-between gap-3 pt-1">
            <div className="flex flex-wrap items-center gap-4">
              <label className="inline-flex items-center gap-1.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={clashReloadMihomo}
                  onChange={(e) => setClashReloadMihomo(e.target.checked)}
                />
                <span>
                  生成 <code className="font-mono-num">/etc/mihomo/config.yaml</code> 并自动热重载本机 Mihomo
                </span>
              </label>
              <label className="inline-flex items-center gap-1.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={clashActivateAfterSync}
                  onChange={(e) => setClashActivateAfterSync(e.target.checked)}
                />
                <span>同步后立即将目标 Profile 设为当前运行环境</span>
              </label>
            </div>

            <div className="flex items-center gap-2">
              {lastClashResult?.generated_config && (
                <button
                  type="button"
                  onClick={() => setShowGeneratedYaml((v) => !v)}
                  className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
                >
                  <FileCode2 size={13} />
                  <span>
                    {showGeneratedYaml
                      ? "隐藏 Mihomo YAML"
                      : "预览生成的 Mihomo YAML"}
                  </span>
                </button>
              )}
              <button
                type="button"
                disabled={syncingClash}
                onClick={handleSyncClash}
                className="inline-flex items-center gap-1.5 px-4 py-1.5 rounded-xl z-primary-btn font-medium cursor-pointer"
              >
                <RefreshCw
                  size={13}
                  className={syncingClash ? "animate-spin" : ""}
                />
                <span>
                  {syncingClash
                    ? "正在转换并写入数据库..."
                    : "一键拉取订阅 & 生成多端口入库"}
                </span>
              </button>
            </div>
          </div>

          {showGeneratedYaml && lastClashResult?.generated_config && (
            <div className="rounded-xl overflow-hidden p-code-box">
              <div className="px-3 py-2 border-b z-border flex items-center justify-between">
                <span className="font-mono-num z-text-muted">
                  {lastClashResult.config_path || "/etc/mihomo/config.yaml"} · 端口{" "}
                  {lastClashResult.start_port}..{lastClashResult.end_port} (
                  {lastClashResult.mapped_count} 节点)
                </span>
                <button
                  type="button"
                  onClick={handleCopyYaml}
                  className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg border z-border z-surface z-ghost-btn cursor-pointer"
                >
                  {copiedYaml ? <Check size={12} /> : <Copy size={12} />}
                  <span>{copiedYaml ? "已复制" : "复制 YAML"}</span>
                </button>
              </div>
              <pre className="p-3 font-mono-num text-xs max-h-60 overflow-auto m-0">
                {lastClashResult.generated_config}
              </pre>
            </div>
          )}
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Left: Outbound Proxy Pool */}
        <div className="p-card rounded-2xl p-4 space-y-4 text-xs flex flex-col">
          <div className="flex items-center justify-between gap-2">
            <h2 className="text-sm font-semibold flex items-center gap-1.5">
              <Globe size={14} />
              <span>
                Profile [{viewProfile}] 代理池 ({summary.active}/{summary.total}{" "}
                健康
                {summary.cooling > 0 ? ` · ${summary.cooling} 冷却` : ""})
              </span>
            </h2>
            <span className="z-text-muted">
              {viewProfile === activeProfile
                ? "当前正在生效 · 连续错误 ≥ 3 次自动熔断 5m"
                : "预览其他环境配置 · 可编辑保存或点击上方切换生效"}
            </span>
          </div>

          <div className="space-y-2 max-h-96 overflow-y-auto pr-1">
            {nodes.length === 0 && (
              <div
                className="p-4 rounded-xl text-center z-text-muted"
                style={{
                  background: "var(--bg-input)",
                  border: "1px solid var(--hairline)",
                }}
              >
                当前 Profile [{viewProfile}] 尚未配置代理节点（可在下方填入代理并保存，或使用上方「填入{" "}
                {viewProfile} 默认代理模板」/「Clash 订阅转多端口」一键入库）。
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
                  key={`${node.profile || viewProfile}-${node.url}`}
                  className="p-3 rounded-xl space-y-1.5"
                  style={{
                    background: "var(--bg-input)",
                    border: "1px solid var(--hairline)",
                  }}
                >
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2 min-w-0 flex-wrap">
                      <span
                        className={
                          healthy ? "status-dot-green" : "status-dot-amber"
                        }
                      />
                      <span className="font-mono-num font-medium truncate">
                        {node.url}
                      </span>
                      {node.region && (
                        <span className="px-1.5 py-0.5 rounded-md border z-pill-info font-mono-num text-[11px]">
                          {node.region}
                        </span>
                      )}
                      {node.node_name && (
                        <span
                          className="px-2 py-0.5 rounded-md border z-border truncate max-w-[220px]"
                          title={node.node_name}
                        >
                          {node.node_name}
                        </span>
                      )}
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
                    <span>
                      粘性绑定：{node.bound_accounts} 个账号
                      {node.source === "clash_sub" ? " · 来源: Clash 多端口" : ""}
                    </span>
                    <span>
                      连续失败：{node.consecutive_errors} · 累计：
                      {node.total_failures}
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
              编辑 Profile [{viewProfile}] 代理列表（每行一条 http:// 或 socks5://，持久化写入数据库并按环境独立保存）
            </label>
            <textarea
              rows={4}
              value={proxyText}
              onChange={(e) => setProxyText(e.target.value)}
              placeholder={
                PROFILE_PRESET_EXAMPLES[viewProfile]?.example ||
                "http://127.0.0.1:10809\nsocks5://127.0.0.1:10808"
              }
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
                <span>
                  {saving
                    ? "保存校准中..."
                    : `保存到 [${viewProfile}] Profile 并校准`}
                </span>
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
                    （上游 {counts.upstream}
                    {modelsCachedAt
                      ? ` · ${formatClockTime(modelsCachedAt)}`
                      : ""}
                    ）
                  </span>
                ) : (
                  <span>
                    暂无实时模型
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
