import React, { useEffect, useMemo, useState } from "react";
import {
  Activity,
  Check,
  Copy,
  Download,
  LogIn,
  Plus,
  RefreshCw,
  Search,
  X,
} from "lucide-react";
import {
  executeAccountAction,
  bulkUpdateAccountProxy,
  fetchAccounts,
  fetchKamiLoginTask,
  fetchOverview,
  fetchProxies,
  importAccounts,
  loginKamiAccounts,
  probeAllAccounts,
  type AccountActionType,
  type AccountView,
  type KamiWorkerStats,
  type KamiLoginTask,
  type PoolStats,
  type ProviderSummary,
  type ProxyNodeStatus,
} from "../api/client";
import {
  formatRemainingCountdown,
  NoticeBanner,
  StatusBadge,
  TotpRingModal,
} from "../components/ui";

const IMPORT_PLACEHOLDERS: Record<string, string> = {
  kami: "每行一条卡密，格式: email----password----2FA_secret\n例如: user@example.com----DemoPass123----JBSWY3DPEHPK3PXP",
  sub2api_json:
    '粘贴 Sub2API 标准导出 JSON（包含 "type": "sub2api-data" 与 "accounts" 数组）',
  access_token: "每行一条 Access Token (eyJ...)，可选携带 account_id",
  refresh_token: "每行一条 Refresh Token (rt_...)",
  session_token: "每行一条 __Secure-next-auth.session-token",
  free_uuid: "留空则按下方数量自动生成 UUID 访客设备，或每行粘贴一个 UUID",
};

function getAvatarInitial(acc: AccountView): string {
  const src = (acc.email || acc.chatgpt_account_id || acc.id || "A").trim();
  const ch = src.charAt(0).toUpperCase();
  return /[A-Z0-9]/.test(ch) ? ch : "A";
}

function proxyOptionLabel(node: ProxyNodeStatus, index: number): string {
  let endpoint = `代理节点 ${index + 1}`;
  try {
    endpoint = new URL(node.url).host || endpoint;
  } catch {
    // Keep credentials out of the option label when the URL is malformed.
  }
  return `${endpoint} · ${node.status === "active" ? "可用" : "冷却"} · ${node.bound_accounts} 个账号`;
}

function getTtlBar(days?: number): { pct: number; color: string } {
  if (days === undefined) return { pct: 100, color: "var(--text-muted)" };
  if (days <= 0) return { pct: 4, color: "#ef4444" };
  if (days < 2.5)
    return {
      pct: Math.max(12, Math.min(100, (days / 10) * 100)),
      color: "#f59e0b",
    };
  return {
    pct: Math.max(18, Math.min(100, (days / 10) * 100)),
    color: "#10b981",
  };
}

export const PoolConsole: React.FC<{
  onOpenExport: (provider: string) => void;
}> = ({ onOpenExport }) => {
  const [providers, setProviders] = useState<ProviderSummary[]>([]);
  const [activeProvider, setActiveProvider] = useState("chatgpt");
  const [stats, setStats] = useState<PoolStats | null>(null);
  const [accounts, setAccounts] = useState<AccountView[]>([]);
  const [workerStats, setWorkerStats] = useState<KamiWorkerStats | null>(null);

  const [statusFilter, setStatusFilter] = useState("");
  const [typeFilter, setTypeFilter] = useState("");
  const [search, setSearch] = useState("");
  const [loading, setLoading] = useState(false);
  const [probingAll, setProbingAll] = useState(false);
  const [loggingInKami, setLoggingInKami] = useState(false);
  const [loginTaskId, setLoginTaskId] = useState("");
  const [loginTaskProvider, setLoginTaskProvider] = useState("");
  const [loginTask, setLoginTask] = useState<KamiLoginTask | null>(null);
  const [updatingProxy, setUpdatingProxy] = useState(false);
  const [headlessLogin, setHeadlessLogin] = useState(true);
  const [proxyNodes, setProxyNodes] = useState<ProxyNodeStatus[]>([]);
  const [bulkProxyUrl, setBulkProxyUrl] = useState("");
  const [selectedAccountIds, setSelectedAccountIds] = useState<Set<string>>(
    () => new Set()
  );
  const [actionLoadingKey, setActionLoadingKey] = useState<string | null>(null);
  const [notice, setNotice] = useState<{
    type: "info" | "success" | "error";
    text: string;
  } | null>(null);
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [nowMs, setNowMs] = useState(() => Date.now());

  const [showImport, setShowImport] = useState(false);
  const [showBulkEdit, setShowBulkEdit] = useState(false);
  const [importFormat, setImportFormat] = useState("kami");
  const [importText, setImportText] = useState("");
  const [generateCount, setGenerateCount] = useState(10);
  const [importing, setImporting] = useState(false);
  const importCredentialCount = importText
    .split(/\r?\n/)
    .filter((line) => line.trim().length > 0).length;

  const [totpInfo, setTotpInfo] = useState<{
    accountId: string;
    email: string;
    code: string;
    remainingSeconds: number;
  } | null>(null);
  const [copiedTotp, setCopiedTotp] = useState(false);

  const [deleteTarget, setDeleteTarget] = useState<AccountView | null>(null);

  const activeProviderSummary = useMemo(
    () => providers.find((p) => p.descriptor.id === activeProvider) || null,
    [providers, activeProvider]
  );

  const loadData = async () => {
    setLoading(true);
    try {
      const ov = await fetchOverview();
      setProviders(ov.providers || []);
      setWorkerStats(ov.kami_worker || null);

      const targetSummary = (ov.providers || []).find(
        (p) => p.descriptor.id === activeProvider
      );
      if (targetSummary?.descriptor.status === "enabled") {
        const backendStatus =
          statusFilter === "image_limited" || statusFilter === "renewing"
            ? ""
            : statusFilter;
        const accRes = await fetchAccounts(
          activeProvider,
          backendStatus,
          typeFilter,
          ""
        );
        setStats(accRes.stats);
        setAccounts(accRes.accounts || []);
      } else {
        setStats(targetSummary?.stats || null);
        setAccounts([]);
      }
      window.dispatchEvent(new Event("pool-updated"));
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `加载数据失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, [activeProvider, statusFilter, typeFilter]);

  useEffect(() => {
    setSelectedAccountIds(new Set());
  }, [activeProvider, statusFilter, typeFilter]);

  useEffect(() => {
    let cancelled = false;
    fetchProxies()
      .then((res) => {
        if (cancelled) return;
        setProxyNodes(res.nodes || []);
        setBulkProxyUrl(
          (current) => current || res.nodes.find((node) => node.status === "active")?.url || ""
        );
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!loginTaskId || !loginTaskProvider) return;
    let cancelled = false;
    let timer = 0;
    let lastUpdatedAt = "";

    const poll = async () => {
      try {
        const { task } = await fetchKamiLoginTask(loginTaskProvider, loginTaskId);
        if (cancelled) return;
        setLoginTask(task);
        if (task.updated_at !== lastUpdatedAt) {
          lastUpdatedAt = task.updated_at;
          if (loginTaskProvider === activeProvider) {
            const backendStatus =
              statusFilter === "image_limited" || statusFilter === "renewing"
                ? ""
                : statusFilter;
            const accRes = await fetchAccounts(
              activeProvider,
              backendStatus,
              typeFilter,
              ""
            );
            if (!cancelled) {
              setStats(accRes.stats);
              setAccounts(accRes.accounts || []);
            }
          }
        }
        if (task.status === "completed" || task.status === "failed") {
          const failures = task.results.filter((item) => !item.ok);
          const examples = failures
            .slice(0, 3)
            .map((item) => `${item.email || item.account_id}: ${item.error || "登录失败"}`);
          setNotice({
            type: task.failed > 0 ? "info" : "success",
            text: `后台登录任务完成：${task.success} 成功，${task.failed} 失败${examples.length ? `；${examples.join("；")}${failures.length > examples.length ? `；另有 ${failures.length - examples.length} 项失败，详见链路日志` : ""}` : ""}`,
          });
          setSelectedAccountIds(new Set());
          setLoginTaskId("");
          setLoggingInKami(false);
          return;
        }
        timer = window.setTimeout(poll, 1500);
      } catch {
        if (!cancelled) timer = window.setTimeout(poll, 3000);
      }
    };

    void poll();
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [loginTaskId, loginTaskProvider, activeProvider, statusFilter, typeFilter]);

  useEffect(() => {
    const timer = window.setInterval(() => setNowMs(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (!totpInfo) return;
    const timer = window.setInterval(() => {
      setTotpInfo((prev) => {
        if (!prev) return null;
        if (prev.remainingSeconds <= 1) {
          executeAccountAction(activeProvider, prev.accountId, "totp")
            .then((res) => {
              if (res.result?.totp_code) {
                setTotpInfo({
                  accountId: prev.accountId,
                  email: res.result.email || prev.email,
                  code: res.result.totp_code,
                  remainingSeconds: res.result.remaining_seconds ?? 30,
                });
              }
            })
            .catch(() => {});
          return { ...prev, remainingSeconds: 0 };
        }
        return {
          ...prev,
          remainingSeconds: prev.remainingSeconds - 1,
        };
      });
    }, 1000);
    return () => window.clearInterval(timer);
  }, [totpInfo !== null, activeProvider]);

  const filteredAccounts = useMemo(() => {
    const q = search.trim().toLowerCase();
    return accounts.filter((a) => {
      if (statusFilter === "image_limited" && !a.image_limited) return false;
      if (
        statusFilter === "renewing" &&
        a.renew_state !== "queued" &&
        a.renew_state !== "running"
      ) {
        return false;
      }
      if (!q) return true;
      return (
        a.id.toLowerCase().includes(q) ||
        (a.email || "").toLowerCase().includes(q) ||
        (a.chatgpt_account_id || "").toLowerCase().includes(q) ||
        (a.proxy || "").toLowerCase().includes(q)
      );
    });
  }, [accounts, search, statusFilter]);

  const handleCopyText = async (key: string, text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopiedId(key);
      window.setTimeout(() => setCopiedId(null), 1500);
    } catch {
      // ignore
    }
  };

  const handleAction = async (acc: AccountView, action: AccountActionType) => {
    setActionLoadingKey(`${acc.id}:${action}`);
    try {
      const res = await executeAccountAction(activeProvider, acc.id, action);
      if (action === "totp" && res.result?.totp_code) {
        setTotpInfo({
          accountId: acc.id,
          email: res.result.email || acc.email || acc.id,
          code: res.result.totp_code,
          remainingSeconds: res.result.remaining_seconds ?? 30,
        });
        setCopiedTotp(false);
      } else {
        const msg =
          res.result?.message ||
          (action === "probe"
            ? res.result?.ok
              ? `账号 ${acc.email || acc.id} 测活通过 (${res.result?.latency_ms ?? 0}ms, 可用模型 ${res.result?.models_count ?? 0} 个)`
              : `账号 ${acc.email || acc.id} 测活异常: ${res.result?.error || "未知错误"}`
            : `操作 ${action} 执行成功`);
        setNotice({
          type:
            action === "probe" && res.result?.ok === false ? "error" : "success",
          text: msg,
        });
      }
      await loadData();
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `操作失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setActionLoadingKey(null);
    }
  };

  const handleProbeAll = async () => {
    setProbingAll(true);
    setNotice({ type: "info", text: "正在并发测活池内全部可用账号..." });
    try {
      const res = await probeAllAccounts(activeProvider);
      setNotice({
        type: res.failed > 0 ? "info" : "success",
        text: `全池测活完成：共探测 ${res.probed} 个账号，正常 ${res.passed} 个，异常 ${res.failed} 个`,
      });
      await loadData();
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `全池测活失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setProbingAll(false);
    }
  };

  const handleLoginKami = async () => {
    setLoggingInKami(true);
    setNotice({
      type: "info",
      text: "正在创建待激活账号后台登录任务...",
    });
    try {
      const res = await loginKamiAccounts(activeProvider, headlessLogin, false);
      setLoginTask(res.task);
      setLoginTaskProvider(activeProvider);
      setLoginTaskId(res.task.id);
      setNotice({ type: "info", text: `后台登录任务已启动：${res.task.total} 个待激活账号。` });
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `批量登录失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setLoggingInKami(false);
    }
  };

  const eligibleVisibleAccounts = filteredAccounts.filter(
    (acc) => acc.has_kami_credentials && acc.status !== "disabled"
  );
  const selectedCount = selectedAccountIds.size;
  const loginTaskActive = Boolean(loginTaskId);
  const proxyUnavailable =
    proxyNodes.length > 0 && !proxyNodes.some((node) => node.status === "active");
  const allVisibleSelected =
    filteredAccounts.length > 0 &&
    filteredAccounts.every((acc) => selectedAccountIds.has(acc.id));

  const toggleAccountSelection = (id: string) => {
    if (selectedAccountIds.has(id)) {
      setSelectedAccountIds((current) => {
        const next = new Set(current);
        next.delete(id);
        return next;
      });
      return;
    }
    setSelectedAccountIds((current) => new Set(current).add(id));
  };

  const selectVisibleKamiAccounts = () => {
    const next = new Set(selectedAccountIds);
    for (const acc of eligibleVisibleAccounts.filter((item) => item.status !== "active")) {
      next.add(acc.id);
    }
    setSelectedAccountIds(next);
  };

  const selectVisibleAccounts = () => {
    const next = new Set(selectedAccountIds);
    for (const acc of filteredAccounts) {
      next.add(acc.id);
    }
    setSelectedAccountIds(next);
  };

  const toggleVisibleSelection = () => {
    setSelectedAccountIds((current) => {
      const next = new Set(current);
      if (allVisibleSelected) {
        filteredAccounts.forEach((acc) => next.delete(acc.id));
      } else {
        filteredAccounts.forEach((acc) => next.add(acc.id));
      }
      return next;
    });
  };

  const handleLoginSelectedKami = async () => {
    const ids = [...selectedAccountIds];
    if (!ids.length) return;
    setLoggingInKami(true);
    setNotice({
      type: "info",
      text: `正在为 ${ids.length} 个账号创建后台登录任务，账号间间隔 10 秒...`,
    });
    try {
      const res = await loginKamiAccounts(activeProvider, headlessLogin, false, ids);
      setLoginTask(res.task);
      setLoginTaskProvider(activeProvider);
      setLoginTaskId(res.task.id);
      setNotice({ type: "info", text: `后台登录任务已启动：${res.task.total} 个账号已排队。` });
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `批量重登失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setLoggingInKami(false);
    }
  };

  const handleBulkProxyUpdate = async () => {
    const ids = [...selectedAccountIds];
    if (!ids.length) return;
    setUpdatingProxy(true);
    try {
      const res = await bulkUpdateAccountProxy(activeProvider, ids, bulkProxyUrl);
      const failures = res.result.results
        .filter((item) => !item.ok)
        .map((item) => `${item.email || item.id}: ${item.error || "更新失败"}`);
      setNotice({
        type: res.result.failed > 0 ? "info" : "success",
        text: `代理更新完成：${res.result.updated}/${res.result.total} 个账号已重建客户端${failures.length ? `；${failures.join("；")}` : ""}`,
      });
      setShowBulkEdit(false);
      await loadData();
      const proxyRes = await fetchProxies();
      setProxyNodes(proxyRes.nodes || []);
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `批量代理更新失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setUpdatingProxy(false);
    }
  };

  const handleImportSubmit = async () => {
    const trimmed = importText.trim();
    if (importFormat !== "free_uuid" && !trimmed) {
      setNotice({ type: "error", text: "请粘贴要导入的账号凭据内容" });
      return;
    }
    setImporting(true);
    try {
      const res = await importAccounts({
        provider: activeProvider,
        credentialType: importFormat,
        rawText: trimmed,
        persist: true,
        autoLogin: false,
        headless: headlessLogin,
        generateCount:
          importFormat === "free_uuid" && !trimmed ? generateCount : 0,
      });
      const r = res.result;
      const extraMsg = r.messages?.length ? `（${r.messages.join("；")}）` : "";
      setNotice({
        type: r.failed > 0 && r.added === 0 ? "error" : "success",
        text: `导入完成：新增 ${r.added} 个，跳过重复 ${r.skipped} 个，失败 ${r.failed} 个${extraMsg}`,
      });
      setShowImport(false);
      setImportText("");
      await loadData();
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `导入失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setImporting(false);
    }
  };

  const isPlannedProvider =
    activeProviderSummary && activeProviderSummary.descriptor.status !== "enabled";

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">
            号池与供应商管理
          </h1>
          <p className="text-xs z-text-muted mt-1">
            分级隔离全局 429 与生图专池冷却、粘性代理指纹绑定、实时 2FA TOTP 与 Sub2API 脱敏导出。
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2 text-xs">
          <button
            type="button"
            onClick={handleProbeAll}
            disabled={probingAll || Boolean(isPlannedProvider)}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
          >
            <Activity size={13} />
            <span>{probingAll ? "测活中..." : "全池测活"}</span>
          </button>
          <button
            type="button"
            onClick={handleLoginKami}
            disabled={loggingInKami || loginTaskActive || proxyUnavailable || Boolean(isPlannedProvider)}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
          >
            <LogIn size={13} />
            <span>{loggingInKami ? "创建任务..." : "待激活批量登录"}</span>
          </button>
          <label className="inline-flex items-center gap-1.5 px-2 py-1.5 border z-border rounded-xl z-text-sec">
            <input
              type="checkbox"
              checked={headlessLogin}
              onChange={(e) => setHeadlessLogin(e.target.checked)}
              disabled={loggingInKami || updatingProxy}
            />
            无头模式
          </label>
          <button
            type="button"
            onClick={() => onOpenExport(activeProvider)}
            disabled={Boolean(isPlannedProvider)}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
          >
            <Download size={13} />
            <span>导出号池</span>
          </button>
          <button
            type="button"
            onClick={() => setShowImport(true)}
            disabled={Boolean(isPlannedProvider)}
            className="inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl z-primary-btn font-medium cursor-pointer"
          >
            <Plus size={13} />
            <span>导入卡密 / JSON</span>
          </button>
        </div>
      </div>

      {proxyUnavailable && (
        <div className="border-l-2 border-amber-500 px-3 py-2 text-xs text-amber-800" role="status">
          当前代理池没有健康节点，批量登录已暂停。请先测活代理或更新代理列表。
        </div>
      )}

      {/* Provider Tabs + Kami Queue */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-b z-border text-xs">
        <div className="flex flex-wrap items-center gap-4">
          {providers.map((p) => {
            const active = p.descriptor.id === activeProvider;
            const enabled = p.descriptor.status === "enabled";
            return (
              <button
                key={p.descriptor.id}
                type="button"
                onClick={() => setActiveProvider(p.descriptor.id)}
                className={`pb-2.5 border-b-2 transition-colors inline-flex items-center gap-1.5 cursor-pointer ${
                  active
                    ? "border-current font-semibold z-text"
                    : "border-transparent z-text-muted hover:opacity-80"
                }`}
              >
                <span>{p.descriptor.name}</span>
                <span className="font-mono-num px-1.5 py-0.25 rounded-md z-muted">
                  {enabled ? p.stats.total : "Planned"}
                </span>
              </button>
            );
          })}
          {providers.length === 0 && (
            <button
              type="button"
              className="pb-2.5 border-b-2 border-current font-semibold"
            >
              ChatGPT Web
            </button>
          )}
        </div>

        {workerStats && (workerStats.running || workerStats.queue_length > 0) && (
          <div className="mb-1.5 inline-flex items-center gap-2 px-2.5 py-1 rounded-full border z-pill-info font-mono-num">
            <span>
              串行续期队列：{workerStats.queue_length} 排队
              {workerStats.running && workerStats.running_email
                ? ` · 正在登录 ${workerStats.running_email}`
                : ""}
            </span>
          </div>
        )}
      </div>

      {notice && (
        <NoticeBanner
          type={notice.type}
          text={notice.text}
          onClose={() => setNotice(null)}
        />
      )}

      {loginTaskActive && loginTask && (
        <div className="p-card rounded-xl px-3.5 py-3 space-y-2 text-xs" role="status" aria-live="polite">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="font-medium">
              后台登录任务：{loginTask.completed}/{loginTask.total}
            </span>
            <span className="z-text-muted">
              {loginTask.current_email ? `正在处理 ${loginTask.current_email}` : "等待执行"}
              {` · ${loginTask.success} 成功 · ${loginTask.failed} 失败`}
            </span>
          </div>
          <progress
            className="w-full h-1.5 accent-emerald-600"
            max={Math.max(loginTask.total, 1)}
            value={loginTask.completed}
            aria-label="批量登录进度"
          />
        </div>
      )}

      {isPlannedProvider && activeProviderSummary ? (
        <div className="p-card rounded-2xl p-6 text-xs space-y-2">
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold">
              {activeProviderSummary.descriptor.name}
            </span>
            <span className="px-2 py-0.5 rounded-full border z-pill-info">
              架构预留槽位 (Planned)
            </span>
          </div>
          <p className="z-text-muted leading-relaxed">
            {activeProviderSummary.descriptor.description}
          </p>
        </div>
      ) : (
        <>
          {/* Condensed KPI Strip */}
          {stats && (
            <div className="grid grid-cols-2 sm:grid-cols-5 gap-2.5 text-xs">
              <div className="p-card rounded-xl px-3.5 py-2.5">
                <div className="z-text-muted">可用 / 总容量</div>
                <div className="text-base font-semibold font-mono-num mt-0.5">
                  {stats.active} / {stats.total}
                </div>
              </div>
              <div className="p-card rounded-xl px-3.5 py-2.5">
                <div className="z-text-muted">仅生图冷却</div>
                <div className="text-base font-semibold font-mono-num mt-0.5">
                  {stats.image_limited ?? 0}
                </div>
              </div>
              <div className="p-card rounded-xl px-3.5 py-2.5">
                <div className="z-text-muted">全局 429 冷却</div>
                <div className="text-base font-semibold font-mono-num mt-0.5">
                  {stats.rate_limited}
                </div>
              </div>
              <div className="p-card rounded-xl px-3.5 py-2.5">
                <div className="z-text-muted">待激活 / 过期</div>
                <div className="text-base font-semibold font-mono-num mt-0.5">
                  {stats.pending + stats.expired}
                </div>
              </div>
              <div className="p-card rounded-xl px-3.5 py-2.5">
                <div className="z-text-muted">累计调用 / 失败</div>
                <div className="text-base font-semibold font-mono-num mt-0.5">
                  {stats.total_calls} / {stats.failed_calls}
                </div>
              </div>
            </div>
          )}

          {/* Filter & Search Bar */}
          <div className="flex flex-wrap items-center justify-between gap-2.5 text-xs">
            <div className="flex flex-wrap items-center gap-2">
              <div className="p-surface p-1 rounded-xl inline-flex flex-wrap gap-0.5">
                {[
                  { k: "", label: "全部" },
                  { k: "active", label: "在线" },
                  { k: "image_limited", label: "生图冷却" },
                  { k: "rate_limited", label: "全局429" },
                  { k: "renewing", label: "续期队列" },
                  { k: "expired", label: "已过期" },
                  { k: "pending", label: "待激活" },
                ].map((opt) => (
                  <button
                    key={opt.k}
                    type="button"
                    onClick={() => setStatusFilter(opt.k)}
                    className={`px-2.5 py-1 rounded-lg cursor-pointer transition-colors ${
                      statusFilter === opt.k
                        ? "z-primary-btn font-medium"
                        : "z-text-muted z-ghost-btn"
                    }`}
                  >
                    {opt.label}
                  </button>
                ))}
              </div>

              <div className="p-surface p-1 rounded-xl inline-flex gap-0.5">
                {[
                  { k: "", label: "全部类型" },
                  { k: "free", label: "Free" },
                  { k: "noauth", label: "NoAuth" },
                  { k: "puid", label: "PUID" },
                ].map((t) => (
                  <button
                    key={t.k}
                    type="button"
                    onClick={() => setTypeFilter(t.k)}
                    className={`px-2 py-1 rounded-lg cursor-pointer transition-colors ${
                      typeFilter === t.k
                        ? "z-primary-btn font-medium"
                        : "z-text-muted z-ghost-btn"
                    }`}
                  >
                    {t.label}
                  </button>
                ))}
              </div>

              <button
                type="button"
                onClick={selectVisibleKamiAccounts}
                disabled={
                  eligibleVisibleAccounts.every((item) => item.status === "active") ||
                  loginTaskActive
                }
                className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn cursor-pointer disabled:opacity-50"
              >
                选择待激活账号
              </button>
              <button
                type="button"
                onClick={selectVisibleAccounts}
                disabled={filteredAccounts.length === 0 || loginTaskActive}
                className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn cursor-pointer disabled:opacity-50"
              >
                全量选择
              </button>
              {selectedCount > 0 && (
                <>
                  <span className="z-text-muted">已选 {selectedCount}</span>
                  <button
                    type="button"
                    onClick={() => setSelectedAccountIds(new Set())}
                    disabled={loggingInKami || loginTaskActive || updatingProxy}
                    className="px-2 py-1.5 rounded-xl z-text-muted z-ghost-btn cursor-pointer"
                  >
                    清除选择
                  </button>
                </>
              )}
            </div>

            <div className="flex items-center gap-2">
              <div className="relative">
                <Search
                  size={13}
                  className="absolute left-3 top-2.5 z-text-muted pointer-events-none"
                />
                <input
                  type="text"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="搜索邮箱 / 代理节点..."
                  className="pl-8 pr-3 py-1.5 rounded-xl p-input w-56"
                />
              </div>

              <span className="z-text-muted font-mono-num hidden sm:inline">
                {filteredAccounts.length} 个
              </span>

              <button
                type="button"
                onClick={loadData}
                disabled={loading}
                className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
              >
                <RefreshCw
                  size={12}
                  className={loading ? "animate-spin" : ""}
                />
                <span>刷新</span>
              </button>
            </div>
          </div>

          {selectedCount > 0 && (
            <div className="flex flex-wrap items-center justify-between gap-2.5 p-card rounded-xl px-3 py-2.5 text-xs">
              <span className="z-text-muted">已选择 {selectedCount} 个账号</span>
              <div className="flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  onClick={() => setShowBulkEdit(true)}
                  disabled={updatingProxy || loggingInKami || loginTaskActive || Boolean(isPlannedProvider)}
                  className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn cursor-pointer disabled:opacity-50"
                >
                  批量编辑
                </button>
                <button
                  type="button"
                  onClick={handleLoginSelectedKami}
                  disabled={
                    loggingInKami ||
                    updatingProxy ||
                    loginTaskActive ||
                    proxyUnavailable ||
                    Boolean(isPlannedProvider) ||
                    selectedCount === 0
                  }
                  className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl z-primary-btn font-medium cursor-pointer disabled:opacity-50"
                >
                  <LogIn size={13} />
                  <span>
                    {loggingInKami
                      ? "登录中..."
                      : "批量重新登录"}
                  </span>
                </button>
              </div>
            </div>
          )}

          {/* Linear / Supabase Style Precision Account Table */}
          <div className="p-card rounded-2xl overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full text-left border-collapse text-xs">
                <thead>
                  <tr
                    className="border-b z-border z-text-muted"
                    style={{ background: "var(--bg-input)" }}
                  >
                    <th className="py-3 px-2 w-9 text-center">
                      <input
                        type="checkbox"
                        checked={allVisibleSelected}
                        onChange={toggleVisibleSelection}
                        disabled={filteredAccounts.length === 0 || loginTaskActive}
                        aria-label="全量选择当前筛选账号"
                      />
                    </th>
                    <th className="py-3 px-4 font-medium">账号邮箱 / 身份</th>
                    <th className="py-3 px-3 font-medium">分级健康状态</th>
                    <th className="py-3 px-3 font-medium">粘性出口代理</th>
                    <th className="py-3 px-3 font-medium">Token 剩余寿命</th>
                    <th className="py-3 px-3 font-medium">调用 / 失败</th>
                    <th className="py-3 px-4 font-medium text-right">
                      快捷操作
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y z-border">
                  {filteredAccounts.map((acc) => {
                    const fullCool = formatRemainingCountdown(
                      acc.cooldown_until,
                      nowMs
                    );
                    const imgCool = formatRemainingCountdown(
                      acc.image_cooldown_until,
                      nowMs
                    );
                    const renewCool = formatRemainingCountdown(
                      acc.renew_cooldown_until,
                      nowMs
                    );
                    const errTip = acc.last_renew_error || acc.last_error;
                    const ttlBar = getTtlBar(acc.remaining_days);
                    const subMeta = [
                      acc.token_preview || "",
                      acc.tls_profile ? `TLS ${acc.tls_profile}` : "",
                    ]
                      .filter(Boolean)
                      .join(" · ");

                    return (
                      <tr key={acc.id} className="z-row-hover group">
                        <td className="py-3 px-2 text-center">
                          <input
                            type="checkbox"
                            aria-label={`选择 ${acc.email || acc.id} 进行重新登录`}
                            checked={selectedAccountIds.has(acc.id)}
                            disabled={
                              loggingInKami || updatingProxy || loginTaskActive
                            }
                            onChange={() => toggleAccountSelection(acc.id)}
                          />
                        </td>
                        <td className="py-3 px-4">
                          <div className="flex items-center gap-2.5">
                            <div
                              className="w-7 h-7 rounded-full flex items-center justify-center font-mono-num font-semibold shrink-0"
                              style={{
                                background: "var(--bg-muted)",
                                border: "1px solid var(--hairline)",
                              }}
                            >
                              {getAvatarInitial(acc)}
                            </div>
                            <div className="min-w-0">
                              <div className="flex items-center gap-1.5">
                                <span className="font-mono-num font-medium truncate max-w-[230px]">
                                  {acc.email ||
                                    acc.chatgpt_account_id ||
                                    acc.id}
                                </span>
                                <span className="px-1.5 py-0.25 rounded-md border z-border z-muted font-mono-num uppercase text-[11px]">
                                  {acc.type}
                                </span>
                                <button
                                  type="button"
                                  onClick={() =>
                                    handleCopyText(
                                      acc.id,
                                      acc.email ||
                                        acc.chatgpt_account_id ||
                                        acc.id
                                    )
                                  }
                                  className="p-0.5 rounded z-text-muted hover-reveal cursor-pointer"
                                  title="复制账号标识"
                                >
                                  {copiedId === acc.id ? (
                                    <Check size={12} />
                                  ) : (
                                    <Copy size={12} />
                                  )}
                                </button>
                              </div>
                              {subMeta && (
                                <div className="font-mono-num z-text-muted mt-0.5 truncate max-w-[260px]">
                                  {subMeta}
                                </div>
                              )}
                            </div>
                          </div>
                        </td>

                        <td className="py-3 px-3">
                          <StatusBadge
                            status={acc.status}
                            imageLimited={acc.image_limited}
                            renewState={acc.renew_state}
                          />
                          {(fullCool || imgCool || renewCool) && (
                            <div className="font-mono-num z-text-muted mt-1">
                              {fullCool ? `全局恢复 ${fullCool} ` : ""}
                              {imgCool ? `生图恢复 ${imgCool} ` : ""}
                              {renewCool ? `重登退避 ${renewCool}` : ""}
                            </div>
                          )}
                          {errTip && (
                            <div
                              className="mt-1 max-w-[230px] truncate"
                              style={{ color: "var(--pill-danger-text)" }}
                              title={errTip}
                            >
                              {errTip}
                            </div>
                          )}
                        </td>

                        <td className="py-3 px-3 font-mono-num z-text-sec max-w-[180px] truncate">
                          {acc.proxy || "直连出口"}
                        </td>

                        <td className="py-3 px-3">
                          <div className="w-24 space-y-1">
                            <div className="font-mono-num">
                              {acc.remaining_days !== undefined
                                ? `${acc.remaining_days.toFixed(1)} 天`
                                : "免登会话"}
                            </div>
                            <div className="h-1 w-full rounded-full overflow-hidden z-muted">
                              <div
                                className="h-full rounded-full"
                                style={{
                                  width: `${ttlBar.pct}%`,
                                  background: ttlBar.color,
                                }}
                              />
                            </div>
                          </div>
                        </td>

                        <td className="py-3 px-3 font-mono-num whitespace-nowrap z-text-sec">
                          {acc.total_calls ?? 0} / {acc.failed_calls ?? 0}
                        </td>

                        <td className="py-3 px-4 text-right whitespace-nowrap">
                          <div className="inline-flex items-center gap-3 hover-reveal">
                            <button
                              type="button"
                              disabled={actionLoadingKey === `${acc.id}:probe`}
                              onClick={() => handleAction(acc, "probe")}
                              className="z-text-sec hover:underline cursor-pointer disabled:opacity-50"
                            >
                              {actionLoadingKey === `${acc.id}:probe`
                                ? "测活中"
                                : "测活"}
                            </button>
                            {acc.has_kami_credentials && (
                              <>
                                <button
                                  type="button"
                                  disabled={
                                    actionLoadingKey === `${acc.id}:totp`
                                  }
                                  onClick={() => handleAction(acc, "totp")}
                                  className="z-text-sec hover:underline font-mono-num cursor-pointer disabled:opacity-50"
                                  title="获取实时 6 位 2FA 动态码"
                                >
                                  2FA
                                </button>
                                <button
                                  type="button"
                                  disabled={
                                    actionLoadingKey === `${acc.id}:login`
                                  }
                                  onClick={() => handleAction(acc, "login")}
                                  className="z-text-sec hover:underline cursor-pointer disabled:opacity-50"
                                >
                                  重登
                                </button>
                              </>
                            )}
                            {(acc.has_refresh_token ||
                              acc.has_session_token) && (
                              <button
                                type="button"
                                disabled={
                                  actionLoadingKey === `${acc.id}:refresh`
                                }
                                onClick={() => handleAction(acc, "refresh")}
                                className="z-text-sec hover:underline cursor-pointer disabled:opacity-50"
                              >
                                续期
                              </button>
                            )}
                            <button
                              type="button"
                              disabled={
                                actionLoadingKey === `${acc.id}:enable` ||
                                actionLoadingKey === `${acc.id}:disable`
                              }
                              onClick={() =>
                                handleAction(
                                  acc,
                                  acc.status === "disabled"
                                    ? "enable"
                                    : "disable"
                                )
                              }
                              className="z-text-sec hover:underline cursor-pointer disabled:opacity-50"
                            >
                              {acc.status === "disabled" ? "启用" : "停用"}
                            </button>
                            <button
                              type="button"
                              onClick={() => setDeleteTarget(acc)}
                              className="hover:underline cursor-pointer"
                              style={{ color: "var(--pill-danger-text)" }}
                              title="从号池移除该账号"
                            >
                              移除
                            </button>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                  {filteredAccounts.length === 0 && (
                    <tr>
                      <td
                        colSpan={7}
                        className="py-10 text-center z-text-muted"
                      >
                        当前号池无匹配账号，可点击右上角「导入卡密 / JSON」添加账号。
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}

      {/* 批量导入弹窗 */}
      {showImport && (
        <div
          className="fixed inset-0 z-50 bg-black/55 backdrop-blur-xs flex items-center justify-center p-4"
          onClick={() => !importing && setShowImport(false)}
        >
          <div
            className="p-surface z-text rounded-2xl max-w-lg w-full p-5 shadow-2xl space-y-4 text-xs"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between pb-3 border-b z-border">
              <div>
                <h3 className="text-sm font-semibold">导入账号到号池</h3>
                <p className="z-text-muted mt-0.5">
                  支持 ChatGPT 卡密（email----pwd----totp）、Sub2API JSON、JWT Token 等格式。
                </p>
              </div>
              <button
                type="button"
                onClick={() => !importing && setShowImport(false)}
                className="p-1 rounded-lg border z-border z-ghost-btn cursor-pointer"
              >
                <X size={14} />
              </button>
            </div>

            <div className="space-y-1.5">
              <label className="font-medium z-text-sec block">凭据格式</label>
              <div className="grid grid-cols-2 sm:grid-cols-3 gap-1.5">
                {[
                  { k: "kami", label: "卡密账号" },
                  { k: "sub2api_json", label: "Sub2API JSON" },
                  { k: "access_token", label: "Access Token" },
                  { k: "refresh_token", label: "Refresh Token" },
                  { k: "session_token", label: "Session Token" },
                  { k: "free_uuid", label: "NoAuth UUID" },
                ].map((opt) => (
                  <button
                    key={opt.k}
                    type="button"
                    onClick={() => setImportFormat(opt.k)}
                    className={`px-2.5 py-1.5 rounded-xl border text-center cursor-pointer ${
                      importFormat === opt.k
                        ? "z-primary-btn border-transparent font-medium"
                        : "z-border z-surface z-ghost-btn"
                    }`}
                  >
                    {opt.label}
                  </button>
                ))}
              </div>
            </div>

            {importFormat === "free_uuid" && (
              <div className="space-y-1.5">
                <label className="font-medium z-text-sec block">
                  自动生成免登设备数量（下方文本留空时生效）
                </label>
                <input
                  type="number"
                  min={1}
                  max={200}
                  value={generateCount}
                  onChange={(e) => setGenerateCount(Number(e.target.value))}
                  className="w-32 px-3 py-1.5 rounded-xl p-input font-mono-num"
                />
              </div>
            )}

            <div className="space-y-1.5">
              <label className="font-medium z-text-sec block">凭据内容</label>
              <span className="z-text-muted">
                {importFormat === "sub2api_json"
                  ? "支持完整 Sub2API 导出 JSON"
                  : importFormat === "free_uuid"
                    ? `已填写 ${importCredentialCount} 行 UUID`
                    : `已填写 ${importCredentialCount} 行凭据`}
              </span>
              <textarea
                rows={7}
                value={importText}
                onChange={(e) => setImportText(e.target.value)}
                placeholder={IMPORT_PLACEHOLDERS[importFormat] || ""}
                className="w-full p-3 rounded-xl p-code-box font-mono-num resize-y focus:outline-none"
              />
            </div>

            {importFormat === "kami" && (
              <div className="px-3 py-2.5 rounded-lg border z-border z-muted">
                导入后可全量选择账号，使用后台批量登录任务；账号按序处理并动态更新状态。
              </div>
            )}

            <div className="flex justify-end gap-2 pt-2 border-t z-border">
              <button
                type="button"
                disabled={importing}
                onClick={() => setShowImport(false)}
                className="px-3.5 py-1.5 rounded-xl border z-border z-ghost-btn font-medium cursor-pointer"
              >
                取消
              </button>
              <button
                type="button"
                disabled={importing}
                onClick={handleImportSubmit}
                className="px-4 py-1.5 rounded-xl z-primary-btn font-medium cursor-pointer"
              >
                {importing ? "正在导入处理..." : "确认导入并持久化"}
              </button>
            </div>
          </div>
        </div>
      )}

      {showBulkEdit && (
        <div
          className="fixed inset-0 z-50 bg-black/55 backdrop-blur-xs flex items-center justify-center p-4"
          onClick={() => !updatingProxy && setShowBulkEdit(false)}
        >
          <div
            className="p-surface z-text rounded-xl max-w-lg w-full p-5 shadow-2xl space-y-4 text-xs"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between pb-3 border-b z-border">
              <div>
                <h3 className="text-sm font-semibold">批量编辑账号</h3>
                <p className="z-text-muted mt-0.5">已选择 {selectedCount} 个账号</p>
              </div>
              <button
                type="button"
                onClick={() => !updatingProxy && setShowBulkEdit(false)}
                disabled={updatingProxy}
                className="p-1 rounded-lg border z-border z-ghost-btn cursor-pointer"
                aria-label="关闭批量编辑"
              >
                <X size={14} />
              </button>
            </div>

            <div className="space-y-1.5">
              <label className="font-medium z-text-sec block" htmlFor="bulk-proxy">
                出口代理
              </label>
              <select
                id="bulk-proxy"
                value={bulkProxyUrl}
                onChange={(e) => setBulkProxyUrl(e.target.value)}
                disabled={updatingProxy || loginTaskActive}
                className="w-full px-3 py-2 rounded-lg p-input"
              >
                {proxyNodes.length === 0 ? (
                  <option value="">直连出口（未配置代理）</option>
                ) : (
                  <option value="" disabled>选择健康代理</option>
                )}
                {proxyNodes.map((node, index) => (
                  <option key={node.url} value={node.url} disabled={node.status !== "active"}>
                    {proxyOptionLabel(node, index)}
                  </option>
                ))}
              </select>
              <p className="z-text-muted">
                新代理立即应用到所选账号；服务重启后会按代理池重新初始化绑定。
              </p>
            </div>

            {proxyNodes.length > 0 && !proxyNodes.some((node) => node.status === "active") && (
              <p className="text-amber-700">
                当前没有健康代理节点，请先到代理与模型目录页测活或更新代理池。
              </p>
            )}

            <div className="flex justify-end gap-2 pt-2 border-t z-border">
              <button
                type="button"
                disabled={updatingProxy}
                onClick={() => setShowBulkEdit(false)}
                className="px-3.5 py-1.5 rounded-lg border z-border z-ghost-btn font-medium cursor-pointer"
              >
                取消
              </button>
              <button
                type="button"
                disabled={
                  updatingProxy ||
                  loginTaskActive ||
                  selectedCount === 0 ||
                  Boolean(isPlannedProvider) ||
                  (proxyNodes.length > 0 && !bulkProxyUrl)
                }
                onClick={handleBulkProxyUpdate}
                className="px-4 py-1.5 rounded-lg z-primary-btn font-medium cursor-pointer disabled:opacity-50"
              >
                {updatingProxy ? "正在更新..." : "应用到所选账号"}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Animated SVG Ring 2FA TOTP Modal */}
      <TotpRingModal
        info={totpInfo}
        copied={copiedTotp}
        onCopy={async () => {
          if (!totpInfo) return;
          await navigator.clipboard.writeText(totpInfo.code);
          setCopiedTotp(true);
          window.setTimeout(() => setCopiedTotp(false), 1500);
        }}
        onClose={() => setTotpInfo(null)}
      />

      {/* Delete Confirmation Modal */}
      {deleteTarget && (
        <div
          className="fixed inset-0 z-50 bg-black/55 backdrop-blur-xs flex items-center justify-center p-4"
          onClick={() => setDeleteTarget(null)}
        >
          <div
            className="p-surface z-text rounded-2xl max-w-sm w-full p-5 shadow-2xl space-y-3 text-xs"
            onClick={(e) => e.stopPropagation()}
          >
            <h3 className="text-sm font-semibold">确认移出账号？</h3>
            <p className="z-text-muted leading-relaxed">
              即将从运行时号池移除账号{" "}
              <span className="font-mono-num font-semibold z-text">
                {deleteTarget.email || deleteTarget.id}
              </span>
              ，确定继续吗？
            </p>
            <div className="flex justify-end gap-2 pt-2">
              <button
                type="button"
                onClick={() => setDeleteTarget(null)}
                className="px-3 py-1.5 rounded-xl border z-border z-ghost-btn cursor-pointer"
              >
                取消
              </button>
              <button
                type="button"
                onClick={async () => {
                  const target = deleteTarget;
                  setDeleteTarget(null);
                  await handleAction(target, "delete");
                }}
                className="px-3.5 py-1.5 rounded-xl bg-rose-600 text-white font-medium cursor-pointer"
              >
                确认移除
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
