import React, { useEffect, useMemo, useState } from "react";
import {
  Check,
  Copy,
  Eye,
  KeyRound,
  Layers,
  Plus,
  RefreshCw,
  Search,
  Shield,
  Trash2,
  Zap,
} from "lucide-react";
import {
  createGatewayGroup,
  createGatewayKey,
  deleteGatewayGroup,
  deleteGatewayKey,
  fetchAccounts,
  fetchGatewayGroups,
  fetchGatewayKeys,
  revealGatewayKeySecret,
  updateGatewayGroup,
  updateGatewayKey,
  type AccountView,
  type GatewayGroup,
  type GatewayKey,
} from "../api/client";
import { useModelsCatalog } from "../lib/models";
import {
  formatClockTime,
  formatRelativeTimeAgo,
  NoticeBanner,
  StatusBadge,
} from "../components/ui";

export const GatewayView: React.FC = () => {
  const [activeTab, setActiveTab] = useState<"keys" | "groups" | "sdk">("keys");
  const [keys, setKeys] = useState<GatewayKey[]>([]);
  const [groups, setGroups] = useState<GatewayGroup[]>([]);
  const [accounts, setAccounts] = useState<AccountView[]>([]);
  const [loading, setLoading] = useState(false);
  const [notice, setNotice] = useState<{
    type: "info" | "success" | "error";
    text: string;
  } | null>(null);

  // Key 创建与复制状态
  const [showCreateKeyModal, setShowCreateKeyModal] = useState(false);
  const [newKeyName, setNewKeyName] = useState("");
  const [newKeyGroupId, setNewKeyGroupId] = useState("");
  const [newKeyRpm, setNewKeyRpm] = useState(60);
  const [newKeyMaxConc, setNewKeyMaxConc] = useState(5);
  const [createdPlainKey, setCreatedPlainKey] = useState<string | null>(null);
  const [revealedSecrets, setRevealedSecrets] = useState<Record<string, string>>({});
  const [revealingKeyId, setRevealingKeyId] = useState<string | null>(null);
  const [copiedKeyId, setCopiedKeyId] = useState<string | null>(null);
  const [importingKeyId, setImportingKeyId] = useState<string | null>(null);

  // Group 创建/编辑状态
  const [showGroupModal, setShowGroupModal] = useState(false);
  const [editingGroup, setEditingGroup] = useState<GatewayGroup | null>(null);
  const [groupName, setGroupName] = useState("");
  const [groupProvider, setGroupProvider] = useState("chatgpt");
  const [groupTypes, setGroupTypes] = useState<string[]>(["free"]);
  const [groupAccountIds, setGroupAccountIds] = useState<string[]>([]);
  const [groupAllowlistText, setGroupAllowlistText] = useState("");
  const [groupAliasesText, setGroupAliasesText] = useState("");
  const [groupFallbackId, setGroupFallbackId] = useState("");
  const [groupEnabled, setGroupEnabled] = useState(true);
  const [groupModelSearch, setGroupModelSearch] = useState("");
  const modelsCatalog = useModelsCatalog();

  // SDK 代码生成状态
  const [sdkKeyId, setSdkKeyId] = useState("");
  const [sdkModel, setSdkModel] = useState("");
  const [sdkLang, setSdkLang] = useState<"curl" | "python" | "node">("curl");
  const [copiedCode, setCopiedCode] = useState(false);

  // 搜索过滤
  const [keyFilter, setKeyFilter] = useState("");
  const [groupFilter, setGroupFilter] = useState("");

  const loadData = async () => {
    setLoading(true);
    try {
      const [kRes, gRes, accRes] = await Promise.all([
        fetchGatewayKeys().catch(() => ({ keys: [] })),
        fetchGatewayGroups().catch(() => ({ groups: [] })),
        fetchAccounts("chatgpt", "", "", "").catch(() => ({ accounts: [] })),
      ]);
      setKeys(kRes.keys || []);
      setGroups(gRes.groups || []);
      setAccounts(accRes.accounts || []);
      if (!newKeyGroupId && (gRes.groups || []).length > 0) {
        setNewKeyGroupId(gRes.groups[0].id);
      }
      if (!sdkKeyId && (kRes.keys || []).length > 0) {
        setSdkKeyId(kRes.keys[0].id);
      }
    } catch (err: any) {
      setNotice({
        type: "error",
        text: err?.message || "加载网关数据失败",
      });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, []);

  useEffect(() => {
    if (!sdkModel && modelsCatalog.chatModels.length > 0) {
      setSdkModel(modelsCatalog.chatModels[0].id);
    }
  }, [modelsCatalog.chatModels, sdkModel]);

  const openCreateGroup = () => {
    setEditingGroup(null);
    setGroupName("");
    setGroupProvider("chatgpt");
    setGroupTypes(["free"]);
    setGroupAccountIds([]);
    setGroupAllowlistText("");
    setGroupAliasesText("");
    setGroupFallbackId("");
    setGroupEnabled(true);
    setShowGroupModal(true);
  };

  const openEditGroup = (g: GatewayGroup) => {
    setEditingGroup(g);
    setGroupName(g.name);
    setGroupProvider(g.provider || "chatgpt");
    setGroupTypes(g.account_types?.length ? g.account_types : ["free"]);
    setGroupAccountIds(g.account_ids || []);
    setGroupAllowlistText((g.model_allowlist || []).join("\n"));
    const aliasLines = Object.entries(g.model_aliases || {}).map(
      ([k, v]) => `${k}=${v}`
    );
    setGroupAliasesText(aliasLines.join("\n"));
    setGroupFallbackId(g.fallback_group_id || "");
    setGroupEnabled(g.enabled);
    setShowGroupModal(true);
  };

  const handleSaveGroup = async () => {
    if (!groupName.trim()) {
      setNotice({ type: "error", text: "分组名称不能为空" });
      return;
    }
    const allowlist = groupAllowlistText
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean);
    const aliases: Record<string, string> = {};
    for (const line of groupAliasesText.split("\n")) {
      const parts = line.split("=");
      if (parts.length === 2 && parts[0].trim() && parts[1].trim()) {
        aliases[parts[0].trim()] = parts[1].trim();
      }
    }

    try {
      const payload: Partial<GatewayGroup> = {
        name: groupName.trim(),
        provider: groupProvider,
        account_types: groupTypes,
        account_ids: groupAccountIds,
        model_allowlist: allowlist,
        model_aliases: aliases,
        fallback_group_id: groupFallbackId || undefined,
        enabled: groupEnabled,
      };
      if (editingGroup) {
        await updateGatewayGroup(editingGroup.id, payload);
        setNotice({ type: "success", text: `分组 ${groupName} 已更新` });
      } else {
        await createGatewayGroup(payload);
        setNotice({ type: "success", text: `分组 ${groupName} 创建成功` });
      }
      setShowGroupModal(false);
      await loadData();
    } catch (err: any) {
      setNotice({ type: "error", text: err?.message || "保存分组失败" });
    }
  };

  const handleDeleteGroup = async (g: GatewayGroup) => {
    if (!window.confirm(`确定要删除分组 "${g.name}" 吗？关联该分组的 Key 将无法继续使用。`)) {
      return;
    }
    try {
      await deleteGatewayGroup(g.id);
      setNotice({ type: "success", text: `分组 ${g.name} 已删除` });
      await loadData();
    } catch (err: any) {
      setNotice({ type: "error", text: err?.message || "删除分组失败" });
    }
  };

  const handleCreateKey = async () => {
    if (!newKeyName.trim() || !newKeyGroupId) {
      setNotice({ type: "error", text: "请填写 Key 名称并选择所属分组" });
      return;
    }
    try {
      const res = await createGatewayKey({
        name: newKeyName.trim(),
        group_id: newKeyGroupId,
        rpm: Number(newKeyRpm) || 0,
        max_concurrent: Number(newKeyMaxConc) || 0,
      });
      setCreatedPlainKey(res.key);
      setNotice({ type: "success", text: `Key "${newKeyName}" 生成成功！请及时保存` });
      setNewKeyName("");
      await loadData();
    } catch (err: any) {
      setNotice({ type: "error", text: err?.message || "创建 Key 失败" });
    }
  };

  const handleToggleKey = async (k: GatewayKey) => {
    try {
      await updateGatewayKey(k.id, { enabled: !k.enabled });
      setNotice({
        type: "success",
        text: `Key "${k.name}" 已${!k.enabled ? "启用" : "禁用"}`,
      });
      await loadData();
    } catch (err: any) {
      setNotice({ type: "error", text: err?.message || "操作失败" });
    }
  };

  const handleDeleteKey = async (k: GatewayKey) => {
    if (!window.confirm(`确定要彻底删除 API Key "${k.name}" (${k.prefix}) 吗？`)) {
      return;
    }
    try {
      await deleteGatewayKey(k.id);
      setNotice({ type: "success", text: `Key "${k.name}" 已删除` });
      await loadData();
    } catch (err: any) {
      setNotice({ type: "error", text: err?.message || "删除 Key 失败" });
    }
  };

  const handleRevealSecret = async (k: GatewayKey) => {
    if (revealedSecrets[k.id]) {
      // 已经解密则直接复制
      navigator.clipboard.writeText(revealedSecrets[k.id]);
      setCopiedKeyId(k.id);
      setTimeout(() => setCopiedKeyId(null), 2000);
      return;
    }
    setRevealingKeyId(k.id);
    try {
      const secret = await revealGatewayKeySecret(k.id);
      setRevealedSecrets((prev) => ({ ...prev, [k.id]: secret }));
      navigator.clipboard.writeText(secret);
      setCopiedKeyId(k.id);
      setTimeout(() => setCopiedKeyId(null), 2000);
    } catch (err: any) {
      setNotice({
        type: "error",
        text: err?.message || "解密 Key 明文失败，请确认服务端配置了 ADMIN_TOKEN 或 GATEWAY_KEY_SECRET",
      });
    } finally {
      setRevealingKeyId(null);
    }
  };

  const handleImportToCCSwitch = async (k: GatewayKey) => {
    if (!k.enabled) {
      setNotice({ type: "error", text: "该 Key 已禁用，请先启用后再导入 CC Switch" });
      return;
    }

    setImportingKeyId(k.id);
    try {
      const secret = revealedSecrets[k.id] || (await revealGatewayKeySecret(k.id));
      setRevealedSecrets((prev) => ({ ...prev, [k.id]: secret }));
      openCCSwitchImport(k, secret);
    } catch (err: any) {
      const message = String(err?.message || "");
      setNotice({
        type: "error",
        text: message.includes("cannot be revealed")
          ? "这是旧版 Key，创建时未保存明文，无法重复显示；请新建一次 Key"
          : message || "生成 CC Switch 导入链接失败，请确认 Key 可解密",
      });
    } finally {
      setImportingKeyId(null);
    }
  };

  const openCCSwitchImport = (k: GatewayKey, secret: string) => {
    const group = groupMap.get(k.group_id);
    const params = new URLSearchParams({
      resource: "provider",
      app: "codex",
      name: `AI Web Free · ${k.name}`,
      endpoint: `${window.location.origin}/v1`,
      apiKey: secret,
      apiFormat: "openai_responses",
      homepage: window.location.origin,
      notes: [
        group ? `分组：${group.name}` : "分组：未关联",
        `RPM：${k.rpm > 0 ? k.rpm : "无限制"}`,
        `并发：${k.max_concurrent > 0 ? k.max_concurrent : "无限制"}`,
      ].join(" / "),
    });
    const model = group?.model_allowlist?.[0] || sdkModel;
    if (model) params.set("model", model);

    window.location.href = `ccswitch://v1/import?${params.toString()}`;
    setNotice({
      type: "info",
      text: "已请求打开 CC Switch；请在 CC Switch 确认框中核对信息并点击导入。若未打开，请先安装 CC Switch。",
    });
  };

  const groupMap = useMemo(() => {
    const map = new Map<string, GatewayGroup>();
    for (const g of groups) map.set(g.id, g);
    return map;
  }, [groups]);

  const filteredKeys = useMemo(() => {
    if (!keyFilter.trim()) return keys;
    const q = keyFilter.trim().toLowerCase();
    return keys.filter(
      (k) =>
        k.name.toLowerCase().includes(q) ||
        k.prefix.toLowerCase().includes(q) ||
        groupMap.get(k.group_id)?.name.toLowerCase().includes(q)
    );
  }, [keys, keyFilter, groupMap]);

  const filteredGroups = useMemo(() => {
    if (!groupFilter.trim()) return groups;
    const q = groupFilter.trim().toLowerCase();
    return groups.filter(
      (g) =>
        g.name.toLowerCase().includes(q) ||
        g.provider.toLowerCase().includes(q) ||
        (g.model_allowlist || []).some((m) => m.toLowerCase().includes(q))
    );
  }, [groups, groupFilter]);

  const selectedAllowlistModels = useMemo(
    () =>
      groupAllowlistText
        .split("\n")
        .map((item) => item.trim())
        .filter(Boolean),
    [groupAllowlistText]
  );

  const selectableGroupModels = useMemo(() => {
    const query = groupModelSearch.trim().toLowerCase();
    const source = modelsCatalog.chatModels.map((item) => item.id);
    if (!query) return source;
    return source.filter((model) => model.toLowerCase().includes(query));
  }, [modelsCatalog.chatModels, groupModelSearch]);

  const toggleGroupModel = (model: string) => {
    const selected = new Set(selectedAllowlistModels);
    if (selected.has(model)) {
      selected.delete(model);
    } else {
      selected.add(model);
    }
    setGroupAllowlistText([...selected].join("\n"));
  };

  const selectedGroupModelSet = useMemo(
    () => new Set(selectedAllowlistModels),
    [selectedAllowlistModels]
  );

  // 生成 SDK 代码
  const sdkSnippet = useMemo(() => {
    const origin = window.location.origin;
    const selectedKey = keys.find((k) => k.id === sdkKeyId);
    const keyStr = selectedKey
      ? revealedSecrets[selectedKey.id] || selectedKey.prefix
      : "sk-gw-xxxxxxxxxxxx";

    if (sdkLang === "curl") {
      return `curl ${origin}/v1/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer ${keyStr}" \\
  -d '{
    "model": "${sdkModel}",
    "messages": [
      {"role": "user", "content": "你好，请介绍一下你自己"}
    ],
    "stream": false
  }'`;
    }

    if (sdkLang === "python") {
      return `from openai import OpenAI

client = OpenAI(
    api_key="${keyStr}",
    base_url="${origin}/v1"
)

response = client.chat.completions.create(
    model="${sdkModel}",
    messages=[{"role": "user", "content": "你好，请介绍一下你自己"}],
    stream=True
)

for chunk in response:
    content = chunk.choices[0].delta.content or ""
    print(content, end="", flush=True)
`;
    }

    return `import OpenAI from "openai";

const client = new OpenAI({
  apiKey: "${keyStr}",
  baseURL: "${origin}/v1",
});

async function main() {
  const stream = await client.chat.completions.create({
    model: "${sdkModel}",
    messages: [{ role: "user", content: "你好，请介绍一下你自己" }],
    stream: true,
  });

  for await (const chunk of stream) {
    process.stdout.write(chunk.choices[0]?.delta?.content || "");
  }
}

main().catch(console.error);`;
  }, [sdkLang, sdkKeyId, sdkModel, keys, revealedSecrets]);

  return (
    <div className="space-y-6">
      {/* 头部标题与 Tab */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
        <div>
          <div className="flex items-center gap-2.5">
            <KeyRound size={22} className="z-text" />
            <h1 className="text-xl font-semibold tracking-tight">网关密钥与路由分组</h1>
            <span className="px-2 py-0.5 rounded-full border z-pill-cyan text-xs font-mono-num">
              Sub2API 架构
            </span>
          </div>
          <p className="text-sm z-text-muted mt-1">
            为各个独立 Agent 或客户端分配专用 API Key（sk-gw-*），绑定分级号池分组与模型白名单
          </p>
        </div>

        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={loadData}
            disabled={loading}
            className="btn-ghost flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border cursor-pointer disabled:opacity-50"
          >
            <RefreshCw size={14} className={loading ? "animate-spin" : ""} />
            <span>刷新</span>
          </button>
          {activeTab === "keys" && (
            <button
              type="button"
              onClick={() => {
                setCreatedPlainKey(null);
                setShowCreateKeyModal(true);
              }}
              className="btn-primary flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl cursor-pointer"
            >
              <Plus size={15} />
              <span>新建网关 Key</span>
            </button>
          )}
          {activeTab === "groups" && (
            <button
              type="button"
              onClick={openCreateGroup}
              className="btn-primary flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl cursor-pointer"
            >
              <Plus size={15} />
              <span>新建路由分组</span>
            </button>
          )}
        </div>
      </div>

      {notice && (
        <NoticeBanner
          type={notice.type}
          text={notice.text}
          onClose={() => setNotice(null)}
        />
      )}

      {/* 选项卡导航 */}
      <div className="flex border-b z-border gap-6">
        <button
          type="button"
          onClick={() => setActiveTab("keys")}
          className={`pb-2.5 text-sm font-medium border-b-2 cursor-pointer transition-colors flex items-center gap-2 ${
            activeTab === "keys"
              ? "border-current z-text"
              : "border-transparent z-text-muted hover:z-text"
          }`}
        >
          <KeyRound size={15} />
          <span>API 客户端密钥</span>
          <span className="px-1.5 py-0.2 rounded-full text-xs z-muted font-mono-num">
            {keys.length}
          </span>
        </button>

        <button
          type="button"
          onClick={() => setActiveTab("groups")}
          className={`pb-2.5 text-sm font-medium border-b-2 cursor-pointer transition-colors flex items-center gap-2 ${
            activeTab === "groups"
              ? "border-current z-text"
              : "border-transparent z-text-muted hover:z-text"
          }`}
        >
          <Layers size={15} />
          <span>号池路由分组</span>
          <span className="px-1.5 py-0.2 rounded-full text-xs z-muted font-mono-num">
            {groups.length}
          </span>
        </button>

        <button
          type="button"
          onClick={() => setActiveTab("sdk")}
          className={`pb-2.5 text-sm font-medium border-b-2 cursor-pointer transition-colors flex items-center gap-2 ${
            activeTab === "sdk"
              ? "border-current z-text"
              : "border-transparent z-text-muted hover:z-text"
          }`}
        >
          <Zap size={15} />
          <span>接入指南与代码示例</span>
        </button>
      </div>

      {/* ─── TAB 1: API KEYS ─────────────────────────────────── */}
      {activeTab === "keys" && (
        <div className="space-y-4">
          <div className="flex items-center justify-between gap-3">
            <div className="relative flex-1 max-w-sm">
              <Search
                size={14}
                className="absolute left-3 top-1/2 -translate-y-1/2 z-text-muted"
              />
              <input
                type="text"
                value={keyFilter}
                onChange={(e) => setKeyFilter(e.target.value)}
                placeholder="搜索 Key 名称、前缀或分组..."
                className="w-full pl-8 pr-3 py-1.5 text-sm rounded-xl p-input focus:outline-none"
              />
            </div>
            <div className="text-xs z-text-muted">
              共 {filteredKeys.length} 个密钥 · 凭据采用 AES-GCM 安全加密存储
            </div>
          </div>

          <div className="overflow-x-auto rounded-xl border z-border">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b z-border z-muted z-text-muted text-xs">
                  <th className="py-2.5 px-4 font-medium">名称 / 客户端</th>
                  <th className="py-2.5 px-4 font-medium">Key 前缀</th>
                  <th className="py-2.5 px-4 font-medium">绑定分组</th>
                  <th className="py-2.5 px-4 font-medium">限额配置</th>
                  <th className="py-2.5 px-4 font-medium">调用统计 (成功/总计)</th>
                  <th className="py-2.5 px-4 font-medium">状态</th>
                  <th className="py-2.5 px-4 font-medium text-right">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y z-border">
                {filteredKeys.length === 0 ? (
                  <tr>
                    <td colSpan={7} className="py-8 text-center z-text-muted">
                      暂无 API 密钥，请点击右上角「新建网关 Key」
                    </td>
                  </tr>
                ) : (
                  filteredKeys.map((k) => {
                    const g = groupMap.get(k.group_id);
                    const isRevealed = !!revealedSecrets[k.id];
                    const isCopied = copiedKeyId === k.id;
                    return (
                      <tr key={k.id} className="hover:z-hover transition-colors">
                        <td className="py-3 px-4 font-medium">
                          <div>{k.name}</div>
                          <div className="text-xs z-text-muted font-mono-num">
                            创建于 {formatRelativeTimeAgo(k.created_at)}
                          </div>
                        </td>
                        <td className="py-3 px-4">
                          <div className="flex items-center gap-1.5 font-mono text-xs">
                            <span className="px-2 py-0.5 rounded border z-border z-surface">
                              {isRevealed ? revealedSecrets[k.id] : k.prefix}
                            </span>
                            <button
                              type="button"
                              onClick={() => handleRevealSecret(k)}
                              disabled={revealingKeyId === k.id}
                              className="p-1 rounded hover:z-muted cursor-pointer z-text-sec"
                              title={isRevealed ? "复制完整 Key" : "解密并复制完整 Key"}
                            >
                              {isCopied ? (
                                <Check size={14} className="text-emerald-500" />
                              ) : isRevealed ? (
                                <Copy size={14} />
                              ) : (
                                <Eye size={14} />
                              )}
                            </button>
                          </div>
                        </td>
                        <td className="py-3 px-4">
                          {g ? (
                            <span className="px-2 py-0.5 rounded-full border z-border z-muted text-xs">
                              {g.name}
                            </span>
                          ) : (
                            <span className="text-xs z-text-muted">未关联分组</span>
                          )}
                        </td>
                        <td className="py-3 px-4 font-mono text-xs z-text-sec">
                          <div>RPM: {k.rpm > 0 ? k.rpm : "无限制"}</div>
                          <div>并发: {k.max_concurrent > 0 ? k.max_concurrent : "无限制"}</div>
                        </td>
                        <td className="py-3 px-4 font-mono-num text-xs">
                          <div>
                            {k.total_requests - k.failed_requests} / {k.total_requests} 次
                          </div>
                          {k.last_used_at && (
                            <div className="z-text-muted">
                              活跃: {formatRelativeTimeAgo(k.last_used_at)}
                            </div>
                          )}
                        </td>
                        <td className="py-3 px-4">
                          <StatusBadge status={k.enabled ? "active" : "disabled"} />
                        </td>
                        <td className="py-3 px-4 text-right">
                          <div className="flex flex-col items-end gap-1.5">
                            <button
                              type="button"
                              onClick={() => handleImportToCCSwitch(k)}
                              disabled={!k.enabled || importingKeyId === k.id}
                              className="px-2 py-1 rounded-lg border z-border z-surface text-[11px] z-text-sec hover:z-hover cursor-pointer disabled:cursor-not-allowed disabled:opacity-50 whitespace-nowrap"
                              title="使用该 Key 唤起 CC Switch，导入后仍需在 CC Switch 中确认"
                            >
                              {importingKeyId === k.id ? "唤起中..." : "导入 CC Switch"}
                            </button>
                            <button
                              type="button"
                              onClick={() => handleToggleKey(k)}
                              className="px-2 py-1 rounded-lg border z-border z-surface text-[11px] z-text-sec hover:z-hover cursor-pointer whitespace-nowrap"
                            >
                              {k.enabled ? "禁用" : "启用"}
                            </button>
                            <button
                              type="button"
                              onClick={() => handleDeleteKey(k)}
                              className="px-2 py-1 rounded-lg border border-rose-500/30 text-[11px] text-rose-500 hover:bg-rose-500/10 cursor-pointer whitespace-nowrap"
                            >
                              删除
                            </button>
                          </div>
                        </td>
                      </tr>
                    );
                  })
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* ─── TAB 2: GROUPS ───────────────────────────────────── */}
      {activeTab === "groups" && (
        <div className="space-y-4">
          <div className="flex items-center justify-between gap-3">
            <div className="relative flex-1 max-w-sm">
              <Search
                size={14}
                className="absolute left-3 top-1/2 -translate-y-1/2 z-text-muted"
              />
              <input
                type="text"
                value={groupFilter}
                onChange={(e) => setGroupFilter(e.target.value)}
                placeholder="搜索分组名称或白名单模型..."
                className="w-full pl-8 pr-3 py-1.5 text-sm rounded-xl p-input focus:outline-none"
              />
            </div>
            <div className="text-xs z-text-muted">共 {filteredGroups.length} 个路由分组</div>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {filteredGroups.length === 0 ? (
              <div className="col-span-full py-8 text-center z-text-muted border z-border rounded-xl">
                暂无路由分组，请点击右上角「新建路由分组」
              </div>
            ) : (
              filteredGroups.map((g) => {
                const fallbackG = g.fallback_group_id
                  ? groupMap.get(g.fallback_group_id)
                  : null;
                const boundKeyCount = keys.filter((k) => k.group_id === g.id).length;

                return (
                  <div
                    key={g.id}
                    className="p-4 rounded-xl border z-border space-y-3 flex flex-col justify-between"
                    style={{ background: "var(--bg-input)" }}
                  >
                    <div>
                      <div className="flex items-center justify-between gap-2">
                        <div className="font-semibold text-base flex items-center gap-1.5">
                          <Layers size={16} className="z-text-muted" />
                          <span>{g.name}</span>
                        </div>
                        <StatusBadge status={g.enabled ? "active" : "disabled"} />
                      </div>

                      <div className="mt-3 space-y-1.5 text-xs">
                        <div className="flex items-center justify-between">
                          <span className="z-text-muted">账号匹配规则：</span>
                          <span className="font-mono">
                            {g.account_ids?.length
                              ? `指定 ${g.account_ids.length} 个账号`
                              : `类型: ${(g.account_types || ["free"]).join(", ")}`}
                          </span>
                        </div>

                        <div className="flex items-center justify-between">
                          <span className="z-text-muted">模型白名单：</span>
                          <span className="font-mono truncate max-w-[160px]" title={(g.model_allowlist || []).join(", ")}>
                            {g.model_allowlist?.length
                              ? g.model_allowlist.join(", ")
                              : "全部放行"}
                          </span>
                        </div>

                        {g.model_aliases && Object.keys(g.model_aliases).length > 0 && (
                          <div className="flex items-center justify-between">
                            <span className="z-text-muted">模型映射：</span>
                            <span className="font-mono text-xs z-text-sec">
                              {Object.keys(g.model_aliases).length} 条别名
                            </span>
                          </div>
                        )}

                        <div className="flex items-center justify-between">
                          <span className="z-text-muted">降级回退分组：</span>
                          <span>{fallbackG ? fallbackG.name : "无"}</span>
                        </div>

                        <div className="flex items-center justify-between">
                          <span className="z-text-muted">当前绑定 Key：</span>
                          <span className="font-mono-num">{boundKeyCount} 个客户端</span>
                        </div>
                      </div>
                    </div>

                    <div className="pt-3 border-t z-border flex items-center justify-end gap-3 text-xs">
                      <button
                        type="button"
                        onClick={() => openEditGroup(g)}
                        className="z-text-sec hover:underline cursor-pointer"
                      >
                        编辑分组
                      </button>
                      <button
                        type="button"
                        onClick={() => handleDeleteGroup(g)}
                        className="text-rose-500 hover:underline cursor-pointer"
                      >
                        删除
                      </button>
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>
      )}

      {/* ─── TAB 3: SDK & PLAYGROUND ─────────────────────────── */}
      {activeTab === "sdk" && (
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          <div className="p-4 rounded-xl border z-border space-y-4" style={{ background: "var(--bg-input)" }}>
            <h2 className="font-semibold text-sm">参数配置</h2>
            <div className="space-y-3 text-xs">
              <div>
                <label className="font-medium z-text-sec block mb-1">选择网关 Key</label>
                <select
                  value={sdkKeyId}
                  onChange={(e) => setSdkKeyId(e.target.value)}
                  className="w-full p-2 rounded-lg p-input"
                >
                  {keys.map((k) => (
                    <option key={k.id} value={k.id}>
                      {k.name} ({k.prefix}) - {groupMap.get(k.group_id)?.name || "默认组"}
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label className="font-medium z-text-sec block mb-1">请求模型</label>
                <input
                  type="text"
                  value={sdkModel}
                  onChange={(e) => setSdkModel(e.target.value)}
                  list="gateway-live-models"
                  placeholder="请选择实时模型"
                  className="w-full p-2 rounded-lg p-input font-mono"
                />
                <datalist id="gateway-live-models">
                  {modelsCatalog.chatModels.map((model) => (
                    <option key={model.id} value={model.id} />
                  ))}
                </datalist>
              </div>

              <div>
                <label className="font-medium z-text-sec block mb-1">接口规范</label>
                <div className="p-2.5 rounded-lg border z-border space-y-1 text-xs z-text-muted">
                  <div>Base URL: <code className="font-mono text-xs">{window.location.origin}/v1</code></div>
                  <div>Chat: <code className="font-mono text-xs">POST /v1/chat/completions</code></div>
                  <div>Models: <code className="font-mono text-xs">GET /v1/models</code></div>
                </div>
              </div>
            </div>
          </div>

          <div className="lg:col-span-2 p-4 rounded-xl border z-border space-y-3" style={{ background: "var(--bg-input)" }}>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setSdkLang("curl")}
                  className={`px-3 py-1 rounded-lg text-xs font-mono cursor-pointer ${
                    sdkLang === "curl" ? "z-surface font-semibold border z-border" : "z-text-muted"
                  }`}
                >
                  cURL
                </button>
                <button
                  type="button"
                  onClick={() => setSdkLang("python")}
                  className={`px-3 py-1 rounded-lg text-xs font-mono cursor-pointer ${
                    sdkLang === "python" ? "z-surface font-semibold border z-border" : "z-text-muted"
                  }`}
                >
                  Python (OpenAI SDK)
                </button>
                <button
                  type="button"
                  onClick={() => setSdkLang("node")}
                  className={`px-3 py-1 rounded-lg text-xs font-mono cursor-pointer ${
                    sdkLang === "node" ? "z-surface font-semibold border z-border" : "z-text-muted"
                  }`}
                >
                  Node.js (OpenAI SDK)
                </button>
              </div>

              <button
                type="button"
                onClick={() => {
                  navigator.clipboard.writeText(sdkSnippet);
                  setCopiedCode(true);
                  setTimeout(() => setCopiedCode(false), 2000);
                }}
                className="btn-ghost flex items-center gap-1.5 px-2.5 py-1 text-xs rounded-lg border z-border cursor-pointer"
              >
                {copiedCode ? <Check size={13} className="text-emerald-500" /> : <Copy size={13} />}
                <span>{copiedCode ? "已复制" : "复制"}</span>
              </button>
            </div>

            <pre className="p-3.5 rounded-xl border z-border overflow-x-auto text-xs font-mono bg-zinc-950 text-zinc-100 leading-relaxed">
              {sdkSnippet}
            </pre>
          </div>
        </div>
      )}

      {/* ─── MODAL: 新建 Key ─────────────────────────────────── */}
      {showCreateKeyModal && (
        <div className="fixed inset-0 z-50 bg-black/50 backdrop-blur-xs flex items-center justify-center p-4">
          <div className="w-full max-w-md p-5 rounded-2xl p-card shadow-xl space-y-4">
            <h3 className="text-base font-semibold flex items-center gap-2">
              <KeyRound size={18} />
              <span>新建网关 API Key</span>
            </h3>

            {createdPlainKey ? (
              <div className="space-y-3">
                <div className="p-3 rounded-xl border border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 text-xs space-y-1">
                  <div className="font-semibold">密钥已生成，请立即复制保存：</div>
                  <div className="font-mono text-sm break-all select-all font-bold">
                    {createdPlainKey}
                  </div>
                </div>
                <div className="flex justify-end gap-2">
                  <button
                    type="button"
                    onClick={() => {
                      navigator.clipboard.writeText(createdPlainKey);
                      setNotice({ type: "success", text: "已复制明文密钥" });
                      setShowCreateKeyModal(false);
                    }}
                    className="btn-primary px-4 py-1.5 rounded-xl text-sm cursor-pointer"
                  >
                    复制并关闭
                  </button>
                </div>
              </div>
            ) : (
              <div className="space-y-3 text-xs">
                <div>
                  <label className="font-medium block mb-1">Key 名称 (例如 Agent 标识或项目名)</label>
                  <input
                    type="text"
                    value={newKeyName}
                    onChange={(e) => setNewKeyName(e.target.value)}
                    placeholder="my-agent-key"
                    className="w-full p-2 rounded-lg p-input text-sm"
                  />
                </div>

                <div>
                  <label className="font-medium block mb-1">绑定号池分组</label>
                  <select
                    value={newKeyGroupId}
                    onChange={(e) => setNewKeyGroupId(e.target.value)}
                    className="w-full p-2 rounded-lg p-input text-sm"
                  >
                    {groups.map((g) => (
                      <option key={g.id} value={g.id}>
                        {g.name} ({g.provider})
                      </option>
                    ))}
                  </select>
                </div>

                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="font-medium block mb-1">RPM 限制 (每分钟请求数)</label>
                    <input
                      type="number"
                      value={newKeyRpm}
                      onChange={(e) => setNewKeyRpm(Number(e.target.value))}
                      placeholder="0 表示不限制"
                      className="w-full p-2 rounded-lg p-input text-sm font-mono-num"
                    />
                  </div>
                  <div>
                    <label className="font-medium block mb-1">最大并发请求数</label>
                    <input
                      type="number"
                      value={newKeyMaxConc}
                      onChange={(e) => setNewKeyMaxConc(Number(e.target.value))}
                      placeholder="0 表示不限制"
                      className="w-full p-2 rounded-lg p-input text-sm font-mono-num"
                    />
                  </div>
                </div>

                <div className="pt-2 flex justify-end gap-2">
                  <button
                    type="button"
                    onClick={() => setShowCreateKeyModal(false)}
                    className="btn-ghost px-3.5 py-1.5 rounded-xl text-sm border z-border cursor-pointer"
                  >
                    取消
                  </button>
                  <button
                    type="button"
                    onClick={handleCreateKey}
                    className="btn-primary px-4 py-1.5 rounded-xl text-sm cursor-pointer"
                  >
                    确认生成
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* ─── MODAL: 新建 / 编辑分组 ─────────────────────────── */}
      {showGroupModal && (
        <div className="fixed inset-0 z-50 bg-black/50 backdrop-blur-xs flex items-center justify-center p-4">
          <div className="w-full max-w-lg p-5 rounded-2xl p-card shadow-xl space-y-4 max-h-[90vh] overflow-y-auto">
            <h3 className="text-base font-semibold flex items-center gap-2">
              <Layers size={18} />
              <span>{editingGroup ? "编辑号池路由分组" : "新建号池路由分组"}</span>
            </h3>

            <div className="space-y-3 text-xs">
              <div>
                <label className="font-medium block mb-1">分组名称</label>
                <input
                  type="text"
                  value={groupName}
                  onChange={(e) => setGroupName(e.target.value)}
                  placeholder="例如: free-chat-group"
                  className="w-full p-2 rounded-lg p-input text-sm"
                />
              </div>

              <div>
                <label className="font-medium block mb-1">供应商</label>
                <select
                  value={groupProvider}
                  onChange={(e) => setGroupProvider(e.target.value)}
                  className="w-full p-2 rounded-lg p-input text-sm"
                >
                  <option value="chatgpt">ChatGPT (Web 逆向号池)</option>
                </select>
              </div>

              <div>
                <label className="font-medium block mb-1">匹配账号类型 (当未显式指定账号列表时生效)</label>
                <div className="flex items-center gap-3">
                  {["free", "puid", "noauth"].map((t) => (
                    <label key={t} className="flex items-center gap-1.5 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={groupTypes.includes(t)}
                        onChange={(e) => {
                          if (e.target.checked) {
                            setGroupTypes([...groupTypes, t]);
                          } else {
                            setGroupTypes(groupTypes.filter((x) => x !== t));
                          }
                        }}
                      />
                      <span className="font-mono">{t}</span>
                    </label>
                  ))}
                </div>
              </div>

              <div>
                <label className="font-medium block mb-1">
                  模型白名单 (来自实时目录，留空表示透传号池所有模型)
                </label>
                <div className="flex items-center gap-2 mb-2">
                  <div className="relative flex-1">
                    <Search
                      size={13}
                      className="absolute left-2.5 top-1/2 -translate-y-1/2 z-text-muted pointer-events-none"
                    />
                    <input
                      type="search"
                      value={groupModelSearch}
                      onChange={(e) => setGroupModelSearch(e.target.value)}
                      placeholder="搜索实时模型"
                      className="w-full pl-7 pr-2 py-1.5 rounded-lg p-input"
                    />
                  </div>
                  <button
                    type="button"
                    onClick={() => void modelsCatalog.refresh()}
                    disabled={modelsCatalog.loading}
                    className="flex items-center gap-1 px-2 py-1.5 rounded-lg border z-border z-text-sec hover:z-hover cursor-pointer disabled:opacity-60 whitespace-nowrap"
                  >
                    <RefreshCw
                      size={13}
                      className={modelsCatalog.loading ? "animate-spin" : ""}
                    />
                    刷新
                  </button>
                </div>

                <div className="mb-2 max-h-44 overflow-y-auto rounded-lg border z-border divide-y z-border">
                  {modelsCatalog.loading && selectableGroupModels.length === 0 ? (
                    <div className="px-2.5 py-3 z-text-muted">正在拉取实时模型...</div>
                  ) : selectableGroupModels.length > 0 ? (
                    selectableGroupModels.map((model) => (
                      <label
                        key={model}
                        className="flex items-center gap-2 px-2.5 py-1.5 cursor-pointer hover:z-hover"
                      >
                        <input
                          type="checkbox"
                          checked={selectedGroupModelSet.has(model)}
                          onChange={() => toggleGroupModel(model)}
                        />
                        <span className="font-mono truncate" title={model}>
                          {model}
                        </span>
                      </label>
                    ))
                  ) : (
                    <div className="px-2.5 py-3 z-text-muted">
                      {modelsCatalog.warning ||
                        modelsCatalog.error ||
                        "尚未拉取到实时模型，请点击刷新"}
                    </div>
                  )}
                </div>

                <div className="flex items-center justify-between mb-2 z-text-muted">
                  <span>已选 {selectedAllowlistModels.length} 个模型</span>
                  <span>手动输入的模型仍会保留</span>
                </div>

                <textarea
                  rows={3}
                  value={groupAllowlistText}
                  onChange={(e) => setGroupAllowlistText(e.target.value)}
                  placeholder={"每行一个模型 ID"}
                  className="w-full p-2 rounded-lg p-input font-mono text-xs"
                />
              </div>

              <div>
                <label className="font-medium block mb-1">
                  模型别名重定向映射 (每行一条 请求模型=实际模型)
                </label>
                <textarea
                  rows={2}
                  value={groupAliasesText}
                  onChange={(e) => setGroupAliasesText(e.target.value)}
                  placeholder={"gpt-4o=gpt-5-mini\ngpt-3.5-turbo=auto"}
                  className="w-full p-2 rounded-lg p-input font-mono text-xs"
                />
              </div>

              <div>
                <label className="font-medium block mb-1">降级回退分组 (当本组号池耗尽或限流时自动回退)</label>
                <select
                  value={groupFallbackId}
                  onChange={(e) => setGroupFallbackId(e.target.value)}
                  className="w-full p-2 rounded-lg p-input text-sm"
                >
                  <option value="">无降级回退</option>
                  {groups
                    .filter((g) => !editingGroup || g.id !== editingGroup.id)
                    .map((g) => (
                      <option key={g.id} value={g.id}>
                        {g.name}
                      </option>
                    ))}
                </select>
              </div>

              <div className="pt-2 flex justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setShowGroupModal(false)}
                  className="btn-ghost px-3.5 py-1.5 rounded-xl text-sm border z-border cursor-pointer"
                >
                  取消
                </button>
                <button
                  type="button"
                  onClick={handleSaveGroup}
                  className="btn-primary px-4 py-1.5 rounded-xl text-sm cursor-pointer"
                >
                  保存分组
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
