import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Check,
  Cloud,
  Copy,
  Download,
  Eye,
  EyeOff,
  FileCode2,
  Globe,
  HardDrive,
  KeyRound,
  Layers,
  Lock,
  RefreshCw,
  RotateCcw,
  Save,
  Search,
  Server,
  Shield,
  Sparkles,
  Upload,
  X,
  Zap,
} from "lucide-react";
import {
  deleteSystemSettingOverride,
  exportSystemSettings,
  fetchSystemSettings,
  revealSystemSetting,
  updateSystemSettings,
  type SystemSettingItem,
  type SystemSettingsResponse,
} from "../api/client";
import { NoticeBanner, triggerTextDownload } from "../components/ui";

type CategoryId =
  | "featured"
  | "multimodal"
  | "proxy"
  | "pool"
  | "gateway"
  | "storage"
  | "bootstrap";

type FilterMode = "all" | "modified" | "secrets";

interface CategoryMeta {
  id: CategoryId;
  label: string;
  subtitle: string;
}

const CATEGORIES: CategoryMeta[] = [
  { id: "featured", label: "常用核心配置", subtitle: "高频能力 Key、代理与水位一键直达" },
  { id: "multimodal", label: "多模态云端 Key", subtitle: "SiliconFlow / DashScope / Grok / 语音渲染" },
  { id: "proxy", label: "Clash 订阅与代理", subtitle: "机场订阅转 Mihomo 多端口池与默认出口" },
  { id: "pool", label: "号池与热池策略", subtitle: "热池/待命池水位、定时轮换与卡密无头续期" },
  { id: "gateway", label: "网关与对话协议", subtitle: "Bearer 鉴权、流式输出与工具调用协议" },
  { id: "storage", label: "存储与审计日志", subtitle: "本地多媒体目录、S3/R2、七牛与审计轮转" },
  { id: "bootstrap", label: "引导层只读 (.env)", subtitle: "精简 8 行引导变量与 GitHub Secret 模板" },
];

const SCOPE_LABELS: Record<string, string> = {
  global: "全局默认 (Global)",
  home: "本地调试 (home)",
  office: "办公网络 (office)",
  vps: "生产 VPS (vps)",
};

function renderCategoryIcon(id: CategoryId) {
  switch (id) {
    case "featured":
      return <Zap size={14} className="shrink-0 text-amber-500" />;
    case "multimodal":
      return <Sparkles size={14} className="shrink-0 text-violet-500" />;
    case "proxy":
      return <Cloud size={14} className="shrink-0 text-sky-500" />;
    case "pool":
      return <Shield size={14} className="shrink-0 text-emerald-500" />;
    case "gateway":
      return <Globe size={14} className="shrink-0 text-blue-500" />;
    case "storage":
      return <HardDrive size={14} className="shrink-0 text-cyan-500" />;
    case "bootstrap":
      return <Lock size={14} className="shrink-0 text-zinc-500" />;
  }
}

function sourceBadge(item: SystemSettingItem) {
  if (item.read_only) {
    return (
      <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[11px] font-medium bg-zinc-500/10 z-text-muted border z-border">
        <Lock size={10} /> 引导只读
      </span>
    );
  }
  switch (item.source) {
    case "profile":
      return (
        <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[11px] font-medium bg-indigo-500/12 text-indigo-600 dark:text-indigo-300 border border-indigo-500/25">
          Profile 覆盖
        </span>
      );
    case "global":
      return (
        <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[11px] font-medium z-pill-success">
          DB 全局
        </span>
      );
    case "env":
      return (
        <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[11px] font-medium z-pill-warn">
          .env 继承
        </span>
      );
    default:
      return (
        <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[11px] font-medium bg-zinc-500/10 z-text-muted">
          默认值
        </span>
      );
  }
}

export const SettingsView: React.FC = () => {
  const [scope, setScope] = useState<string>("global");
  const [category, setCategory] = useState<CategoryId>("featured");
  const [filterMode, setFilterMode] = useState<FilterMode>("all");
  const [searchText, setSearchText] = useState<string>("");

  const [data, setData] = useState<SystemSettingsResponse | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [saving, setSaving] = useState<boolean>(false);
  const [notice, setNotice] = useState<{ type: "info" | "success" | "error"; text: string } | null>(null);

  // dirtyMap 仅记录用户真正触碰修改过的 Key -> 值；未触碰的敏感掩码字段绝不回传后端
  const [dirtyMap, setDirtyMap] = useState<Record<string, string>>({});
  const [revealedMap, setRevealedMap] = useState<Record<string, string>>({});
  const [plainVisible, setPlainVisible] = useState<Record<string, boolean>>({});
  const [revealingKey, setRevealingKey] = useState<string | null>(null);
  const [resettingKey, setResettingKey] = useState<string | null>(null);
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  // 导入 / 导出弹窗状态
  const [importOpen, setImportOpen] = useState<boolean>(false);
  const [importText, setImportText] = useState<string>("");
  const [exportOpen, setExportOpen] = useState<boolean>(false);
  const [exportFormat, setExportFormat] = useState<"dotenv" | "json">("dotenv");
  const [exportIncludeSecrets, setExportIncludeSecrets] = useState<boolean>(false);
  const [exportContent, setExportContent] = useState<string>("");
  const [exportLoading, setExportLoading] = useState<boolean>(false);

  const loadSettings = useCallback(async (targetScope: string) => {
    setLoading(true);
    try {
      const resp = await fetchSystemSettings(targetScope);
      setData(resp);
      setDirtyMap({});
      setRevealedMap({});
      setPlainVisible({});
    } catch (e) {
      setNotice({
        type: "error",
        text: e instanceof Error ? e.message : "加载系统配置失败",
      });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadSettings(scope);
  }, [scope, loadSettings]);

  const dirtyKeys = useMemo(() => Object.keys(dirtyMap), [dirtyMap]);
  const isDirty = dirtyKeys.length > 0;

  // 若切换至非 global 的 Profile，且当前在 multimodal/gateway 等纯全局分类，自动切到 proxy（用户仍可点选 pool/storage）
  const handleSelectScope = (nextScope: string) => {
    if (nextScope === scope) return;
    if (isDirty && !window.confirm(`当前共有 ${dirtyKeys.length} 项未保存更改，切换视图将丢弃这些未保存改动，确认继续吗？`)) {
      return;
    }
    setScope(nextScope);
    if (nextScope !== "global" && (category === "multimodal" || category === "gateway" || category === "bootstrap")) {
      setCategory("proxy");
    } else if (nextScope === "global" && category === "proxy" && searchText === "") {
      // keep current category
    }
  };

  const handleSave = useCallback(async () => {
    if (dirtyKeys.length === 0 || saving) return;
    setSaving(true);
    setNotice(null);
    try {
      const resp = await updateSystemSettings({
        scope,
        values: dirtyMap,
      });
      setData(resp);
      setDirtyMap({});
      setRevealedMap({});
      setPlainVisible({});
      setNotice({
        type: "success",
        text: `已保存 ${dirtyKeys.length} 项配置（scope=${resp.scope}），内存配置、多供应商引擎与热池策略已零重启同步生效`,
      });
    } catch (e) {
      setNotice({
        type: "error",
        text: e instanceof Error ? e.message : "保存系统配置失败",
      });
    } finally {
      setSaving(false);
    }
  }, [dirtyKeys.length, dirtyMap, saving, scope]);

  // Ctrl+S / Cmd+S 快捷保存
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
        if (dirtyKeys.length > 0) {
          e.preventDefault();
          void handleSave();
        }
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [dirtyKeys.length, handleSave]);

  const handleRevealAndEdit = async (item: SystemSettingItem) => {
    if (Object.prototype.hasOwnProperty.call(revealedMap, item.key)) {
      setPlainVisible((prev) => ({ ...prev, [item.key]: !prev[item.key] }));
      return;
    }
    setRevealingKey(item.key);
    try {
      const res = await revealSystemSetting(scope, item.key);
      const plain = res.value ?? "";
      setRevealedMap((prev) => ({ ...prev, [item.key]: plain }));
      setPlainVisible((prev) => ({ ...prev, [item.key]: true }));
    } catch (e) {
      setNotice({
        type: "error",
        text: e instanceof Error ? e.message : `读取 ${item.key} 明文失败`,
      });
    } finally {
      setRevealingKey(null);
    }
  };

  const handleCopyValue = async (tag: string, text: string, label?: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopiedKey(tag);
      window.setTimeout(() => setCopiedKey((prev) => (prev === tag ? null : prev)), 1500);
      if (label) {
        setNotice({ type: "success", text: `已复制 ${label}` });
      }
    } catch {
      setNotice({ type: "error", text: "剪贴板写入失败，请手动全选复制" });
    }
  };

  const handleCopySecretPlain = async (item: SystemSettingItem) => {
    let plain = dirtyMap[item.key] ?? revealedMap[item.key];
    if (plain === undefined) {
      setRevealingKey(item.key);
      try {
        const res = await revealSystemSetting(scope, item.key);
        plain = res.value ?? "";
        setRevealedMap((prev) => ({ ...prev, [item.key]: plain ?? "" }));
      } catch (e) {
        setNotice({ type: "error", text: e instanceof Error ? e.message : "读取明文失败" });
        return;
      } finally {
        setRevealingKey(null);
      }
    }
    await handleCopyValue(`val:${item.key}`, plain, `${item.key} 明文`);
  };

  const handleResetItem = async (item: SystemSettingItem) => {
    setResettingKey(item.key);
    setNotice(null);
    try {
      const resp = await deleteSystemSettingOverride(scope, item.key);
      setData(resp);
      setDirtyMap((prev) => {
        const next = { ...prev };
        delete next[item.key];
        return next;
      });
      setRevealedMap((prev) => {
        const next = { ...prev };
        delete next[item.key];
        return next;
      });
      setNotice({
        type: "success",
        text:
          scope === "global"
            ? `${item.label} (${item.key}) 已恢复为环境变量 / 代码默认值`
            : `${item.label} (${item.key}) 已取消 ${scope} 覆盖，自动继承全局配置`,
      });
    } catch (e) {
      setNotice({
        type: "error",
        text: e instanceof Error ? e.message : "恢复默认配置失败",
      });
    } finally {
      setResettingKey(null);
    }
  };

  const handleFieldChange = (item: SystemSettingItem, nextVal: string) => {
    setDirtyMap((prev) => ({ ...prev, [item.key]: nextVal }));
  };

  const handleDotEnvImport = async () => {
    if (!importText.trim()) return;
    setSaving(true);
    setNotice(null);
    try {
      const resp = await updateSystemSettings({
        scope,
        dotenv: importText,
      });
      setData(resp);
      setDirtyMap({});
      setRevealedMap({});
      setImportOpen(false);
      setImportText("");
      setNotice({
        type: "success",
        text: `已批量解析并导入 .env 配置至 ${resp.scope}，网关与号池参数已热重载生效`,
      });
    } catch (e) {
      setNotice({
        type: "error",
        text: e instanceof Error ? e.message : "批量导入 .env 失败",
      });
    } finally {
      setSaving(false);
    }
  };

  const loadExportPreview = useCallback(
    async (fmt: "dotenv" | "json", includeSecrets: boolean) => {
      setExportLoading(true);
      try {
        const res = await exportSystemSettings({
          scope,
          format: fmt,
          includeSecrets,
        });
        setExportContent(res.content);
      } catch (e) {
        setNotice({ type: "error", text: e instanceof Error ? e.message : "导出预览失败" });
      } finally {
        setExportLoading(false);
      }
    },
    [scope]
  );

  useEffect(() => {
    if (exportOpen) {
      void loadExportPreview(exportFormat, exportIncludeSecrets);
    }
  }, [exportOpen, exportFormat, exportIncludeSecrets, loadExportPreview]);

  const items = data?.items ?? [];

  // 若在 Profile 覆盖视图下，先按 profile_overridable 过滤；若用户在搜索时允许全域搜索，给出清晰标注
  const scopeBaseItems = useMemo(() => {
    if (scope === "global") return items;
    return items.filter((it) => it.profile_overridable);
  }, [items, scope]);

  const categoryCounts = useMemo(() => {
    const map: Record<CategoryId, { total: number; configured: number }> = {
      featured: { total: 0, configured: 0 },
      multimodal: { total: 0, configured: 0 },
      proxy: { total: 0, configured: 0 },
      pool: { total: 0, configured: 0 },
      gateway: { total: 0, configured: 0 },
      storage: { total: 0, configured: 0 },
      bootstrap: { total: 0, configured: 0 },
    };
    for (const it of scopeBaseItems) {
      if (it.is_featured) {
        map.featured.total += 1;
        if (scope === "global" ? it.is_configured : it.has_scope_override) {
          map.featured.configured += 1;
        }
      }
      const cat = it.category as CategoryId;
      if (map[cat]) {
        map[cat].total += 1;
        if (scope === "global" ? it.is_configured : it.has_scope_override) {
          map[cat].configured += 1;
        }
      }
    }
    return map;
  }, [scopeBaseItems, scope]);

  const visibleItems = useMemo(() => {
    const q = searchText.trim().toLowerCase();
    return scopeBaseItems.filter((it) => {
      // 有搜索关键字时允许跨分类快速定位
      if (!q) {
        if (category === "featured") {
          if (!it.is_featured) return false;
        } else if (it.category !== category) {
          return false;
        }
      } else {
        const hay = `${it.key} ${it.label} ${it.description} ${it.group_title}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }

      if (filterMode === "modified") {
        const modifiedInDirty = Object.prototype.hasOwnProperty.call(dirtyMap, it.key);
        const modifiedInDB = scope === "global" ? it.has_global_value : it.has_scope_override;
        if (!modifiedInDirty && !modifiedInDB) return false;
      } else if (filterMode === "secrets") {
        if (!it.is_secret) return false;
      }
      return true;
    });
  }, [scopeBaseItems, searchText, category, filterMode, dirtyMap, scope]);

  // 按 GroupTitle 分组保持顺序
  const groupedSections = useMemo(() => {
    const groups: Array<{ title: string; items: SystemSettingItem[] }> = [];
    const indexMap = new Map<string, number>();
    for (const it of visibleItems) {
      const gName = it.group_title || "基础配置";
      const existingIdx = indexMap.get(gName);
      if (existingIdx !== undefined) {
        groups[existingIdx].items.push(it);
      } else {
        indexMap.set(gName, groups.length);
        groups.push({ title: gName, items: [it] });
      }
    }
    return groups;
  }, [visibleItems]);

  // 常用核心能力点亮概览
  const featuredSignals = useMemo(() => {
    const findKey = (k: string) => items.find((i) => i.key === k);
    return [
      { label: "SiliconFlow 多模态", key: "SILICONFLOW_API_KEY", ok: Boolean(findKey("SILICONFLOW_API_KEY")?.is_configured) },
      { label: "DashScope 百炼", key: "DASHSCOPE_API_KEY", ok: Boolean(findKey("DASHSCOPE_API_KEY")?.is_configured) },
      { label: "Grok SSO", key: "GROK_SSO_COOKIES", ok: Boolean(findKey("GROK_SSO_COOKIES")?.is_configured) },
      { label: "Clash 订阅同步", key: "CLASH_SUB_URL", ok: Boolean(findKey("CLASH_SUB_URL")?.is_configured) },
      {
        label: "热池水位限流",
        key: "POOL_CHATGPT_FREE_ACTIVE_TARGET",
        ok: Number(findKey("POOL_CHATGPT_FREE_ACTIVE_TARGET")?.effective_value || "0") > 0,
      },
      { label: "API Bearer 鉴权", key: "Authorization", ok: Boolean(findKey("Authorization")?.is_configured) },
    ];
  }, [items]);

  const scopesList = data?.available_scopes?.length ? data.available_scopes : ["global", "home", "office", "vps"];

  return (
    <div className="space-y-4 pb-20">
      {/* 顶栏标题与操作按钮 */}
      <div className="p-card rounded-2xl p-4 sm:p-5 flex flex-col gap-4">
        <div className="flex flex-col lg:flex-row lg:items-center lg:justify-between gap-3">
          <div>
            <div className="flex items-center gap-2.5">
              <div className="w-8 h-8 rounded-xl bg-primary/10 text-primary flex items-center justify-center shrink-0">
                <KeyRound size={17} />
              </div>
              <div>
                <h2 className="text-base font-semibold z-text tracking-tight">系统配置中心</h2>
                <p className="text-xs z-text-muted mt-0.5">
                  配置 AES-256-GCM 加密入库并随数据库自动双向同步；修改即热重载，彻底告别 GitHub Secret 明文盲改
                </p>
              </div>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              onClick={() => setImportOpen(true)}
              className="px-3 py-1.5 rounded-xl text-xs font-medium z-ghost-btn border z-border z-text flex items-center gap-1.5 cursor-pointer"
            >
              <Upload size={13} /> 粘贴导入 .env
            </button>
            <button
              type="button"
              onClick={() => setExportOpen(true)}
              className="px-3 py-1.5 rounded-xl text-xs font-medium z-ghost-btn border z-border z-text flex items-center gap-1.5 cursor-pointer"
            >
              <Download size={13} /> 导出配置
            </button>
            <button
              type="button"
              onClick={() => void loadSettings(scope)}
              disabled={loading}
              className="px-3 py-1.5 rounded-xl text-xs font-medium z-ghost-btn border z-border z-text flex items-center gap-1.5 cursor-pointer disabled:opacity-50"
            >
              <RefreshCw size={13} className={loading ? "animate-spin" : ""} /> 刷新
            </button>
          </div>
        </div>

        {/* Scope 分段切换栏 */}
        <div className="flex flex-wrap items-center justify-between gap-2 pt-3 border-t z-border">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-xs font-medium z-text-muted mr-1 flex items-center gap-1">
              <Layers size={12} /> 作用域:
            </span>
            {scopesList.map((sc) => {
              const selected = scope === sc;
              const isActiveRuntime = data?.active_profile === sc;
              const friendly = SCOPE_LABELS[sc] || `环境 (${sc})`;
              return (
                <button
                  key={sc}
                  type="button"
                  onClick={() => handleSelectScope(sc)}
                  className={`px-3 py-1.5 rounded-xl text-xs font-medium transition-all flex items-center gap-1.5 cursor-pointer border ${
                    selected
                      ? "bg-primary text-primary-foreground border-primary shadow-sm"
                      : "z-surface z-text-sec z-border hover:z-text"
                  }`}
                >
                  <span>{friendly}</span>
                  {isActiveRuntime && (
                    <span
                      className={`inline-flex items-center gap-1 px-1.5 py-0.25 rounded-full text-[10px] ${
                        selected
                          ? "bg-white/20 text-white"
                          : "bg-emerald-500/12 text-emerald-600 dark:text-emerald-300"
                      }`}
                    >
                      <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" />
                      运行中
                    </span>
                  )}
                </button>
              );
            })}
          </div>

          {data?.active_profile && (
            <div className="text-xs z-text-muted flex items-center gap-1.5">
              <Server size={12} />
              <span>当前节点生效 Profile:</span>
              <code className="px-1.5 py-0.5 rounded z-muted font-mono z-text">{data.active_profile}</code>
            </div>
          )}
        </div>
      </div>

      {notice && (
        <NoticeBanner type={notice.type} text={notice.text} onClose={() => setNotice(null)} />
      )}

      {scope !== "global" && (
        <div className="p-card rounded-2xl px-4 py-3 border border-indigo-500/25 bg-indigo-500/5 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2">
          <div className="text-xs z-text">
            当前处于 <span className="font-semibold text-indigo-600 dark:text-indigo-300">{SCOPE_LABELS[scope] || scope}</span>{" "}
            环境覆盖视图：仅展示支持按运行环境差异化覆盖的代理、订阅、存储与无头浏览器参数，其余全局 API Key 自动继承 Global。
          </div>
          <button
            type="button"
            onClick={() => handleSelectScope("global")}
            className="px-2.5 py-1 rounded-lg text-xs font-medium z-ghost-btn border z-border z-text shrink-0 cursor-pointer"
          >
            切回全局默认 (Global)
          </button>
        </div>
      )}

      {/* 主体左右双栏 */}
      <div className="flex flex-col lg:flex-row gap-4 items-start">
        {/* 左侧分类导航 */}
        <aside className="w-full lg:w-64 shrink-0 p-card rounded-2xl p-2.5 space-y-1">
          <div className="px-2.5 py-1.5 text-[11px] font-medium uppercase tracking-wider z-text-muted">
            配置分类导航
          </div>
          <div className="flex lg:flex-col gap-1 overflow-x-auto lg:overflow-visible pb-1 lg:pb-0">
            {CATEGORIES.map((cat) => {
              const count = categoryCounts[cat.id];
              if (scope !== "global" && count.total === 0) {
                return null;
              }
              const active = category === cat.id && !searchText.trim();
              return (
                <button
                  key={cat.id}
                  type="button"
                  onClick={() => {
                    setCategory(cat.id);
                    if (searchText.trim()) setSearchText("");
                  }}
                  className={`shrink-0 lg:w-full flex items-center justify-between gap-2 px-3 py-2.5 rounded-xl text-left transition-all cursor-pointer border ${
                    active
                      ? "p-nav-active font-medium"
                      : "border-transparent z-text-sec z-ghost-btn"
                  }`}
                >
                  <div className="flex items-center gap-2 min-w-0">
                    {renderCategoryIcon(cat.id)}
                    <div className="min-w-0">
                      <div className="text-xs truncate">{cat.label}</div>
                    </div>
                  </div>
                  <span
                    className={`font-mono text-[11px] px-1.5 py-0.5 rounded-md shrink-0 ${
                      active ? "z-surface z-text border z-border" : "z-muted z-text-muted"
                    }`}
                  >
                    {count.configured}/{count.total}
                  </span>
                </button>
              );
            })}
          </div>
        </aside>

        {/* 右侧表单工作区 */}
        <section className="flex-1 min-w-0 w-full space-y-4">
          {/* 搜索 + 快捷过滤栏 */}
          <div className="p-card rounded-2xl p-3 flex flex-col sm:flex-row sm:items-center justify-between gap-2.5">
            <div className="relative flex-1 min-w-0">
              <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 z-text-muted" />
              <input
                type="text"
                value={searchText}
                onChange={(e) => setSearchText(e.target.value)}
                placeholder="全局模糊搜索配置名、环境变量 Key 或功能说明（跨分类直达）..."
                className="w-full p-input pl-8 pr-8 py-1.5 text-xs rounded-xl"
              />
              {searchText && (
                <button
                  type="button"
                  onClick={() => setSearchText("")}
                  title="清空搜索"
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 z-text-muted hover:z-text cursor-pointer"
                >
                  <X size={13} />
                </button>
              )}
            </div>

            <div className="flex items-center gap-1 shrink-0">
              {(
                [
                  { id: "all", label: "全部" },
                  { id: "modified", label: scope === "global" ? "仅看已配置" : "仅看已覆盖" },
                  { id: "secrets", label: "仅看敏感凭据" },
                ] as const
              ).map((f) => (
                <button
                  key={f.id}
                  type="button"
                  onClick={() => setFilterMode(f.id)}
                  className={`px-2.5 py-1.5 rounded-xl text-xs font-medium cursor-pointer border transition-all ${
                    filterMode === f.id
                      ? "bg-primary/10 text-primary border-primary/30"
                      : "z-surface z-text-sec z-border hover:z-text"
                  }`}
                >
                  {f.label}
                </button>
              ))}
            </div>
          </div>

          {/* 常用核心配置页顶部能力点亮概览 */}
          {!searchText.trim() && category === "featured" && scope === "global" && (
            <div className="p-card rounded-2xl p-4 space-y-2.5">
              <div className="flex items-center justify-between">
                <div className="text-xs font-semibold z-text flex items-center gap-1.5">
                  <Zap size={14} className="text-amber-500" />
                  核心能力就绪雷达
                </div>
                <span className="text-[11px] z-text-muted">
                  已激活 {featuredSignals.filter((s) => s.ok).length} / {featuredSignals.length} 项核心功能
                </span>
              </div>
              <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-2">
                {featuredSignals.map((sig) => (
                  <div
                    key={sig.key}
                    className={`rounded-xl px-2.5 py-2 border text-xs flex items-center gap-2 ${
                      sig.ok
                        ? "border-emerald-500/25 bg-emerald-500/5 z-text"
                        : "z-border z-surface z-text-muted"
                    }`}
                  >
                    <span
                      className={`w-2 h-2 rounded-full shrink-0 ${
                        sig.ok ? "bg-emerald-500" : "bg-amber-400/70"
                      }`}
                    />
                    <span className="truncate font-medium">{sig.label}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* 引导层分类顶部的 VPS_ENV_CONTENT 极简模板一键复制卡 */}
          {!searchText.trim() && category === "bootstrap" && data?.bootstrap_env_template && (
            <div className="p-card rounded-2xl p-4 space-y-3 border border-amber-500/25 bg-amber-500/5">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
                <div>
                  <h3 className="text-xs font-semibold z-text flex items-center gap-1.5">
                    <FileCode2 size={14} className="text-amber-500" />
                    GitHub Secret (`VPS_ENV_CONTENT`) 极简 8 行引导模板
                  </h3>
                  <p className="text-xs z-text-muted mt-0.5">
                    API Key、代理订阅与号池策略均已托管到数据库；VPS `.env` 与 GitHub Secret 今后只需维护以下连接引导变量（部署 workflow 已升级为按键智能合并，不会抹除既有变量）。
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() =>
                    void handleCopyValue(
                      "bootstrap-template",
                      data.bootstrap_env_template,
                      "VPS_ENV_CONTENT 极简引导模板"
                    )
                  }
                  className="px-3 py-1.5 rounded-xl text-xs font-medium z-primary-btn shrink-0 flex items-center gap-1.5 cursor-pointer"
                >
                  {copiedKey === "bootstrap-template" ? <Check size={13} /> : <Copy size={13} />}
                  一键复制引导模板
                </button>
              </div>
              <pre className="p-3 rounded-xl z-surface border z-border text-[11px] font-mono z-text overflow-x-auto leading-relaxed">
                {data.bootstrap_env_template}
              </pre>
            </div>
          )}

          {/* 配置分组渲染 */}
          {loading && !data ? (
            <div className="p-card rounded-2xl p-10 text-center text-xs z-text-muted">
              正在加载系统配置中心...
            </div>
          ) : groupedSections.length === 0 ? (
            <div className="p-card rounded-2xl p-10 text-center space-y-2">
              <div className="text-sm font-medium z-text">当前筛选条件下没有匹配的配置项</div>
              <div className="text-xs z-text-muted">
                请尝试清空搜索关键词、切换过滤条件，或切换到其他配置分类
              </div>
            </div>
          ) : (
            groupedSections.map((group) => (
              <div key={group.title} className="p-card rounded-2xl overflow-hidden">
                <div className="px-4 py-3 border-b z-border z-surface flex items-center justify-between">
                  <h3 className="text-xs font-semibold z-text tracking-wide">{group.title}</h3>
                  <span className="text-[11px] font-mono z-text-muted">{group.items.length} 项</span>
                </div>
                <div className="divide-y z-border">
                  {group.items.map((item) => {
                    const hasDirty = Object.prototype.hasOwnProperty.call(dirtyMap, item.key);
                    const hasRevealed = Object.prototype.hasOwnProperty.call(revealedMap, item.key);
                    const isBool = item.type === "bool";
                    const canReset =
                      !item.read_only &&
                      (scope === "global" ? item.has_global_value : item.has_scope_override);

                    // 计算控件显示值
                    const currentVal = hasDirty
                      ? dirtyMap[item.key]
                      : item.is_secret
                        ? hasRevealed
                          ? revealedMap[item.key]
                          : item.effective_value
                        : item.effective_value;

                    const boolChecked = currentVal.trim().toLowerCase() === "true";

                    // 敏感字段：如果还没存值（is_configured=false 且不含掩码），直接可输入，不强迫先点“查看”
                    const lockedSecretMask =
                      item.is_secret &&
                      !hasRevealed &&
                      !hasDirty &&
                      (item.effective_value.includes("••••") || item.is_configured);

                    return (
                      <div
                        key={item.key}
                        className={`p-4 transition-colors ${
                          hasDirty ? "bg-amber-500/5" : ""
                        }`}
                      >
                        <div className="flex flex-col lg:flex-row lg:items-start justify-between gap-3">
                          {/* 左列：名称、Key、来源标签与说明 */}
                          <div className="min-w-0 lg:w-[44%] space-y-1">
                            <div className="flex flex-wrap items-center gap-1.5">
                              <span className="text-xs font-semibold z-text">{item.label}</span>
                              {hasDirty && (
                                <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-500/15 text-amber-600 dark:text-amber-300 border border-amber-500/30">
                                  未保存
                                </span>
                              )}
                            </div>

                            <div className="flex flex-wrap items-center gap-1.5">
                              <button
                                type="button"
                                onClick={() =>
                                  void handleCopyValue(`key:${item.key}`, item.key)
                                }
                                title="点击复制环境变量 Key"
                                className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded z-muted text-[11px] font-mono z-text-sec hover:z-text cursor-pointer border z-border"
                              >
                                <span>{item.key}</span>
                                {copiedKey === `key:${item.key}` ? (
                                  <Check size={10} className="text-emerald-500" />
                                ) : (
                                  <Copy size={10} />
                                )}
                              </button>
                              {sourceBadge(item)}
                              {scope === "global" && item.profile_overridable && !item.read_only && (
                                <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] z-text-muted border z-border">
                                  支持分环境覆盖
                                </span>
                              )}
                            </div>

                            <p className="text-[11px] z-text-muted leading-relaxed pt-0.5">
                              {item.description}
                            </p>
                          </div>

                          {/* 右列：防反人类交互表单控件 */}
                          <div className="flex-1 min-w-0 space-y-1.5">
                            {isBool ? (
                              <div className="flex items-center justify-between gap-3 p-2.5 rounded-xl z-surface border z-border">
                                <div className="flex items-center gap-2.5">
                                  <button
                                    type="button"
                                    role="switch"
                                    aria-checked={boolChecked}
                                    disabled={item.read_only}
                                    onClick={() =>
                                      handleFieldChange(item, boolChecked ? "false" : "true")
                                    }
                                    className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors cursor-pointer disabled:opacity-50 ${
                                      boolChecked ? "bg-emerald-500" : "bg-zinc-300 dark:bg-zinc-700"
                                    }`}
                                  >
                                    <span
                                      className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow transition-transform ${
                                        boolChecked ? "translate-x-[18px]" : "translate-x-[3px]"
                                      }`}
                                    />
                                  </button>
                                  <span className="text-xs font-medium z-text">
                                    {boolChecked ? "已开启 (true)" : "已关闭 (false)"}
                                  </span>
                                </div>
                                {canReset && (
                                  <button
                                    type="button"
                                    disabled={resettingKey === item.key}
                                    onClick={() => void handleResetItem(item)}
                                    className="px-2 py-1 rounded-lg text-[11px] z-ghost-btn z-text-muted hover:z-text flex items-center gap-1 cursor-pointer"
                                  >
                                    <RotateCcw size={11} />
                                    {scope === "global" ? "恢复默认" : "取消覆盖"}
                                  </button>
                                )}
                              </div>
                            ) : (
                              <div className="flex flex-col sm:flex-row sm:items-center gap-2">
                                <div className="relative flex-1 min-w-0">
                                  <input
                                    type={
                                      item.is_secret && !lockedSecretMask && !plainVisible[item.key]
                                        ? "password"
                                        : "text"
                                    }
                                    readOnly={item.read_only || lockedSecretMask}
                                    value={currentVal}
                                    placeholder={
                                      item.placeholder ||
                                      (item.default_value ? `默认值: ${item.default_value}` : "未配置")
                                    }
                                    inputMode={
                                      item.type === "int"
                                        ? "numeric"
                                        : item.type === "float"
                                          ? "decimal"
                                          : undefined
                                    }
                                    onChange={(e) => handleFieldChange(item, e.target.value)}
                                    className={`w-full p-input px-3 py-2 text-xs rounded-xl font-mono ${
                                      item.read_only || lockedSecretMask
                                        ? "opacity-80 cursor-default"
                                        : ""
                                    }`}
                                  />
                                </div>

                                <div className="flex items-center gap-1.5 shrink-0">
                                  {item.is_secret && !item.read_only && (
                                    <>
                                      <button
                                        type="button"
                                        disabled={revealingKey === item.key}
                                        onClick={() => void handleRevealAndEdit(item)}
                                        className="px-2.5 py-2 rounded-xl text-[11px] font-medium z-ghost-btn border z-border z-text flex items-center gap-1 cursor-pointer disabled:opacity-50"
                                      >
                                        {plainVisible[item.key] && hasRevealed ? (
                                          <>
                                            <EyeOff size={12} /> 遮罩
                                          </>
                                        ) : (
                                          <>
                                            <Eye size={12} />
                                            {revealingKey === item.key
                                              ? "解密中..."
                                              : lockedSecretMask
                                                ? "查看/编辑明文"
                                                : "明文"}
                                          </>
                                        )}
                                      </button>
                                      {item.is_configured && (
                                        <button
                                          type="button"
                                          disabled={revealingKey === item.key}
                                          onClick={() => void handleCopySecretPlain(item)}
                                          title="直接解密并复制完整明文"
                                          className="p-2 rounded-xl text-[11px] z-ghost-btn border z-border z-text cursor-pointer disabled:opacity-50"
                                        >
                                          {copiedKey === `val:${item.key}` ? (
                                            <Check size={12} className="text-emerald-500" />
                                          ) : (
                                            <Copy size={12} />
                                          )}
                                        </button>
                                      )}
                                    </>
                                  )}

                                  {item.is_secret && item.read_only && item.is_configured && (
                                    <button
                                      type="button"
                                      disabled={revealingKey === item.key}
                                      onClick={() => void handleCopySecretPlain(item)}
                                      className="px-2.5 py-2 rounded-xl text-[11px] font-medium z-ghost-btn border z-border z-text flex items-center gap-1 cursor-pointer"
                                    >
                                      <Copy size={12} /> 复制
                                    </button>
                                  )}

                                  {canReset && (
                                    <button
                                      type="button"
                                      disabled={resettingKey === item.key}
                                      onClick={() => void handleResetItem(item)}
                                      title={
                                        scope === "global"
                                          ? "清除 DB 配置并回退为 .env 或默认值"
                                          : `移除 ${scope} 环境覆盖并继承全局`
                                      }
                                      className="px-2.5 py-2 rounded-xl text-[11px] font-medium z-ghost-btn border z-border z-text-muted hover:z-text flex items-center gap-1 cursor-pointer disabled:opacity-50"
                                    >
                                      <RotateCcw size={12} />
                                      {scope === "global" ? "默认" : "继承"}
                                    </button>
                                  )}
                                </div>
                              </div>
                            )}

                            {/* 辅助元信息：Profile 下提示 Global 当前值，或显示更新人/时间 */}
                            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] z-text-muted">
                              {item.default_value && (
                                <span>
                                  默认: <code className="font-mono">{item.default_value}</code>
                                </span>
                              )}
                              {scope !== "global" && item.has_global_value && (
                                <span>
                                  Global 继承值:{" "}
                                  <code className="font-mono">{item.global_value || "(空)"}</code>
                                </span>
                              )}
                              {item.updated_by && (
                                <span>
                                  最近修改: <code className="font-mono">{item.updated_by}</code>
                                </span>
                              )}
                            </div>
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            ))
          )}
        </section>
      </div>

      {/* 底部悬浮未保存更改 Dock */}
      {isDirty && (
        <div className="fixed bottom-4 left-1/2 -translate-x-1/2 z-40 w-[calc(100%-2rem)] max-w-3xl">
          <div className="p-card rounded-2xl px-4 py-3 border-2 border-primary/40 shadow-xl flex flex-col sm:flex-row sm:items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-2 min-w-0">
              <span className="inline-flex items-center px-2 py-0.5 rounded-lg text-xs font-semibold bg-amber-500/15 text-amber-600 dark:text-amber-300">
                有 {dirtyKeys.length} 项未保存更改
              </span>
              <div className="flex flex-wrap items-center gap-1">
                {dirtyKeys.slice(0, 3).map((k) => (
                  <code key={k} className="px-1.5 py-0.5 rounded z-muted text-[11px] font-mono z-text-sec">
                    {k}
                  </code>
                ))}
                {dirtyKeys.length > 3 && (
                  <span className="text-[11px] z-text-muted">+{dirtyKeys.length - 3} 项</span>
                )}
              </div>
            </div>

            <div className="flex items-center gap-2 shrink-0">
              <button
                type="button"
                onClick={() => setDirtyMap({})}
                disabled={saving}
                className="px-3 py-1.5 rounded-xl text-xs font-medium z-ghost-btn border z-border z-text-sec cursor-pointer"
              >
                放弃修改
              </button>
              <button
                type="button"
                onClick={() => void handleSave()}
                disabled={saving}
                className="px-4 py-1.5 rounded-xl text-xs font-semibold z-primary-btn flex items-center gap-1.5 cursor-pointer disabled:opacity-50"
              >
                <Save size={13} />
                {saving ? "保存并重载中..." : "保存并立即热重载 (Ctrl+S)"}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 批量粘贴导入 .env 弹窗 */}
      {importOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-xs p-4">
          <div className="p-card rounded-2xl w-full max-w-xl p-5 space-y-4 shadow-2xl">
            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-sm font-semibold z-text">批量粘贴导入 .env 配置</h3>
                <p className="text-xs z-text-muted mt-0.5">
                  将原 `.env` 片段粘贴到下方（自动忽略 `#` 注释与引号；只读引导层变量会被安全跳过），保存至{" "}
                  <code className="font-mono font-semibold">{scope}</code> 并热重载
                </p>
              </div>
              <button
                type="button"
                onClick={() => setImportOpen(false)}
                className="p-1.5 rounded-lg z-ghost-btn z-text-muted cursor-pointer"
              >
                <X size={15} />
              </button>
            </div>

            <textarea
              rows={9}
              value={importText}
              onChange={(e) => setImportText(e.target.value)}
              placeholder={`# 示例：\nSILICONFLOW_API_KEY=sk-...\nDASHSCOPE_API_KEY=sk-...\nPOOL_CHATGPT_FREE_ACTIVE_TARGET=15\nCLASH_SUB_URL=https://...`}
              className="w-full p-input p-3 rounded-xl text-xs font-mono leading-relaxed"
            />

            <div className="flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => setImportOpen(false)}
                className="px-3.5 py-1.5 rounded-xl text-xs font-medium z-ghost-btn border z-border z-text cursor-pointer"
              >
                取消
              </button>
              <button
                type="button"
                onClick={() => void handleDotEnvImport()}
                disabled={saving || !importText.trim()}
                className="px-4 py-1.5 rounded-xl text-xs font-semibold z-primary-btn cursor-pointer disabled:opacity-50"
              >
                {saving ? "导入中..." : "解析并写入配置中心"}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 导出配置弹窗 */}
      {exportOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-xs p-4">
          <div className="p-card rounded-2xl w-full max-w-2xl p-5 space-y-4 shadow-2xl">
            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-sm font-semibold z-text">导出系统配置 ({scope})</h3>
                <p className="text-xs z-text-muted mt-0.5">
                  可导出为分类注释的 `.env` 备份或 `JSON` 快照，默认对敏感密钥自动脱敏
                </p>
              </div>
              <button
                type="button"
                onClick={() => setExportOpen(false)}
                className="p-1.5 rounded-lg z-ghost-btn z-text-muted cursor-pointer"
              >
                <X size={15} />
              </button>
            </div>

            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center gap-1.5">
                {(["dotenv", "json"] as const).map((fmt) => (
                  <button
                    key={fmt}
                    type="button"
                    onClick={() => setExportFormat(fmt)}
                    className={`px-3 py-1.5 rounded-xl text-xs font-medium border cursor-pointer ${
                      exportFormat === fmt
                        ? "bg-primary text-primary-foreground border-primary"
                        : "z-surface z-border z-text-sec"
                    }`}
                  >
                    {fmt === "dotenv" ? ".env 格式" : "JSON 格式"}
                  </button>
                ))}
              </div>

              <label className="flex items-center gap-2 text-xs z-text cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={exportIncludeSecrets}
                  onChange={(e) => setExportIncludeSecrets(e.target.checked)}
                  className="rounded border z-border"
                />
                包含真实明文密钥
              </label>
            </div>

            <pre className="p-3 rounded-xl z-surface border z-border text-[11px] font-mono z-text max-h-80 overflow-auto leading-relaxed">
              {exportLoading ? "正在生成导出内容..." : exportContent || "(当前无已配置项)"}
            </pre>

            <div className="flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => setExportOpen(false)}
                className="px-3.5 py-1.5 rounded-xl text-xs font-medium z-ghost-btn border z-border z-text cursor-pointer"
              >
                关闭
              </button>
              <button
                type="button"
                onClick={() => void handleCopyValue("export-content", exportContent, "导出配置内容")}
                disabled={!exportContent}
                className="px-3.5 py-1.5 rounded-xl text-xs font-medium z-ghost-btn border z-border z-text flex items-center gap-1.5 cursor-pointer disabled:opacity-50"
              >
                <Copy size={13} /> 复制内容
              </button>
              <button
                type="button"
                onClick={() =>
                  triggerTextDownload(
                    exportFormat === "json"
                      ? `ai-web-free-settings-${scope}.json`
                      : `ai-web-free-settings-${scope}.env`,
                    exportContent
                  )
                }
                disabled={!exportContent}
                className="px-4 py-1.5 rounded-xl text-xs font-semibold z-primary-btn flex items-center gap-1.5 cursor-pointer disabled:opacity-50"
              >
                <Download size={13} /> 下载文件
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
