import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Activity,
  AlertCircle,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Copy,
  Download,
  Loader2,
  LogIn,
  Plus,
  RefreshCw,
  Search,
  X,
  KeyRound,
  ExternalLink,
  Sparkles,
  Pencil,
  Trash2,
  Play,
  Pause,
  ShieldAlert,
  SlidersHorizontal,
} from "lucide-react";
import {
  cancelKamiLogin,
  dismissAdminTask,
  executeAccountAction,
  bulkUpdateAccountProxy,
  bulkAccountAction,
  fetchAccounts,
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
  type ProbeTask,
  type ProviderSummary,
  type ProxyNodeStatus,
  fetchProviderChannels,
  type ProviderChannel,
} from "../api/client";
import {
  formatElapsedSeconds,
  formatRelativeTimeAgo,
  formatRemainingCountdown,
  NoticeBanner,
  StatusBadge,
  TotpRingModal,
} from "../components/ui";
import { seedModelsCatalogFromSlugs } from "../lib/models";

const IMPORT_PLACEHOLDERS: Record<string, string> = {
  cookie:
    "每行一个账号凭据，支持纯 Cookie 内容：\ndatr=...; hatch_sess=...; ps_l=0\n\n或附带账号标识（邮箱/备注）：\nuser@example.com----datr=...; hatch_sess=...\n\n导入后将直接激活号池并持久化。",
  kami: "每行一条卡密，支持 ---- / | / 空格 / Tab 分隔，自动识别 邮箱、密码、2FA密钥、Access Token (eyJ...) 与 Refresh Token (rt_...)，不限字段顺序\n例如: user@example.com----MyPassword123----JBSWY3DPEHPK3PXP\n或带 at: user@example.com----eyJhbGci...----MyPassword123----JBSWY3DPEHPK3PXP",
  sub2api_json:
    '粘贴 Sub2API 标准导出 JSON（包含 "type": "sub2api-data" 与 "accounts" 数组）',
  access_token: "每行一条 Access Token (eyJ...)，可选携带 account_id",
  refresh_token: "每行一条 Refresh Token (rt_...)",
  session_token: "每行一条会话 Cookie (如 datr=...; ps_l=1) 或 NextAuth Session Token",
  free_uuid: "留空则按下方数量自动生成 UUID 访客设备，或每行粘贴一个 UUID",
};

const GROK_AUTO_PLACEHOLDER =
  "每行自动识别，可混合粘贴：\nuser@example.com|MyPassword123|JBSWY3DPEHPK3PXP\nuser@example.com----MyPassword123----JBSWY3DPEHPK3PXP\nuser@example.com----sso=...; sso-rw=...\nsso=...; sso-rw=...";

const GROK_KAMI_PLACEHOLDER =
  "每行一条 Grok 卡密：邮箱|密码|2FA密钥\nuser@example.com|MyPassword123|JBSWY3DPEHPK3PXP\n第三列也支持旧 JWT (eyJ...) 或旧 SSO Cookie；旧 JWT 仅导入为待命凭据，不会当作可用 SSO 或自动重登\n可选追加代理：user@example.com|MyPassword123|JBSWY3DPEHPK3PXP|http://proxy:port";

const GROK_COOKIE_PLACEHOLDER =
  "每行一条已有 SSO Cookie：sso=...; sso-rw=...\n也可附带邮箱标识：user@example.com----sso=...; sso-rw=...";

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
  const [dbChannels, setDbChannels] = useState<ProviderChannel[]>([]);

  const [statusFilter, setStatusFilter] = useState("");
  const [typeFilter, setTypeFilter] = useState("");
  const [search, setSearch] = useState("");
  const [loading, setLoading] = useState(false);
  const [probingAll, setProbingAll] = useState(false);
  const [loggingInKami, setLoggingInKami] = useState(false);
  const [cancellingQueue, setCancellingQueue] = useState(false);
  const [cancellingRenewQueue, setCancellingRenewQueue] = useState(false);
  const [cancellingSelected, setCancellingSelected] = useState(false);
  const [loginTask, setLoginTask] = useState<KamiLoginTask | null>(null);
  const [probeTask, setProbeTask] = useState<ProbeTask | null>(null);
  const [expandLoginDetails, setExpandLoginDetails] = useState(false);
  const [expandProbeDetails, setExpandProbeDetails] = useState(false);
  const trackedLoginIdRef = useRef<string | null>(null);
  const trackedProbeIdRef = useRef<string | null>(null);
  const [updatingProxy, setUpdatingProxy] = useState(false);
  const [headlessLogin, setHeadlessLogin] = useState(false);
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
  const [bulkOperatingAction, setBulkOperatingAction] = useState<string | null>(null);
  const [showBulkDeleteConfirm, setShowBulkDeleteConfirm] = useState(false);
  const [bulkEditTab, setBulkEditTab] = useState<"state" | "proxy" | "danger">("state");
  const [showOAuthGuide, setShowOAuthGuide] = useState(false);
  const [oauthTargetEmail, setOauthTargetEmail] = useState("");
  const [oauthCookieInput, setOauthCookieInput] = useState("");
  const [oauthSubmitting, setOauthSubmitting] = useState(false);
  const [extractingChrome, setExtractingChrome] = useState(false);
  const [browserLoggingIn, setBrowserLoggingIn] = useState(false);
  const [editingCookieAccount, setEditingCookieAccount] = useState<AccountView | null>(null);
  const [editCookieValue, setEditCookieValue] = useState("");
  const [loadingEditCookie, setLoadingEditCookie] = useState(false);
  const [savingEditCookie, setSavingEditCookie] = useState(false);
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

  const applySyncedTasks = useCallback(
    (
      nextLoginTask?: KamiLoginTask | null,
      nextProbeTask?: ProbeTask | null,
      nextWorker?: KamiWorkerStats | null
    ) => {
      if (nextWorker !== undefined) {
        setWorkerStats(nextWorker || null);
      }
      if (nextLoginTask !== undefined) {
        setLoginTask(nextLoginTask || null);
        if (nextLoginTask) {
          if (
            nextLoginTask.status === "queued" ||
            nextLoginTask.status === "running"
          ) {
            trackedLoginIdRef.current = nextLoginTask.id;
          } else if (
            nextLoginTask.status === "completed" &&
            trackedLoginIdRef.current === nextLoginTask.id
          ) {
            trackedLoginIdRef.current = null;
            const failures = (nextLoginTask.results || []).filter(
              (item) => !item.ok && !item.cancelled
            );
            const examples = failures
              .slice(0, 3)
              .map(
                (item) =>
                  `${item.email || item.account_id}: ${item.error || "登录失败"}`
              );
            const cancelPart = nextLoginTask.cancelled
              ? `，${nextLoginTask.cancelled} 已取消`
              : "";
            setNotice({
              type: nextLoginTask.failed > 0 ? "info" : "success",
              text: `后台登录任务完成：${nextLoginTask.success} 成功，${nextLoginTask.failed} 失败${cancelPart}${examples.length ? `；${examples.join("；")}${failures.length > examples.length ? `；另有 ${failures.length - examples.length} 项失败` : ""}` : ""}`,
            });
            setSelectedAccountIds(new Set());
          }
        }
      }
      if (nextProbeTask !== undefined) {
        setProbeTask(nextProbeTask || null);
        if (nextProbeTask) {
          if (nextProbeTask.status === "running") {
            trackedProbeIdRef.current = nextProbeTask.id;
          } else if (
            nextProbeTask.status === "completed" &&
            trackedProbeIdRef.current === nextProbeTask.id
          ) {
            trackedProbeIdRef.current = null;
            setNotice({
              type: nextProbeTask.failed > 0 ? "info" : "success",
              text: `全池测活完成：共探测 ${nextProbeTask.probed} 个账号，正常 ${nextProbeTask.passed} 个，异常 ${nextProbeTask.failed} 个`,
            });
          }
        }
      }
    },
    []
  );

  const loadData = useCallback(
    async (silent = false) => {
      if (!silent) {
        setLoading(true);
      }
      try {
        const ov = await fetchOverview();
        setProviders(ov.providers || []);
        fetchProviderChannels().then((r) => setDbChannels(r.channels || [])).catch(() => {});

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
          applySyncedTasks(
            accRes.login_task ?? ov.login_task,
            accRes.probe_task ?? ov.probe_task,
            accRes.kami_worker ?? ov.kami_worker
          );
        } else {
          setStats(targetSummary?.stats || null);
          setAccounts([]);
          applySyncedTasks(ov.login_task, ov.probe_task, ov.kami_worker);
        }
        window.dispatchEvent(new Event("pool-updated"));
      } catch (e: any) {
        if (!silent) {
          setNotice({
            type: "error",
            text: `加载数据失败: ${e?.message || "未知错误"}`,
          });
        }
      } finally {
        if (!silent) {
          setLoading(false);
        }
      }
    },
    [activeProvider, statusFilter, typeFilter, applySyncedTasks]
  );

  useEffect(() => {
    void loadData(false);
  }, [loadData]);

  useEffect(() => {
    setSelectedAccountIds(new Set());
  }, [activeProvider, statusFilter, typeFilter]);

  useEffect(() => {
    setImportFormat(
      activeProvider === "grok"
        ? "auto"
        : activeProvider === "meta_muse"
          ? "cookie"
          : "kami"
    );
  }, [activeProvider]);

  useEffect(() => {
    let cancelled = false;
    fetchProxies()
      .then((res) => {
        if (cancelled) return;
        setProxyNodes(res.nodes || []);
        setBulkProxyUrl(
          (current) =>
            current ||
            res.nodes.find((node) => node.status === "active")?.url ||
            ""
        );
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  const hasActiveOperations = useMemo(() => {
    const loginBusy = Boolean(
      loginTask &&
        (loginTask.status === "queued" || loginTask.status === "running")
    );
    const probeBusy = Boolean(probeTask && probeTask.status === "running");
    const workerBusy = Boolean(
      workerStats && (workerStats.running || workerStats.queue_length > 0)
    );
    const rowBusy = accounts.some(
      (acc) =>
        Boolean(acc.active_action) ||
        acc.renew_state === "running" ||
        acc.renew_state === "queued"
    );
    return loginBusy || probeBusy || workerBusy || rowBusy;
  }, [loginTask, probeTask, workerStats, accounts]);

  useEffect(() => {
    if (!hasActiveOperations) return;
    let cancelled = false;
    let timer = 0;

    const tick = async () => {
      await loadData(true);
      if (!cancelled) {
        timer = window.setTimeout(tick, 1200);
      }
    };

    timer = window.setTimeout(tick, 1200);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [hasActiveOperations, loadData]);

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
        !a.active_action &&
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
      const res = await executeAccountAction(
        activeProvider,
        acc.id,
        action,
        action === "login" ? { headless: headlessLogin } : undefined
      );
      if (action === "totp" && res.result?.totp_code) {
        setTotpInfo({
          accountId: acc.id,
          email: res.result.email || acc.email || acc.id,
          code: res.result.totp_code,
          remainingSeconds: res.result.remaining_seconds ?? 30,
        });
        setCopiedTotp(false);
      } else if (action === "login" && res.result?.task) {
        trackedLoginIdRef.current = res.result.task.id;
        setLoginTask(res.result.task);
        setNotice({
          type: "info",
          text:
            res.result?.message ||
            `账号 ${acc.email || acc.id} 已加入后台重登队列`,
        });
      } else {
        if (
          action === "probe" &&
          res.result?.ok &&
          Array.isArray(res.result?.models) &&
          res.result.models.length > 0
        ) {
          seedModelsCatalogFromSlugs(res.result.models);
        }
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
      await loadData(true);
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
    setNotice({ type: "info", text: "正在创建全池并发测活任务..." });
    try {
      const res = await probeAllAccounts(activeProvider, true);
      if (res.task) {
        trackedProbeIdRef.current = res.task.id;
        setProbeTask(res.task);
        if (res.task.status === "running") {
          setNotice({
            type: "info",
            text: `全池测活已启动：共 ${res.task.total} 个账号，实时进度将在上方与列表回显`,
          });
        } else {
          setNotice({
            type: res.task.failed > 0 ? "info" : "success",
            text: `全池测活完成：共探测 ${res.task.probed} 个账号，正常 ${res.task.passed} 个，异常 ${res.task.failed} 个`,
          });
        }
      } else {
        setNotice({
          type: res.failed > 0 ? "info" : "success",
          text: `全池测活完成：共探测 ${res.probed} 个账号，正常 ${res.passed} 个，异常 ${res.failed} 个`,
        });
      }
      await loadData(true);
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
      trackedLoginIdRef.current = res.task.id;
      setLoginTask(res.task);
      setNotice({
        type: "info",
        text: `后台登录任务已更新：当前队列共 ${res.task.total} 个账号`,
      });
      await loadData(true);
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `批量登录失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setLoggingInKami(false);
    }
  };

  const handleCancelQueuedAccount = async (acc: AccountView) => {
    setActionLoadingKey(`${acc.id}:cancel`);
    const isRenewQueued =
      acc.active_action === "renew" ||
      (!acc.active_action && acc.renew_state === "queued");
    try {
      const res = await cancelKamiLogin(
        activeProvider,
        isRenewQueued
          ? {
              account_id: acc.id,
              queue: "renew",
            }
          : {
              task_id: acc.action_task_id || loginTask?.id,
              account_id: acc.id,
              queue: "login",
            }
      );
      if (res.task !== undefined) {
        setLoginTask(res.task || null);
      }
      setNotice({
        type: "info",
        text: `已取消 ${acc.email || acc.id} 的排队等待`,
      });
      await loadData(true);
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `取消排队失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setActionLoadingKey(null);
    }
  };

  const handleCancelRenewQueue = async () => {
    if (!workerStats || workerStats.queue_length <= 0) return;
    setCancellingRenewQueue(true);
    try {
      const res = await cancelKamiLogin(activeProvider, {
        cancel_all: true,
        queue: "renew",
      });
      setNotice({
        type: "info",
        text: `已取消剩余 ${res.cancelled} 个自动续期排队账号`,
      });
      await loadData(true);
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `取消自动续期排队失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setCancellingRenewQueue(false);
    }
  };

  const handleCancelRemainingLogin = async () => {
    if (!loginTask) return;
    setCancellingQueue(true);
    try {
      const res = await cancelKamiLogin(activeProvider, {
        task_id: loginTask.id,
        cancel_all: true,
      });
      if (res.task !== undefined) {
        setLoginTask(res.task || null);
      }
      setNotice({
        type: "info",
        text: `已取消剩余 ${res.cancelled} 个排队账号，当前执行中的账号完成后任务结束`,
      });
      await loadData(true);
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `取消剩余排队失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setCancellingQueue(false);
    }
  };

  const handleDismissTask = async (kind: "login" | "probe", taskId?: string) => {
    try {
      await dismissAdminTask(activeProvider, { kind, task_id: taskId });
      if (kind === "login") {
        setLoginTask(null);
        setExpandLoginDetails(false);
      } else {
        setProbeTask(null);
        setExpandProbeDetails(false);
      }
      await loadData(true);
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `关闭战报失败: ${e?.message || "未知错误"}`,
      });
    }
  };

  const eligibleVisibleAccounts = filteredAccounts.filter(
    (acc) => acc.has_kami_credentials && acc.status !== "disabled"
  );
  const selectedCount = selectedAccountIds.size;
  const selectedQueuedAccounts = useMemo(
    () =>
      accounts.filter((acc) => {
        if (!selectedAccountIds.has(acc.id)) return false;
        const isQueued =
          acc.action_phase === "queued" ||
          (!acc.active_action && acc.renew_state === "queued");
        return (
          isQueued &&
          (acc.active_action === "login" ||
            acc.active_action === "renew" ||
            acc.renew_state === "queued")
        );
      }),
    [accounts, selectedAccountIds]
  );
  const selectedAccountsList = useMemo(
    () => accounts.filter((acc) => selectedAccountIds.has(acc.id)),
    [accounts, selectedAccountIds]
  );
  const disabledSelectedCount = useMemo(
    () => selectedAccountsList.filter((acc) => acc.status === "disabled").length,
    [selectedAccountsList]
  );
  const activeSelectedCount = useMemo(
    () => selectedAccountsList.filter((acc) => acc.status === "active").length,
    [selectedAccountsList]
  );
  const loginTaskActive = Boolean(
    loginTask && (loginTask.status === "queued" || loginTask.status === "running")
  );
  const probeTaskActive = Boolean(probeTask && probeTask.status === "running");
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
      text: loginTaskActive
        ? `正在将 ${ids.length} 个账号追加至运行中的重登队列...`
        : `正在为 ${ids.length} 个账号创建后台登录任务，账号间间隔 10 秒...`,
    });
    try {
      const res = await loginKamiAccounts(activeProvider, headlessLogin, false, ids);
      trackedLoginIdRef.current = res.task.id;
      setLoginTask(res.task);
      setSelectedAccountIds(new Set());
      setNotice({
        type: "info",
        text: `后台登录队列已更新：任务总计 ${res.task.total} 个账号。`,
      });
      await loadData(true);
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `批量重登失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setLoggingInKami(false);
    }
  };

  const handleCancelSelectedQueued = async () => {
    if (!selectedQueuedAccounts.length) return;
    setCancellingSelected(true);
    try {
      const res = await cancelKamiLogin(activeProvider, {
        account_ids: selectedQueuedAccounts.map((acc) => acc.id),
      });
      if (res.task !== undefined) {
        setLoginTask(res.task || null);
      }
      setSelectedAccountIds(new Set());
      setNotice({
        type: "info",
        text: `已批量取消 ${res.cancelled} 个账号的排队等待`,
      });
      await loadData(true);
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `批量取消排队失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setCancellingSelected(false);
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

  const handleBulkAction = async (action: AccountActionType) => {
    const ids = [...selectedAccountIds];
    if (!ids.length) return;
    setBulkOperatingAction(action);
    const actionLabels: Record<string, string> = {
      delete: "批量移除",
      disable: "批量停用",
      enable: "批量启用",
      probe: "批量测试",
      standby: "批量转待命",
      activate: "批量激活",
      refresh: "批量刷新",
    };
    const actionLabel = actionLabels[action] || action;
    try {
      let successCount = 0;
      let failedCount = 0;
      const failures: string[] = [];

      try {
        const res = await bulkAccountAction(activeProvider, ids, action);
        successCount = res.result.success;
        failedCount = res.result.failed;
        res.result.results
          .filter((item) => !item.ok)
          .forEach((item) => failures.push(`${item.id}: ${item.error || "失败"}`));
      } catch (bulkErr: any) {
        // 若后端尚未重启或为旧版本 (404 Not Found)，平滑回退到并发客户端逐个执行
        if (
          bulkErr?.status === 404 ||
          String(bulkErr?.message).includes("404") ||
          String(bulkErr?.message).includes("not found")
        ) {
          const results = await Promise.allSettled(
            ids.map((id) => executeAccountAction(activeProvider, id, action))
          );
          results.forEach((r, idx) => {
            if (r.status === "fulfilled") {
              successCount++;
            } else {
              failedCount++;
              failures.push(`${ids[idx]}: ${r.reason?.message || "失败"}`);
            }
          });
        } else {
          throw bulkErr;
        }
      }

      const isDelete = action === "delete";
      setNotice({
        type: failedCount > 0 ? (successCount > 0 ? "info" : "error") : "success",
        text: `${actionLabel}完成：成功 ${successCount}/${ids.length} 个账号${
          failures.length
            ? `（失败项：${failures.slice(0, 3).join("；")}${failures.length > 3 ? " 等" : ""}）`
            : ""
        }`,
      });
      if (isDelete) {
        setShowBulkDeleteConfirm(false);
      }
      setShowBulkEdit(false);
      setSelectedAccountIds(new Set());
      await loadData(true);
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `${actionLabel}失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setBulkOperatingAction(null);
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
        autoLogin:
          activeProvider === "grok" &&
          (importFormat === "auto" || importFormat === "kami"),
        headless: headlessLogin,
        generateCount:
          importFormat === "free_uuid" && !trimmed ? generateCount : 0,
      });
      const r = res.result;
      const extraMsg = r.messages?.length ? `（${r.messages.join("；")}）` : "";
      let importNotice: { type: "info" | "success" | "error"; text: string } = {
        type: r.failed > 0 && r.added === 0 ? ("error" as const) : ("success" as const),
        text: `导入完成：新增 ${r.added} 个，跳过重复 ${r.skipped} 个，失败 ${r.failed} 个${extraMsg}`,
      };

      setShowImport(false);
      setImportText("");

      if (r.login_task) {
        trackedLoginIdRef.current = r.login_task.id;
        setLoginTask(r.login_task);
        importNotice = {
          type: "info",
          text: `导入完成并已创建无头 SSO 提取任务（队列 ${r.login_task.total} 个账号）。`,
        };
      }

      setNotice(importNotice);
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

  const handleOAuthSubmit = async () => {
    const trimmed = oauthCookieInput.trim();
    if (!trimmed) {
      setNotice({ type: "error", text: "请粘贴登录成功后的完整 Cookie 内容" });
      return;
    }
    if (activeProvider === "meta_muse" && !trimmed.includes("hatch_sess=")) {
      setNotice({
        type: "error",
        text: "Cookie 缺少核心 HttpOnly 鉴权字段 hatch_sess（在 Console 执行 document.cookie 会丢失该字段）。请使用「启动授权浏览器登录」、Cookie-Editor 导出 Header String，或从 F12 -> Network 请求头复制完整 Cookie。",
      });
      return;
    }
    setOauthSubmitting(true);
    try {
      const payload = oauthTargetEmail ? `${oauthTargetEmail}----${trimmed}` : trimmed;
      const res = await importAccounts({
        provider: activeProvider,
        credentialType: "cookie",
        rawText: payload,
        persist: true,
        autoLogin: false,
        headless: true,
      });
      setShowOAuthGuide(false);
      setOauthCookieInput("");
      setNotice({
        type: "success",
        text: `🎉 OAuth2 / 网页授权成功！已为 ${oauthTargetEmail || "账号"} 录入完整凭据并点亮为 active 状态`,
      });
      await loadData();
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `授权凭据提交失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setOauthSubmitting(false);
    }
  };

  const handleExtractLocalChromeCookie = async () => {
    setExtractingChrome(true);
    try {
      const res = await executeAccountAction("meta_muse", "current", "extract_chrome");
      if (res.result?.ok && res.result.cookie) {
        setOauthCookieInput(res.result.cookie);
        try {
          await navigator.clipboard.writeText(res.result.cookie);
        } catch {
          // 剪贴板权限受限时忽略，输入框已自动填入
        }
        setNotice({
          type: "success",
          text: `🎉 成功从本机 Chrome 提取到 ${res.result.count || ""} 个真实 Cookie，已自动填入并复制到剪贴板！点击下方确认即可激活。`,
        });
      } else {
        setNotice({
          type: "error",
          text: res.result?.error || "未在本地 Chrome 中找到已登录的 muse.ai 凭据，请使用「启动授权浏览器」或从 Network 请求头复制 Cookie",
        });
      }
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `读取本地 Chrome 失败: ${e?.message || "请使用「启动授权浏览器」或从 Network 请求头手动复制"}`,
      });
    } finally {
      setExtractingChrome(false);
    }
  };

  const handleBrowserLoginMuseCookie = async () => {
    setBrowserLoggingIn(true);
    setNotice({
      type: "info",
      text: "正在启动内置授权浏览器并打开 muse.ai，请在弹出的浏览器窗口中完成登录...",
    });
    try {
      const res = await executeAccountAction("meta_muse", "current", "browser_login");
      if (res.result?.ok && res.result.cookie) {
        setOauthCookieInput(res.result.cookie);
        try {
          await navigator.clipboard.writeText(res.result.cookie);
        } catch {
          // 剪贴板权限受限时忽略
        }
        setNotice({
          type: "success",
          text: `🎉 已通过授权浏览器捕获 ${res.result.count || ""} 个真实会话 Cookie（含 HttpOnly），已自动填入并复制到剪贴板！`,
        });
      } else {
        setNotice({
          type: "error",
          text: res.result?.error || "浏览器授权登录未获取到 Cookie，请重试或手动从 F12 Network 复制",
        });
      }
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `启动授权浏览器失败: ${e?.message || "请手动从 F12 Network 复制 Cookie"}`,
      });
    } finally {
      setBrowserLoggingIn(false);
    }
  };

  const handleOpenEditCookie = async (acc: AccountView) => {
    setEditingCookieAccount(acc);
    setEditCookieValue("");
    setLoadingEditCookie(true);
    try {
      const res = await executeAccountAction(activeProvider, acc.id, "get_cookie");
      if (res.result?.cookie) {
        setEditCookieValue(res.result.cookie);
      }
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `读取当前 Cookie 失败: ${e?.message || "请直接粘贴新的完整 Cookie"}`,
      });
    } finally {
      setLoadingEditCookie(false);
    }
  };

  const handleSaveEditCookie = async () => {
    if (!editingCookieAccount) return;
    const trimmed = editCookieValue.trim();
    if (!trimmed) {
      setNotice({ type: "error", text: "Cookie 内容不能为空" });
      return;
    }
    if (activeProvider === "meta_muse" && !trimmed.includes("hatch_sess=")) {
      setNotice({
        type: "error",
        text: "Cookie 缺少核心 HttpOnly 鉴权字段 hatch_sess（在 Console 执行 document.cookie 会丢失该字段）。请从 F12 -> Network 请求头或 Cookie-Editor 导出完整 Cookie。",
      });
      return;
    }
    setSavingEditCookie(true);
    try {
      const res = await executeAccountAction(activeProvider, editingCookieAccount.id, "update_cookie", {
        cookie: trimmed,
      });
      setEditingCookieAccount(null);
      setEditCookieValue("");
      setNotice({
        type: "success",
        text: res.result?.message || `已更新 ${editingCookieAccount.email || editingCookieAccount.id} 的 Cookie 凭据`,
      });
      await loadData();
    } catch (e: any) {
      setNotice({
        type: "error",
        text: `保存 Cookie 失败: ${e?.message || "未知错误"}`,
      });
    } finally {
      setSavingEditCookie(false);
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
            disabled={probingAll || probeTaskActive || Boolean(isPlannedProvider)}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer disabled:opacity-50"
          >
            {probingAll || probeTaskActive ? (
              <Loader2 size={13} className="animate-spin" />
            ) : (
              <Activity size={13} />
            )}
            <span>
              {probeTaskActive && probeTask
                ? `测活中 (${probeTask.probed}/${probeTask.total})`
                : probingAll
                  ? "启动测活..."
                  : "全池测活"}
            </span>
          </button>
          {activeProvider !== "meta_muse" && (
            <>
              <button
                type="button"
                onClick={handleLoginKami}
                disabled={loggingInKami || proxyUnavailable || Boolean(isPlannedProvider)}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer disabled:opacity-50"
              >
                <LogIn size={13} />
                <span>
                  {loggingInKami
                    ? "提交中..."
                    : loginTaskActive
                      ? "追加待激活到队列"
                      : "待激活批量登录"}
                </span>
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
            </>
          )}
          <button
            type="button"
            onClick={() => onOpenExport(activeProvider)}
            disabled={Boolean(isPlannedProvider)}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
          >
            <Download size={13} />
            <span>导出号池</span>
          </button>
          {activeProvider === "meta_muse" && (
            <button
              type="button"
              onClick={() => {
                setOauthTargetEmail("");
                setOauthCookieInput("");
                setShowOAuthGuide(true);
              }}
              className="inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl border border-blue-500/30 bg-blue-500/10 text-blue-600 dark:text-blue-400 font-medium cursor-pointer hover:bg-blue-500/20 transition-colors"
              title="打开网页完成认证，回填 Cookie 激活号池"
            >
              <KeyRound size={13} />
              <span>Web 授权向导</span>
            </button>
          )}
          <button
            type="button"
            onClick={() => {
              if (activeProvider === "grok") {
                setImportFormat("auto");
              } else if (activeProvider === "meta_muse") {
                setImportFormat("cookie");
              } else if (importFormat === "cookie") {
                setImportFormat("kami");
              }
              setShowImport(true);
            }}
            disabled={Boolean(isPlannedProvider)}
            className="inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl z-primary-btn font-medium cursor-pointer"
          >
            <Plus size={13} />
            <span>{activeProvider === "grok" ? "导入 Grok 卡密 / SSO" : activeProvider === "meta_muse" ? "导入 Cookie 凭据" : "导入卡密 / JSON"}</span>
          </button>
        </div>
      </div>

      {proxyUnavailable && (
        <div className="border-l-2 border-amber-500 px-3 py-2 text-xs text-amber-800" role="status">
          当前代理池没有健康节点，批量登录已暂停。请先测活代理或更新代理列表。
        </div>
      )}

      {/* 数据库录入多模态免密与公共渠道池概览 */}
      {dbChannels.length > 0 && (
        <div className="p-card rounded-2xl border z-border p-4 shadow-xs space-y-3">
          <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
              <h3 className="text-xs font-semibold z-text">号池数据库录入的多模态渠道 (免登录 & 免费池)</h3>
              <span className="px-2 py-0.2 rounded-full text-[10px] bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 font-mono">
                {dbChannels.filter((c) => c.status === "active").length} 活跃可用 / 共 {dbChannels.length} 渠道
              </span>
            </div>
            <span className="text-[11px] z-text-muted">
              免密逆向已开箱即用；如需真实云端超大算力可随时提供 Free 账号/Key
            </span>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-2.5">
            {dbChannels.map((ch) => (
              <div
                key={ch.id}
                onClick={() => {
                  if (providers.some((p) => p.descriptor.id === ch.provider_id)) {
                    setActiveProvider(ch.provider_id);
                  }
                }}
                className={`p-3 rounded-xl border z-border transition-all cursor-pointer ${
                  ch.status === "active"
                    ? "bg-emerald-500/5 hover:border-emerald-500/40"
                    : "bg-muted/20 hover:border-primary/30"
                }`}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="text-xs font-medium z-text truncate">{ch.channel_name}</span>
                  <span
                    className={`px-1.5 py-0.2 text-[10px] rounded font-mono ${
                      ch.status === "active"
                        ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
                        : "bg-amber-500/10 text-amber-600 dark:text-amber-400"
                    }`}
                  >
                    {ch.status === "active" ? "免密可用" : "待配置Free账号"}
                  </span>
                </div>
                <p className="text-[11px] z-text-muted mt-1 line-clamp-2 leading-relaxed">
                  {ch.description}
                </p>
                <div className="flex items-center justify-between mt-2 pt-1.5 border-t z-border text-[10px] z-text-muted font-mono">
                  <span>{ch.provider_id}</span>
                  <span className="capitalize">{ch.auth_type.replace("_", " ")}</span>
                </div>
              </div>
            ))}
          </div>
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
            {workerStats.running && (
              <Loader2 size={11} className="animate-spin shrink-0" />
            )}
            <span>
              自动续期队列：{workerStats.queue_length} 排队
              {workerStats.running && workerStats.running_email
                ? ` · ${workerStats.running_email}${workerStats.running_phase_label ? ` (${workerStats.running_phase_label})` : ""}`
                : ""}
            </span>
            {workerStats.queue_length > 0 && (
              <button
                type="button"
                disabled={cancellingRenewQueue}
                onClick={handleCancelRenewQueue}
                className="ml-1 px-1.5 py-0.5 rounded border z-border hover:underline cursor-pointer disabled:opacity-50"
                style={{ color: "var(--pill-danger-text)" }}
                title="取消自动续期等待队列中的所有账号"
              >
                {cancellingRenewQueue ? "取消中..." : "取消排队"}
              </button>
            )}
          </div>
        )}
      </div>

      {/* 渠道标签体系徽章 */}
      {activeProviderSummary?.descriptor.tags && activeProviderSummary.descriptor.tags.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5 py-1 text-xs">
          <span className="z-text-muted font-medium text-[11px]">渠道特性：</span>
          {activeProviderSummary.descriptor.tags.map((t) => (
            <span
              key={t.key}
              title={t.tooltip}
              className={`px-2 py-0.5 rounded-full text-[11px] font-medium border ${
                t.variant === "success"
                  ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20"
                  : t.variant === "purple"
                  ? "bg-purple-500/10 text-purple-600 dark:text-purple-400 border-purple-500/20"
                  : t.variant === "warning"
                  ? "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20"
                  : t.variant === "cyan"
                  ? "bg-cyan-500/10 text-cyan-600 dark:text-cyan-400 border-cyan-500/20"
                  : "bg-muted z-text-muted border-border"
              }`}
            >
              {t.label}
            </span>
          ))}
          <span className="z-text-muted text-[11px] ml-1">
            · {activeProviderSummary.descriptor.description}
          </span>
        </div>
      )}

      {notice && (
        <NoticeBanner
          type={notice.type}
          text={notice.text}
          onClose={() => setNotice(null)}
        />
      )}

      {loginTask && !loginTask.dismissed && (
        <div
          className="p-card rounded-xl px-3.5 py-3 space-y-2.5 text-xs"
          role="status"
          aria-live="polite"
        >
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2 min-w-0">
              {loginTaskActive ? (
                <Loader2 size={14} className="animate-spin shrink-0" style={{ color: "var(--pill-info-text)" }} />
              ) : loginTask.failed > 0 ? (
                <AlertCircle size={14} className="shrink-0" style={{ color: "var(--pill-warn-text)" }} />
              ) : (
                <CheckCircle2 size={14} className="shrink-0" style={{ color: "var(--pill-success-text)" }} />
              )}
              <span className="font-medium">
                {loginTaskActive
                  ? `后台重登任务：${loginTask.completed}/${loginTask.total}`
                  : `重登任务已完成：${loginTask.total} 个账号`}
              </span>
              {loginTaskActive && (
                <span className="font-mono-num z-text-sec truncate">
                  {loginTask.current_email
                    ? `${loginTask.current_email} · ${loginTask.current_phase_label || loginTask.current_phase || "执行中"}${formatElapsedSeconds(loginTask.current_started_at, nowMs) ? ` (${formatElapsedSeconds(loginTask.current_started_at, nowMs)})` : ""}`
                    : loginTask.current_phase === "cooldown_wait"
                      ? `${loginTask.current_phase_label || "等待账号间隔"}${formatElapsedSeconds(loginTask.current_started_at, nowMs) ? ` (${formatElapsedSeconds(loginTask.current_started_at, nowMs)})` : ""}`
                      : "准备队列中"}
                </span>
              )}
            </div>

            <div className="flex flex-wrap items-center gap-2 shrink-0">
              <span className="z-text-muted font-mono-num">
                {`${loginTask.success} 成功 · ${loginTask.failed} 失败${loginTask.cancelled ? ` · ${loginTask.cancelled} 已取消` : ""}${(loginTask.queued_account_ids?.length ?? 0) > 0 ? ` · ${loginTask.queued_account_ids!.length} 排队` : ""}`}
              </span>
              {loginTaskActive && (loginTask.queued_account_ids?.length ?? 0) > 0 && (
                <button
                  type="button"
                  disabled={cancellingQueue}
                  onClick={handleCancelRemainingLogin}
                  className="px-2 py-0.5 rounded-lg border z-border hover:underline cursor-pointer disabled:opacity-50"
                  style={{ color: "var(--pill-danger-text)" }}
                >
                  {cancellingQueue
                    ? "取消中..."
                    : `取消剩余排队 (${loginTask.queued_account_ids!.length})`}
                </button>
              )}
              {(loginTask.results?.length ?? 0) > 0 && (
                <button
                  type="button"
                  onClick={() => setExpandLoginDetails((v) => !v)}
                  className="inline-flex items-center gap-1 px-2 py-0.5 rounded-lg border z-border z-ghost-btn cursor-pointer"
                >
                  <span>
                    {expandLoginDetails
                      ? "收起明细"
                      : `展开明细 (${loginTask.results.length})`}
                  </span>
                  {expandLoginDetails ? (
                    <ChevronUp size={12} />
                  ) : (
                    <ChevronDown size={12} />
                  )}
                </button>
              )}
              {!loginTaskActive && (
                <button
                  type="button"
                  onClick={() => handleDismissTask("login", loginTask.id)}
                  className="inline-flex items-center gap-1 px-2 py-0.5 rounded-lg border z-border z-ghost-btn cursor-pointer"
                  aria-label="关闭重登战报"
                >
                  <X size={12} />
                  <span>关闭战报</span>
                </button>
              )}
            </div>
          </div>

          <progress
            className="w-full h-1.5 accent-emerald-600"
            max={Math.max(loginTask.total, 1)}
            value={loginTask.completed}
            aria-label="批量登录进度"
          />

          {expandLoginDetails && (loginTask.results?.length ?? 0) > 0 && (
            <div className="max-h-44 overflow-y-auto border-t z-border pt-2 space-y-1">
              {loginTask.results.map((item, idx) => (
                <div
                  key={`${item.account_id}-${idx}`}
                  className="flex flex-wrap items-center justify-between gap-2 py-1 px-2 rounded-lg"
                  style={{ background: "var(--bg-input)" }}
                >
                  <div className="flex items-center gap-2 min-w-0">
                    <span
                      className={`px-1.5 py-0.25 rounded-full border text-[11px] font-medium shrink-0 ${
                        item.ok
                          ? "z-pill-success"
                          : item.cancelled
                            ? "z-pill-warn"
                            : "z-pill-danger"
                      }`}
                    >
                      {item.ok ? "成功" : item.cancelled ? "已取消" : "失败"}
                    </span>
                    <span className="font-mono-num truncate max-w-[260px]">
                      {item.email || item.account_id}
                    </span>
                  </div>
                  {item.error && (
                    <span
                      className="font-mono-num truncate max-w-[420px]"
                      style={{
                        color: item.cancelled
                          ? "var(--pill-warn-text)"
                          : "var(--pill-danger-text)",
                      }}
                      title={item.error}
                    >
                      {item.error}
                    </span>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {probeTask && !probeTask.dismissed && (
        <div
          className="p-card rounded-xl px-3.5 py-3 space-y-2.5 text-xs"
          role="status"
          aria-live="polite"
        >
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2 min-w-0">
              {probeTaskActive ? (
                <Loader2 size={14} className="animate-spin shrink-0" style={{ color: "var(--pill-info-text)" }} />
              ) : probeTask.failed > 0 ? (
                <AlertCircle size={14} className="shrink-0" style={{ color: "var(--pill-warn-text)" }} />
              ) : (
                <CheckCircle2 size={14} className="shrink-0" style={{ color: "var(--pill-success-text)" }} />
              )}
              <span className="font-medium">
                {probeTaskActive
                  ? `全池测活进行中：${probeTask.probed}/${probeTask.total}`
                  : `全池测活已完成：共探测 ${probeTask.probed}/${probeTask.total} 个账号`}
              </span>
            </div>

            <div className="flex flex-wrap items-center gap-2 shrink-0">
              <span className="z-text-muted font-mono-num">
                {`正常 ${probeTask.passed} · 异常 ${probeTask.failed}`}
              </span>
              {(probeTask.results?.length ?? 0) > 0 && (
                <button
                  type="button"
                  onClick={() => setExpandProbeDetails((v) => !v)}
                  className="inline-flex items-center gap-1 px-2 py-0.5 rounded-lg border z-border z-ghost-btn cursor-pointer"
                >
                  <span>
                    {expandProbeDetails
                      ? "收起明细"
                      : `展开明细 (${probeTask.results!.length})`}
                  </span>
                  {expandProbeDetails ? (
                    <ChevronUp size={12} />
                  ) : (
                    <ChevronDown size={12} />
                  )}
                </button>
              )}
              {!probeTaskActive && (
                <button
                  type="button"
                  onClick={() => handleDismissTask("probe", probeTask.id)}
                  className="inline-flex items-center gap-1 px-2 py-0.5 rounded-lg border z-border z-ghost-btn cursor-pointer"
                  aria-label="关闭测活战报"
                >
                  <X size={12} />
                  <span>关闭战报</span>
                </button>
              )}
            </div>
          </div>

          <progress
            className="w-full h-1.5 accent-emerald-600"
            max={Math.max(probeTask.total, 1)}
            value={probeTask.probed}
            aria-label="全池测活进度"
          />

          {expandProbeDetails && (probeTask.results?.length ?? 0) > 0 && (
            <div className="max-h-44 overflow-y-auto border-t z-border pt-2 space-y-1">
              {probeTask.results!.map((item, idx) => (
                <div
                  key={`${item.id || "probe"}-${idx}`}
                  className="flex flex-wrap items-center justify-between gap-2 py-1 px-2 rounded-lg"
                  style={{ background: "var(--bg-input)" }}
                >
                  <div className="flex items-center gap-2 min-w-0">
                    <span
                      className={`px-1.5 py-0.25 rounded-full border text-[11px] font-medium shrink-0 ${
                        item.ok ? "z-pill-success" : "z-pill-danger"
                      }`}
                    >
                      {item.ok ? "正常" : "异常"}
                    </span>
                    <span className="font-mono-num truncate max-w-[260px]">
                      {item.email || item.id}
                    </span>
                  </div>
                  <span
                    className="font-mono-num truncate max-w-[420px]"
                    style={{
                      color: item.ok
                        ? "var(--text-muted)"
                        : "var(--pill-danger-text)",
                    }}
                    title={item.error || ""}
                  >
                    {item.ok
                      ? `${item.latency_ms ?? 0}ms`
                      : item.error || "探测失败"}
                  </span>
                </div>
              ))}
            </div>
          )}
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
                  {(stats.standby ?? 0) > 0 && (
                    <span className="ml-1.5 text-xs font-normal z-text-muted">
                      待命 {stats.standby}
                    </span>
                  )}
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
                  { k: "standby", label: "待命" },
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
                  eligibleVisibleAccounts.every((item) => item.status === "active")
                }
                className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn cursor-pointer disabled:opacity-50"
              >
                选择待激活账号
              </button>
              <button
                type="button"
                onClick={selectVisibleAccounts}
                disabled={filteredAccounts.length === 0}
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
                    disabled={loggingInKami || updatingProxy}
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
                onClick={() => void loadData(false)}
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
            <div className="flex flex-wrap items-center justify-between gap-2.5 p-card rounded-xl px-3.5 py-2.5 text-xs shadow-sm border border-blue-500/20 bg-blue-500/5 transition-all">
              <div className="flex items-center gap-2">
                <span className="inline-flex items-center gap-1.5 font-medium z-text">
                  <span className="w-2 h-2 rounded-full bg-blue-500 animate-pulse" />
                  已选择 <strong className="font-mono-num text-sm text-blue-600 dark:text-blue-400">{selectedCount}</strong> 个账号
                </span>
                {(activeSelectedCount > 0 || disabledSelectedCount > 0) && (
                  <span className="z-text-muted text-[11px] hidden sm:inline">
                    ({activeSelectedCount} 在线 · {disabledSelectedCount} 停用)
                  </span>
                )}
                <button
                  type="button"
                  onClick={() => setSelectedAccountIds(new Set())}
                  disabled={Boolean(bulkOperatingAction) || loggingInKami || updatingProxy}
                  className="text-[11px] z-text-muted hover:z-text underline cursor-pointer ml-1"
                >
                  清除已选
                </button>
              </div>

              <div className="flex flex-wrap items-center gap-2">
                {selectedQueuedAccounts.length > 0 && (
                  <button
                    type="button"
                    onClick={handleCancelSelectedQueued}
                    disabled={cancellingSelected || loggingInKami || updatingProxy || Boolean(bulkOperatingAction)}
                    className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer disabled:opacity-50"
                    style={{ color: "var(--pill-danger-text)" }}
                  >
                    <span>
                      {cancellingSelected
                        ? "取消中..."
                        : `批量取消排队 (${selectedQueuedAccounts.length})`}
                    </span>
                  </button>
                )}

                {/* 批量测活 */}
                <button
                  type="button"
                  onClick={() => handleBulkAction("probe")}
                  disabled={Boolean(bulkOperatingAction) || updatingProxy || loggingInKami || Boolean(isPlannedProvider)}
                  className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer disabled:opacity-50 text-blue-600 dark:text-blue-400 hover:bg-blue-500/10"
                  title="批量健康检测与可用模型测活"
                >
                  <Activity size={12} className={bulkOperatingAction === "probe" ? "animate-spin" : ""} />
                  <span>{bulkOperatingAction === "probe" ? "测活中..." : "批量测试"}</span>
                </button>

                {/* 批量启用 */}
                <button
                  type="button"
                  onClick={() => handleBulkAction("enable")}
                  disabled={Boolean(bulkOperatingAction) || updatingProxy || loggingInKami || Boolean(isPlannedProvider)}
                  className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer disabled:opacity-50 text-emerald-600 dark:text-emerald-400 hover:bg-emerald-500/10"
                  title="将选中的账号批量恢复为在线活跃状态"
                >
                  <Play size={12} className={bulkOperatingAction === "enable" ? "animate-spin" : ""} />
                  <span>{bulkOperatingAction === "enable" ? "启用中..." : "批量启用"}</span>
                </button>

                {/* 批量停用 */}
                <button
                  type="button"
                  onClick={() => handleBulkAction("disable")}
                  disabled={Boolean(bulkOperatingAction) || updatingProxy || loggingInKami || Boolean(isPlannedProvider)}
                  className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer disabled:opacity-50 text-amber-600 dark:text-amber-400 hover:bg-amber-500/10"
                  title="将选中的账号批量停用，暂停调度"
                >
                  <Pause size={12} className={bulkOperatingAction === "disable" ? "animate-spin" : ""} />
                  <span>{bulkOperatingAction === "disable" ? "停用中..." : "批量停用"}</span>
                </button>

                {/* 更多批量编辑中心 */}
                <button
                  type="button"
                  onClick={() => {
                    setBulkEditTab("state");
                    setShowBulkEdit(true);
                  }}
                  disabled={Boolean(bulkOperatingAction) || updatingProxy || loggingInKami || Boolean(isPlannedProvider)}
                  className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer disabled:opacity-50"
                >
                  <SlidersHorizontal size={12} />
                  <span>批量编辑...</span>
                </button>

                {/* 卡密批量登录（若支持） */}
                {selectedAccountsList.some((acc) => acc.has_kami_credentials) && (
                  <button
                    type="button"
                    onClick={handleLoginSelectedKami}
                    disabled={
                      loggingInKami ||
                      updatingProxy ||
                      Boolean(bulkOperatingAction) ||
                      proxyUnavailable ||
                      Boolean(isPlannedProvider) ||
                      selectedCount === 0
                    }
                    className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl z-primary-btn font-medium cursor-pointer disabled:opacity-50"
                  >
                    <LogIn size={13} />
                    <span>
                      {loggingInKami
                        ? "提交中..."
                        : loginTaskActive
                          ? "追加到重登队列"
                          : "批量重登"}
                    </span>
                  </button>
                )}

                {/* 批量永久移除（危险操作） */}
                <button
                  type="button"
                  onClick={() => setShowBulkDeleteConfirm(true)}
                  disabled={Boolean(bulkOperatingAction) || updatingProxy || loggingInKami || Boolean(isPlannedProvider)}
                  className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border border-red-500/30 bg-red-500/10 text-red-600 dark:text-red-400 hover:bg-red-500/20 font-medium cursor-pointer disabled:opacity-50 transition-colors"
                  title="彻底从号池中移除选中的账号"
                >
                  <Trash2 size={12} className={bulkOperatingAction === "delete" ? "animate-spin" : ""} />
                  <span>批量移除</span>
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
                        disabled={filteredAccounts.length === 0}
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
                    const lastSummary =
                      !acc.active_action && acc.last_action_summary
                        ? `${formatRelativeTimeAgo(acc.last_action_at, nowMs) ? `${formatRelativeTimeAgo(acc.last_action_at, nowMs)} · ` : ""}${acc.last_action_summary}`
                        : "";
                    const showErrTip = errTip
                      ? !lastSummary ||
                        !acc.last_action_summary?.includes(errTip.slice(0, 24))
                      : false;
                    const isLocalBusy =
                      actionLoadingKey?.startsWith(`${acc.id}:`) ?? false;
                    const liveAction =
                      acc.active_action ||
                      (acc.renew_state === "running" || acc.renew_state === "queued"
                        ? "renew"
                        : "");
                    const isRowQueued =
                      liveAction !== "" &&
                      (acc.action_phase === "queued" ||
                        (!acc.active_action && acc.renew_state === "queued"));
                    const isQueueCancellable =
                      isRowQueued &&
                      (acc.active_action === "login" ||
                        acc.active_action === "renew" ||
                        acc.renew_state === "queued");
                    const isRowBusy = Boolean(liveAction) || isLocalBusy;
                    const isProbing =
                      acc.active_action === "probe" ||
                      actionLoadingKey === `${acc.id}:probe`;
                    const isLogging =
                      acc.active_action === "login" ||
                      acc.active_action === "renew" ||
                      acc.renew_state === "running" ||
                      acc.renew_state === "queued" ||
                      actionLoadingKey === `${acc.id}:login`;
                    const isRefreshing =
                      acc.active_action === "refresh" ||
                      actionLoadingKey === `${acc.id}:refresh`;
                    const isBanned = acc.status === "banned";
                    const ttlBar = getTtlBar(acc.remaining_days);
                    const subMeta = [
                      acc.token_preview || "",
                      acc.tls_profile ? `TLS ${acc.tls_profile}` : "",
                    ]
                      .filter(Boolean)
                      .join(" · ");

                    return (
                      <tr
                        key={acc.id}
                        className={`z-row-hover group ${
                          isBanned
                            ? "opacity-60 bg-zinc-500/5 text-zinc-400 dark:text-zinc-500"
                            : ""
                        }`}
                      >
                        <td className="py-3 px-2 text-center">
                          <input
                            type="checkbox"
                            aria-label={`选择 ${acc.email || acc.id} 进行重新登录`}
                            checked={selectedAccountIds.has(acc.id)}
                            disabled={loggingInKami || updatingProxy}
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
                                opacity: isBanned ? 0.6 : 1,
                              }}
                            >
                              {getAvatarInitial(acc)}
                            </div>
                            <div className="min-w-0">
                              <div className="flex items-center gap-1.5">
                                <span
                                  className={`font-mono-num font-medium truncate max-w-[230px] ${
                                    isBanned
                                      ? "text-zinc-400 dark:text-zinc-500"
                                      : ""
                                  }`}
                                >
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
                            activeAction={acc.active_action}
                            actionPhase={acc.action_phase}
                            actionPhaseLabel={acc.action_phase_label}
                            actionQueuePos={acc.action_queue_pos}
                            actionStartedAt={acc.action_started_at}
                            nowMs={nowMs}
                          />
                          {(fullCool || imgCool || renewCool) && (
                            <div className="font-mono-num z-text-muted mt-1">
                              {fullCool ? `全局恢复 ${fullCool} ` : ""}
                              {imgCool ? `生图恢复 ${imgCool} ` : ""}
                              {renewCool ? `重登退避 ${renewCool}` : ""}
                            </div>
                          )}
                          {lastSummary && (
                            <div
                              className="mt-1 max-w-[240px] truncate"
                              style={{
                                color: isBanned
                                  ? "var(--text-muted)"
                                  : acc.last_action_ok
                                  ? "var(--pill-success-text)"
                                  : "var(--pill-danger-text)",
                              }}
                              title={lastSummary}
                            >
                              {lastSummary}
                            </div>
                          )}
                          {showErrTip && (
                            <div
                              className="mt-1 max-w-[240px] truncate"
                              style={{
                                color: isBanned
                                  ? "var(--text-muted)"
                                  : "var(--pill-danger-text)",
                              }}
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
                                  background: isBanned
                                    ? "var(--text-muted)"
                                    : ttlBar.color,
                                }}
                              />
                            </div>
                          </div>
                        </td>

                        <td className="py-3 px-3 font-mono-num whitespace-nowrap z-text-sec">
                          {acc.total_calls ?? 0} / {acc.failed_calls ?? 0}
                        </td>

                        <td className="py-3 px-4 text-right whitespace-nowrap">
                          <div
                            className={`inline-flex items-center gap-3 ${isRowBusy ? "" : "hover-reveal"}`}
                          >
                            <button
                              type="button"
                              disabled={isProbing || isLogging}
                              onClick={() => handleAction(acc, "probe")}
                              className="z-text-sec hover:underline cursor-pointer disabled:opacity-60 inline-flex items-center gap-1"
                            >
                              {isProbing && (
                                <Loader2 size={11} className="animate-spin" />
                              )}
                              <span>{isProbing ? "测活中" : "测活"}</span>
                            </button>
                            {activeProvider === "meta_muse" && (
                              <button
                                type="button"
                                disabled={actionLoadingKey === `${acc.id}:sync_chrome`}
                                onClick={() => handleAction(acc, "sync_chrome")}
                                className="z-text-sec hover:underline cursor-pointer disabled:opacity-60 inline-flex items-center gap-1 text-xs"
                                title="从本机已登录 Chrome 自动提取并同步更新凭据"
                              >
                                {actionLoadingKey === `${acc.id}:sync_chrome` && (
                                  <Loader2 size={11} className="animate-spin" />
                                )}
                                <span>
                                  {actionLoadingKey === `${acc.id}:sync_chrome`
                                    ? "同步中"
                                    : "从本机同步"}
                                </span>
                              </button>
                            )}
                            {(acc.type === "cookie" || activeProvider === "meta_muse") && (
                              <button
                                type="button"
                                onClick={() => handleOpenEditCookie(acc)}
                                className="hover:underline cursor-pointer font-medium text-emerald-600 dark:text-emerald-400 inline-flex items-center gap-0.5 text-xs"
                                title="查看并手动编辑该账号的 Cookie 凭据"
                              >
                                <Pencil size={11} />
                                <span>编辑 Cookie</span>
                              </button>
                            )}
                            {activeProvider === "meta_muse" && (
                              <button
                                type="button"
                                onClick={() => {
                                  setOauthTargetEmail(acc.email || acc.id);
                                  setOauthCookieInput("");
                                  setShowOAuthGuide(true);
                                }}
                                className="hover:underline cursor-pointer font-medium text-blue-500 inline-flex items-center gap-0.5 text-xs"
                                title="打开网页授权向导，为该账号录入最新 Cookie"
                              >
                                <KeyRound size={11} />
                                <span>Web授权</span>
                              </button>
                            )}
                            {activeProvider !== "meta_muse" && acc.has_kami_credentials && (
                              <>
                                {acc.has_totp_secret && (
                                  <button
                                    type="button"
                                    disabled={actionLoadingKey === `${acc.id}:totp`}
                                    onClick={() => handleAction(acc, "totp")}
                                    className="z-text-sec hover:underline font-mono-num cursor-pointer disabled:opacity-50"
                                    title="获取实时 6 位 2FA 动态码"
                                  >
                                    2FA
                                  </button>
                                )}
                                {isQueueCancellable ? (
                                  <button
                                    type="button"
                                    disabled={
                                      actionLoadingKey === `${acc.id}:cancel`
                                    }
                                    onClick={() =>
                                      handleCancelQueuedAccount(acc)
                                    }
                                    className="hover:underline cursor-pointer disabled:opacity-50 font-medium"
                                    style={{ color: "var(--pill-warn-text)" }}
                                    title="从等待队列移除该账号"
                                  >
                                    {actionLoadingKey === `${acc.id}:cancel`
                                      ? "取消中..."
                                      : `取消排队${acc.action_queue_pos ? ` #${acc.action_queue_pos}` : ""}`}
                                  </button>
                                ) : (
                                  <button
                                    type="button"
                                    disabled={isLogging}
                                    onClick={() => handleAction(acc, "login")}
                                    className="z-text-sec hover:underline cursor-pointer disabled:opacity-75 inline-flex items-center gap-1"
                                    style={
                                      isLogging
                                        ? { color: "var(--pill-info-text)" }
                                        : undefined
                                    }
                                  >
                                    {isLogging && (
                                      <Loader2
                                        size={11}
                                        className="animate-spin"
                                      />
                                    )}
                                    <span>
                                      {isLogging
                                        ? acc.action_phase_label
                                          ? `重登 · ${acc.action_phase_label}`
                                          : "重登中..."
                                        : "重登"}
                                    </span>
                                  </button>
                                )}
                              </>
                            )}
                            {(acc.has_refresh_token ||
                              acc.has_session_token) && (
                              <button
                                type="button"
                                disabled={isRefreshing || isLogging}
                                onClick={() => handleAction(acc, "refresh")}
                                className="z-text-sec hover:underline cursor-pointer disabled:opacity-60 inline-flex items-center gap-1"
                              >
                                {isRefreshing && (
                                  <Loader2 size={11} className="animate-spin" />
                                )}
                                <span>{isRefreshing ? "续期中" : "续期"}</span>
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
                            {(acc.status === "active" ||
                              acc.status === "standby") && (
                              <button
                                type="button"
                                disabled={
                                  actionLoadingKey === `${acc.id}:standby` ||
                                  actionLoadingKey === `${acc.id}:activate`
                                }
                                onClick={() =>
                                  handleAction(
                                    acc,
                                    acc.status === "standby"
                                      ? "activate"
                                      : "standby"
                                  )
                                }
                                className="z-text-sec hover:underline cursor-pointer disabled:opacity-50"
                                title="待命账号不参与调度、续期与连接占用"
                              >
                                {acc.status === "standby" ? "激活" : "待命"}
                              </button>
                            )}
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
            className="p-surface z-text rounded-2xl max-w-lg w-full max-h-[90vh] overflow-y-auto p-5 shadow-2xl space-y-4 text-xs"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between pb-3 border-b z-border">
              <div>
                <h3 className="text-sm font-semibold">
                  {activeProvider === "grok"
                    ? "导入 xAI Grok 凭据"
                    : activeProvider === "meta_muse"
                      ? "导入 Meta AI Muse 凭据"
                      : "导入账号到号池"}
                </h3>
                <p className="z-text-muted mt-0.5">
                  {activeProvider === "grok"
                    ? "支持邮箱|密码|2FA 卡密自动提取 SSO，也支持直接导入已有 SSO Cookie。"
                    : activeProvider === "meta_muse"
                      ? "支持批量粘贴 muse.ai 会话 Cookie，或使用 Web 授权向导一键提取。"
                      : "支持 ChatGPT 卡密（email----pwd----totp）、Sub2API JSON、JWT Token 等格式。"}
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

            {activeProvider === "meta_muse" ? (
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <label className="font-medium z-text-sec block">凭据格式</label>
                  <button
                    type="button"
                    onClick={() => {
                      setShowImport(false);
                      setShowOAuthGuide(true);
                    }}
                    className="inline-flex items-center gap-1 text-[11px] text-blue-500 hover:underline cursor-pointer"
                  >
                    <KeyRound size={12} />
                    <span>改用 Web 授权向导一键提取</span>
                  </button>
                </div>
                <div className="p-2.5 rounded-xl border border-blue-500/20 bg-blue-500/5 text-xs text-blue-600 dark:text-blue-400">
                  当前供应商仅支持 <strong>Cookie 会话凭据</strong>。已为您移除失效的卡密/账密自动登录方式。
                </div>
              </div>
            ) : (
              <div className="space-y-1.5">
                <label className="font-medium z-text-sec block">凭据格式</label>
                <div className="grid grid-cols-2 sm:grid-cols-3 gap-1.5">
                  {(
                    activeProvider === "grok"
                      ? [
                          { k: "auto", label: "自动识别" },
                          { k: "kami", label: "Grok 卡密" },
                          { k: "cookie", label: "SSO Cookie" },
                        ]
                      : [
                          { k: "kami", label: "卡密账号" },
                          { k: "sub2api_json", label: "Sub2API JSON" },
                          { k: "access_token", label: "Access Token" },
                          { k: "refresh_token", label: "Refresh Token" },
                          { k: "session_token", label: "Session Token" },
                          { k: "free_uuid", label: "NoAuth UUID" },
                        ]
                  ).map((opt) => (
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
            )}

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
                placeholder={
                    activeProvider === "grok" && importFormat === "auto"
                      ? GROK_AUTO_PLACEHOLDER
                      : activeProvider === "grok" && importFormat === "kami"
                        ? GROK_KAMI_PLACEHOLDER
                        : activeProvider === "grok" && importFormat === "cookie"
                        ? GROK_COOKIE_PLACEHOLDER
                        : IMPORT_PLACEHOLDERS[importFormat] || ""
                  }
                className="w-full p-3 rounded-xl p-code-box font-mono-num resize-y focus:outline-none"
              />
            </div>

            {(importFormat === "kami" || importFormat === "auto") &&
              activeProvider !== "meta_muse" && (
              <div className="px-3 py-2.5 rounded-lg border z-border z-muted">
                {activeProvider === "grok"
                  ? "自动识别卡密与已有 SSO Cookie；卡密会自动创建无头 SSO 提取任务，Cookie 直接激活。未识别行可在服务端开启 GROK_CREDENTIAL_LLM_ENABLED 后交给低成本 LLM 归一化（会发送原始凭据，默认关闭）；成功格式会脱敏沉淀到数据库，后续同格式离线命中。Linux 服务器需提供 CHROME_PATH 指向的 Chrome/Chromium。"
                  : "导入后可全量选择账号，使用后台批量登录任务；账号按序处理并动态更新状态。"}
              </div>
            )}
            {activeProvider === "grok" && importFormat === "cookie" && (
              <div className="px-3 py-2.5 rounded-lg border z-border z-muted">
                直接导入已有 sso / sso-rw Cookie 会立即激活；后续可在账号行使用「重登」从卡密重新提取。
              </div>
            )}
            {activeProvider === "meta_muse" && (
              <div className="px-3 py-2.5 rounded-lg border border-blue-500/20 bg-blue-500/5 text-xs text-blue-600 dark:text-blue-400">
                支持直接粘贴 Cookie（如 datr=...; hatch_sess=...），导入后将立即以 active 状态入池并持久化。
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
          onClick={() => !updatingProxy && !bulkOperatingAction && setShowBulkEdit(false)}
        >
          <div
            className="p-surface z-text rounded-xl max-w-lg w-full max-h-[90vh] overflow-y-auto p-5 shadow-2xl space-y-4 text-xs"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between pb-3 border-b z-border">
              <div>
                <h3 className="text-sm font-semibold flex items-center gap-1.5">
                  <SlidersHorizontal size={15} />
                  <span>批量编辑与管理账号</span>
                </h3>
                <p className="z-text-muted mt-0.5">已选择 {selectedCount} 个账号进行批量操作</p>
              </div>
              <button
                type="button"
                onClick={() => !updatingProxy && !bulkOperatingAction && setShowBulkEdit(false)}
                disabled={updatingProxy || Boolean(bulkOperatingAction)}
                className="p-1 rounded-lg border z-border z-ghost-btn cursor-pointer"
                aria-label="关闭批量编辑"
              >
                <X size={14} />
              </button>
            </div>

            {/* Tab 导航 */}
            <div className="flex border-b z-border gap-4 text-xs font-medium">
              <button
                type="button"
                onClick={() => setBulkEditTab("state")}
                className={`pb-2 -mb-px border-b-2 cursor-pointer transition-colors ${
                  bulkEditTab === "state"
                    ? "border-primary text-primary"
                    : "border-transparent z-text-muted hover:z-text"
                }`}
              >
                状态与健康
              </button>
              <button
                type="button"
                onClick={() => setBulkEditTab("proxy")}
                className={`pb-2 -mb-px border-b-2 cursor-pointer transition-colors ${
                  bulkEditTab === "proxy"
                    ? "border-primary text-primary"
                    : "border-transparent z-text-muted hover:z-text"
                }`}
              >
                出口代理绑定
              </button>
              <button
                type="button"
                onClick={() => setBulkEditTab("danger")}
                className={`pb-2 -mb-px border-b-2 cursor-pointer transition-colors ${
                  bulkEditTab === "danger"
                    ? "border-red-500 text-red-500"
                    : "border-transparent z-text-muted hover:text-red-500"
                }`}
              >
                清理与移除
              </button>
            </div>

            {/* Tab 1: 状态与健康 */}
            {bulkEditTab === "state" && (
              <div className="space-y-3 py-1">
                <div className="grid grid-cols-1 gap-2.5">
                  <div className="flex items-center justify-between p-3 rounded-xl border z-border bg-card">
                    <div>
                      <div className="font-medium flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400">
                        <Play size={13} />
                        <span>批量启用所选账号</span>
                      </div>
                      <p className="z-text-muted text-[11px] mt-0.5">
                        清除冷却与限流标记，恢复选中的 {selectedCount} 个账号为活跃在线状态，参与路由轮询。
                      </p>
                    </div>
                    <button
                      type="button"
                      disabled={Boolean(bulkOperatingAction) || updatingProxy}
                      onClick={() => handleBulkAction("enable")}
                      className="px-3 py-1.5 rounded-lg border border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 hover:bg-emerald-500/20 font-medium cursor-pointer shrink-0 disabled:opacity-50"
                    >
                      {bulkOperatingAction === "enable" ? "启用中..." : "立即启用"}
                    </button>
                  </div>

                  <div className="flex items-center justify-between p-3 rounded-xl border z-border bg-card">
                    <div>
                      <div className="font-medium flex items-center gap-1.5 text-amber-600 dark:text-amber-400">
                        <Pause size={13} />
                        <span>批量停用所选账号</span>
                      </div>
                      <p className="z-text-muted text-[11px] mt-0.5">
                        将所选账号标记为已停用，网关将不再向其分发任何聊天、生图或音频请求。
                      </p>
                    </div>
                    <button
                      type="button"
                      disabled={Boolean(bulkOperatingAction) || updatingProxy}
                      onClick={() => handleBulkAction("disable")}
                      className="px-3 py-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400 hover:bg-amber-500/20 font-medium cursor-pointer shrink-0 disabled:opacity-50"
                    >
                      {bulkOperatingAction === "disable" ? "停用中..." : "立即停用"}
                    </button>
                  </div>

                  <div className="flex items-center justify-between p-3 rounded-xl border z-border bg-card">
                    <div>
                      <div className="font-medium flex items-center gap-1.5 text-blue-600 dark:text-blue-400">
                        <Activity size={13} />
                        <span>批量探测与测活</span>
                      </div>
                      <p className="z-text-muted text-[11px] mt-0.5">
                        向所选账号并发发起上游健康检测，探测连通性并同步可用模型列表。
                      </p>
                    </div>
                    <button
                      type="button"
                      disabled={Boolean(bulkOperatingAction) || updatingProxy}
                      onClick={() => handleBulkAction("probe")}
                      className="px-3 py-1.5 rounded-lg border border-blue-500/30 bg-blue-500/10 text-blue-600 dark:text-blue-400 hover:bg-blue-500/20 font-medium cursor-pointer shrink-0 disabled:opacity-50"
                    >
                      {bulkOperatingAction === "probe" ? "测活中..." : "开始测活"}
                    </button>
                  </div>
                </div>
              </div>
            )}

            {/* Tab 2: 网络与代理 */}
            {bulkEditTab === "proxy" && (
              <div className="space-y-3 py-1">
                <div className="space-y-1.5">
                  <label className="font-medium z-text-sec block" htmlFor="bulk-proxy">
                    选择出口代理节点
                  </label>
                  <select
                    id="bulk-proxy"
                    value={bulkProxyUrl}
                    onChange={(e) => setBulkProxyUrl(e.target.value)}
                    disabled={updatingProxy || Boolean(bulkOperatingAction)}
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
                  <p className="z-text-muted text-[11px]">
                    新代理将立即应用到所选 {selectedCount} 个账号并重建底层客户端；服务重启后会按代理池重新初始化绑定。
                  </p>
                </div>

                {proxyNodes.length > 0 && !proxyNodes.some((node) => node.status === "active") && (
                  <p className="text-amber-700 dark:text-amber-400 text-xs">
                    当前没有健康代理节点，请先到代理管理页测活或更新代理池。
                  </p>
                )}

                <div className="flex justify-end pt-2">
                  <button
                    type="button"
                    disabled={
                      updatingProxy ||
                      Boolean(bulkOperatingAction) ||
                      selectedCount === 0 ||
                      Boolean(isPlannedProvider) ||
                      (proxyNodes.length > 0 && !bulkProxyUrl)
                    }
                    onClick={handleBulkProxyUpdate}
                    className="px-4 py-1.5 rounded-lg z-primary-btn font-medium cursor-pointer disabled:opacity-50"
                  >
                    {updatingProxy ? "正在更新代理..." : "应用代理到所选账号"}
                  </button>
                </div>
              </div>
            )}

            {/* Tab 3: 清理与移除 */}
            {bulkEditTab === "danger" && (
              <div className="space-y-3 py-1">
                <div className="p-3.5 rounded-xl border border-red-500/30 bg-red-500/5 space-y-2.5">
                  <div className="font-semibold text-red-600 dark:text-red-400 flex items-center gap-1.5">
                    <ShieldAlert size={14} />
                    <span>危险操作：彻底移除账号</span>
                  </div>
                  <p className="z-text-muted text-[11px] leading-relaxed">
                    从号池与持久化数据库中永久删除所选的 {selectedCount} 个账号凭据、会话历史与配置。移除后无法撤销，若需重新使用必须重新导入。
                  </p>
                  <button
                    type="button"
                    onClick={() => {
                      setShowBulkEdit(false);
                      setShowBulkDeleteConfirm(true);
                    }}
                    className="px-3.5 py-1.5 rounded-lg bg-red-600 hover:bg-red-700 text-white font-medium cursor-pointer"
                  >
                    前往确认批量移除 ({selectedCount} 个)
                  </button>
                </div>
              </div>
            )}

            <div className="flex justify-end gap-2 pt-3 border-t z-border">
              <button
                type="button"
                disabled={updatingProxy || Boolean(bulkOperatingAction)}
                onClick={() => setShowBulkEdit(false)}
                className="px-3.5 py-1.5 rounded-lg border z-border z-ghost-btn font-medium cursor-pointer"
              >
                关闭
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 批量移除二次确认弹窗 */}
      {showBulkDeleteConfirm && (
        <div
          className="fixed inset-0 z-50 bg-black/60 backdrop-blur-xs flex items-center justify-center p-4"
          onClick={() => !bulkOperatingAction && setShowBulkDeleteConfirm(false)}
        >
          <div
            className="p-surface z-text rounded-2xl max-w-md w-full p-5 shadow-2xl space-y-4 text-xs border border-red-500/30"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-full bg-red-500/10 text-red-600 dark:text-red-400 flex items-center justify-center shrink-0 border border-red-500/20">
                <Trash2 size={20} />
              </div>
              <div>
                <h3 className="text-sm font-semibold text-red-600 dark:text-red-400">
                  确认批量永久移除账号
                </h3>
                <p className="z-text-muted text-[11px] mt-0.5">
                  已选择 <span className="font-semibold text-red-500">{selectedCount}</span> 个账号，此操作不可撤销
                </p>
              </div>
            </div>

            <div className="p-3 rounded-xl bg-red-500/5 border border-red-500/15 text-red-700 dark:text-red-300 text-[11px] leading-relaxed">
              您即将从当前号池中永久移除以下选中的账号及其关联凭据，系统将从内存与数据库中彻底清理。
            </div>

            {/* 选中账号预览清单 */}
            <div className="space-y-1.5">
              <div className="font-medium z-text-muted text-[11px]">受影响账号预览：</div>
              <div className="max-h-36 overflow-y-auto rounded-xl border z-border bg-input/40 p-2 divide-y divide-border/40 font-mono text-[11px]">
                {selectedAccountsList.slice(0, 10).map((acc) => (
                  <div key={acc.id} className="py-1 px-1 flex items-center justify-between">
                    <span className="truncate max-w-[240px] text-foreground">{acc.email || acc.id}</span>
                    <span className="text-[10px] text-muted-foreground ml-2 uppercase">{acc.status}</span>
                  </div>
                ))}
                {selectedAccountsList.length > 10 && (
                  <div className="py-1 px-1 text-center text-muted-foreground italic text-[10px]">
                    ...以及其余 {selectedAccountsList.length - 10} 个账号
                  </div>
                )}
              </div>
            </div>

            <div className="flex justify-end gap-2.5 pt-3 border-t z-border">
              <button
                type="button"
                disabled={Boolean(bulkOperatingAction)}
                onClick={() => setShowBulkDeleteConfirm(false)}
                className="px-3.5 py-1.5 rounded-xl border z-border z-ghost-btn font-medium cursor-pointer"
              >
                取消
              </button>
              <button
                type="button"
                disabled={Boolean(bulkOperatingAction) || selectedCount === 0}
                onClick={() => handleBulkAction("delete")}
                className="inline-flex items-center gap-1.5 px-4 py-1.5 rounded-xl bg-red-600 hover:bg-red-700 text-white font-medium cursor-pointer disabled:opacity-50 transition-colors shadow-sm"
              >
                {bulkOperatingAction === "delete" ? (
                  <>
                    <Loader2 size={13} className="animate-spin" />
                    <span>正在移除...</span>
                  </>
                ) : (
                  <>
                    <Trash2 size={13} />
                    <span>确认移除 ({selectedCount})</span>
                  </>
                )}
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

      {/* sub2api 风格的 OAuth2 / 网页授权向导弹窗 */}
      {showOAuthGuide && (
        <div
          className="fixed inset-0 z-50 bg-black/55 backdrop-blur-xs flex items-center justify-center p-4"
          onClick={() => !oauthSubmitting && setShowOAuthGuide(false)}
        >
          <div
            className="w-full max-w-lg p-card rounded-2xl border z-border shadow-2xl p-6 space-y-4 max-h-[90vh] overflow-y-auto"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between pb-3 border-b z-border">
              <div className="flex items-center gap-2">
                <div className="p-2 bg-blue-500/10 text-blue-500 rounded-lg">
                  <KeyRound size={18} />
                </div>
                <div>
                  <h3 className="font-semibold text-sm z-text">
                    Meta AI Muse Web 授权登录助手
                  </h3>
                  <p className="text-xs z-text-muted">
                    向 sub2api 架构对齐：浏览器完成认证，快速回传 Cookie 激活号池
                  </p>
                </div>
              </div>
              <button
                type="button"
                onClick={() => !oauthSubmitting && setShowOAuthGuide(false)}
                className="p-1 rounded-lg border z-border z-ghost-btn cursor-pointer"
              >
                <X size={14} />
              </button>
            </div>

            {/* 步骤向导 */}
            <div className="space-y-3 text-xs">
              <div className="p-3 rounded-xl border border-blue-500/20 bg-blue-500/5 space-y-2">
                <div className="flex items-center justify-between font-medium text-blue-600 dark:text-blue-400">
                  <span>步骤 1：打开官方登录页面完成认证</span>
                  <a
                    href="https://muse.ai/"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg bg-blue-600 text-white hover:bg-blue-700 transition-colors"
                  >
                    <ExternalLink size={11} />
                    <span>打开 muse.ai 登录页</span>
                  </a>
                </div>
                <p className="z-text-muted">
                  在新打开的标签页中使用您的账号密码正常登录。如遇到滑块人机或邮箱验证码，直接在网页上手动完成即可。
                </p>
              </div>

              <div className="p-3 rounded-xl border z-border bg-muted/30 space-y-2">
                <div className="flex flex-wrap items-center justify-between gap-1.5 font-medium z-text">
                  <span>步骤 2：获取已登录会话 Cookie</span>
                  <div className="flex flex-wrap items-center gap-2.5">
                    <button
                      type="button"
                      disabled={extractingChrome || browserLoggingIn}
                      onClick={handleExtractLocalChromeCookie}
                      className="inline-flex items-center gap-1 text-[11px] text-emerald-600 dark:text-emerald-400 font-medium hover:underline cursor-pointer disabled:opacity-60"
                      title="直接从本机 Chrome / Edge 数据库解密提取真实的 muse.ai Cookie 并填入下方"
                    >
                      {extractingChrome ? (
                        <Loader2 size={11} className="animate-spin" />
                      ) : (
                        <Sparkles size={11} />
                      )}
                      <span>{extractingChrome ? "正在读取..." : "从本机 Chrome 一键提取"}</span>
                    </button>
                    <button
                      type="button"
                      disabled={extractingChrome || browserLoggingIn}
                      onClick={handleBrowserLoginMuseCookie}
                      className="inline-flex items-center gap-1 text-[11px] text-primary font-medium hover:underline cursor-pointer disabled:opacity-60"
                      title="弹出受控浏览器窗口登录 muse.ai，登录完成后自动捕获完整真实 Cookie（含 HttpOnly）"
                    >
                      {browserLoggingIn ? (
                        <Loader2 size={11} className="animate-spin" />
                      ) : (
                        <LogIn size={11} />
                      )}
                      <span>{browserLoggingIn ? "等待浏览器登录..." : "启动授权浏览器登录"}</span>
                    </button>
                  </div>
                </div>
                <p className="z-text-muted">
                  <strong className="z-text">请勿在 Console 执行 document.cookie</strong>（核心鉴权字段 <code className="px-1 py-0.5 rounded bg-muted font-mono">hatch_sess</code>、<code className="px-1 py-0.5 rounded bg-muted font-mono">hatch_gw</code> 受 <code className="px-1 py-0.5 rounded bg-muted font-mono">HttpOnly</code> 保护，JS 无法读取）。推荐使用「启动授权浏览器登录」自动捕获完整 Cookie；若手动获取，请在 <code className="px-1 py-0.5 rounded bg-muted font-mono">F12 → Network</code> 筛选 <code className="px-1 py-0.5 rounded bg-muted font-mono">Fetch/XHR</code> 或 <code className="px-1 py-0.5 rounded bg-muted font-mono">Doc</code>，刷新后点击任意 <code className="px-1 py-0.5 rounded bg-muted font-mono">muse.ai</code> 请求并复制 Request Headers 中的 <code className="px-1 py-0.5 rounded bg-muted font-mono">Cookie:</code> 整行值（或用 Cookie-Editor 插件 Export → Header String，完整长度约 7,000~8,000 字符）。
                </p>
              </div>

              {/* 目标账号 */}
              <div className="space-y-1.5">
                <label className="font-medium z-text-sec block">
                  目标账号邮箱 / 身份标识
                </label>
                <input
                  type="text"
                  value={oauthTargetEmail}
                  onChange={(e) => setOauthTargetEmail(e.target.value)}
                  placeholder="例如：amyreed374923@outlook.com（留空则自动建新凭据）"
                  className="w-full px-3 py-2 rounded-xl p-code-box font-mono-num text-xs focus:outline-none"
                />
              </div>

              {/* Cookie 粘贴框 */}
              <div className="space-y-1.5">
                <label className="font-medium z-text-sec block">
                  粘贴会话 Cookie 内容
                </label>
                <textarea
                  rows={4}
                  value={oauthCookieInput}
                  onChange={(e) => setOauthCookieInput(e.target.value)}
                  placeholder="粘贴包含 hatch_sess 的完整 Cookie（约 7,000~8,000 字符），例如：hatch_sess=...; hatch_gw=...; hatch_vml=...; __Host-hatch_one_tap=...;"
                  className="w-full p-2.5 rounded-xl p-code-box font-mono-num text-xs resize-y focus:outline-none"
                />
              </div>
            </div>

            <div className="flex items-center justify-end gap-2 pt-2 border-t z-border">
              <button
                type="button"
                onClick={() => setShowOAuthGuide(false)}
                disabled={oauthSubmitting}
                className="px-3.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn text-xs cursor-pointer"
              >
                取消
              </button>
              <button
                type="button"
                onClick={handleOAuthSubmit}
                disabled={oauthSubmitting || !oauthCookieInput.trim()}
                className="inline-flex items-center gap-1.5 px-4 py-1.5 rounded-xl z-primary-btn text-xs font-medium cursor-pointer disabled:opacity-50"
              >
                {oauthSubmitting ? (
                  <>
                    <Loader2 size={12} className="animate-spin" />
                    <span>正在验真并保存...</span>
                  </>
                ) : (
                  <>
                    <Check size={12} />
                    <span>确认授权并激活</span>
                  </>
                )}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 手动编辑 Cookie 弹窗 */}
      {editingCookieAccount && (
        <div
          className="fixed inset-0 z-50 bg-black/55 backdrop-blur-xs flex items-center justify-center p-4"
          onClick={() => !savingEditCookie && setEditingCookieAccount(null)}
        >
          <div
            className="p-surface z-text rounded-2xl max-w-xl w-full max-h-[90vh] overflow-y-auto p-5 shadow-2xl space-y-4 text-xs"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between pb-3 border-b z-border">
              <div>
                <h3 className="text-sm font-semibold flex items-center gap-2">
                  <Pencil size={14} className="text-emerald-500" />
                  <span>编辑 Cookie 凭据</span>
                </h3>
                <p className="text-[11px] z-text-muted mt-0.5 font-mono-num">
                  账号：{editingCookieAccount.email || editingCookieAccount.id}
                </p>
              </div>
              <button
                type="button"
                onClick={() => !savingEditCookie && setEditingCookieAccount(null)}
                className="p-1.5 rounded-lg z-ghost-btn cursor-pointer"
                aria-label="关闭"
              >
                <X size={14} />
              </button>
            </div>

            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <label className="font-medium z-text-sec">
                  完整 Cookie 字符串
                </label>
                <div className="flex items-center gap-2 text-[11px]">
                  <span className="font-mono-num z-text-muted">
                    长度：{editCookieValue.trim().length} 字符
                  </span>
                  {activeProvider === "meta_muse" && (
                    <span
                      className={`px-1.5 py-0.5 rounded font-medium ${
                        editCookieValue.includes("hatch_sess=")
                          ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
                          : "bg-amber-500/15 text-amber-600 dark:text-amber-400"
                      }`}
                    >
                      {editCookieValue.includes("hatch_sess=")
                        ? "已包含 hatch_sess"
                        : "缺少 hatch_sess"}
                    </span>
                  )}
                </div>
              </div>
              {loadingEditCookie ? (
                <div className="flex items-center justify-center py-10 z-text-muted gap-2">
                  <Loader2 size={14} className="animate-spin" />
                  <span>正在加载当前 Cookie...</span>
                </div>
              ) : (
                <textarea
                  rows={7}
                  value={editCookieValue}
                  onChange={(e) => setEditCookieValue(e.target.value)}
                  placeholder="粘贴或修改完整的 Cookie 字符串（例如：hatch_sess=...; hatch_gw=...;）"
                  className="w-full p-2.5 rounded-xl p-code-box font-mono-num text-xs resize-y focus:outline-none"
                />
              )}
              <p className="text-[11px] z-text-muted">
                保存后会立即更新该账号凭据并将其状态重置为 <code className="px-1 py-0.5 rounded bg-muted font-mono">active</code>。
              </p>
            </div>

            <div className="flex items-center justify-end gap-2 pt-2 border-t z-border">
              <button
                type="button"
                onClick={() => setEditingCookieAccount(null)}
                disabled={savingEditCookie}
                className="px-3.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn text-xs cursor-pointer"
              >
                取消
              </button>
              <button
                type="button"
                onClick={handleSaveEditCookie}
                disabled={savingEditCookie || loadingEditCookie || !editCookieValue.trim()}
                className="inline-flex items-center gap-1.5 px-4 py-1.5 rounded-xl z-primary-btn text-xs font-medium cursor-pointer disabled:opacity-50"
              >
                {savingEditCookie ? (
                  <>
                    <Loader2 size={12} className="animate-spin" />
                    <span>保存中...</span>
                  </>
                ) : (
                  <>
                    <Check size={12} />
                    <span>保存并激活</span>
                  </>
                )}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Delete Confirmation Modal */}
      {deleteTarget && (
        <div
          className="fixed inset-0 z-50 bg-black/55 backdrop-blur-xs flex items-center justify-center p-4"
          onClick={() => setDeleteTarget(null)}
        >
          <div
            className="p-surface z-text rounded-2xl max-w-sm w-full max-h-[90vh] overflow-y-auto p-5 shadow-2xl space-y-3 text-xs"
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
