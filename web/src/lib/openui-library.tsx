import React, { useEffect, useState } from "react";
import {
  createLibrary,
  defineComponent,
  useTriggerAction,
  type PromptOptions,
} from "@openuidev/react-lang";
import {
  openuiChatComponentGroups,
  openuiChatLibrary,
  openuiChatPromptOptions,
} from "@openuidev/react-ui";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  Download,
  Image as ImageIcon,
  Loader2,
  Pause,
  Play,
  RefreshCw,
  ShieldAlert,
  Sparkles,
  Trash2,
  Wrench,
} from "lucide-react";
import { z } from "zod/v4";
import {
  executeAccountAction,
  executeDiagnosticRepair,
  fetchAccounts,
  fetchDiagnostics,
  fetchOverview,
  generateImage,
  probeAllAccounts,
  type AccountView,
  type DiagnosticRepairResponse,
  type DiagnosticsReport,
  type PoolStats,
} from "../api/client";

function maskProxyHost(raw?: string): string {
  if (!raw) return "直连";
  try {
    const u = new URL(raw);
    return `${u.protocol}//${u.host}`;
  } catch {
    return raw.replace(/\/\/[^@]+@/, "//***@");
  }
}

function classifyAccountReasonBadge(acc: AccountView): {
  label: string;
  tone: "danger" | "warn" | "ok" | "muted";
} {
  const err = (acc.last_error || "").toLowerCase();
  if (
    acc.status === "banned" ||
    /account_deactivated|account deactivated|has been suspended|permanently banned|has been banned/.test(
      err
    )
  ) {
    return { label: "永久封禁", tone: "danger" };
  }
  if (
    /http 403|cloudflare|cf-ray|just a moment|sentinel|waf|turnstile/.test(err)
  ) {
    return { label: "403 风控", tone: "warn" };
  }
  if (
    acc.status === "expired" ||
    acc.renew_state === "cooldown" ||
    /http 401|token_expired|token_invalidated|unauthorized/.test(err)
  ) {
    return { label: "401 过期", tone: "warn" };
  }
  if (acc.status === "rate_limited" || acc.image_limited) {
    return {
      label:
        acc.image_limited && acc.status === "active" ? "生图限流" : "429 限流",
      tone: "warn",
    };
  }
  if (acc.status === "disabled") {
    return { label: "已停用", tone: "muted" };
  }
  return { label: "健康", tone: "ok" };
}

/**
 * 1. PoolOverviewCard — 在 OpenUI Lang 对话流中实时渲染号池 KPI 状态大盘
 */
export const PoolOverviewCard = defineComponent({
  name: "PoolOverviewCard",
  description:
    "Displays real-time AI-Web-Free account pool statistics and health metrics for a provider (e.g. chatgpt) with a live probe action button.",
  props: z.object({
    provider: z.string(),
    title: z.string().optional(),
  }),
  component: ({ props }) => {
    const providerId = props.provider || "chatgpt";
    const [stats, setStats] = useState<PoolStats | null>(null);
    const [probing, setProbing] = useState(false);
    const triggerAction = useTriggerAction();

    const loadStats = async () => {
      try {
        const res = await fetchAccounts(providerId);
        setStats(res.stats);
      } catch {
        // ignore if unavailable
      }
    };

    useEffect(() => {
      loadStats();
    }, [providerId]);

    const handleProbeAll = async () => {
      setProbing(true);
      try {
        await probeAllAccounts(providerId);
        await loadStats();
        window.dispatchEvent(new Event("pool-updated"));
      } finally {
        setProbing(false);
      }
    };

    return (
      <div className="genui-custom-card">
        <div className="genui-custom-header">
          <div>
            <span className="genui-badge-pill">Live Telemetry</span>
            <h4>{props.title || `${providerId.toUpperCase()} 号池实时健康状态`}</h4>
          </div>
          <div className="genui-header-actions">
            <button
              type="button"
              className="btn-secondary btn-sm"
              onClick={handleProbeAll}
              disabled={probing}
            >
              {probing ? (
                <Loader2 size={14} className="spin" />
              ) : (
                <Activity size={14} />
              )}
              <span>{probing ? "测活中..." : "全池测活"}</span>
            </button>
            <button
              type="button"
              className="btn-primary btn-sm"
              onClick={() =>
                triggerAction(
                  `请深入分析 ${providerId} 号池当前各账号状态并给出维护建议`
                )
              }
            >
              <Sparkles size={14} />
              <span>AI 诊断建议</span>
            </button>
          </div>
        </div>
        <div className="genui-kpi-grid">
          <div className="genui-kpi-box">
            <span className="kpi-label">号池总容量</span>
            <strong className="kpi-val">{stats ? stats.total : "--"}</strong>
            <span className="kpi-sub">
              Free {stats?.free_count ?? 0} · 访客 {stats?.noauth_count ?? 0}
            </span>
          </div>
          <div className="genui-kpi-box active">
            <span className="kpi-label">可用账号 (Active)</span>
            <strong className="kpi-val">{stats ? stats.active : "--"}</strong>
            <span className="kpi-sub">
              待登录 {stats?.pending ?? 0} · 调用 {stats?.total_calls ?? 0}
            </span>
          </div>
          <div className="genui-kpi-box warn">
            <span className="kpi-label">限流冷却 (429)</span>
            <strong className="kpi-val">
              {stats ? stats.rate_limited : "--"}
            </strong>
            <span className="kpi-sub">触发 429 自动退避恢复</span>
          </div>
          <div className="genui-kpi-box danger">
            <span className="kpi-label">过期 / 停用 / 封禁</span>
            <strong className="kpi-val">
              {stats ? stats.expired + stats.disabled + stats.banned : "--"}
            </strong>
            <span className="kpi-sub">
              失败请求 {stats?.failed_calls ?? 0}
            </span>
          </div>
        </div>
      </div>
    );
  },
});

/**
 * 2. AccountDiagnosticTable — 在 OpenUI 对话流中渲染可交互账号诊断与测活表格
 */
export const AccountDiagnosticTable = defineComponent({
  name: "AccountDiagnosticTable",
  description:
    "Renders an interactive diagnostic table of accounts in the pool with live Probe, Refresh, and Enable/Disable actions.",
  props: z.object({
    provider: z.string(),
    statusFilter: z.string().optional(),
  }),
  component: ({ props }) => {
    const providerId = props.provider || "chatgpt";
    const [accounts, setAccounts] = useState<AccountView[]>([]);
    const [loadingId, setLoadingId] = useState<string | null>(null);

    const load = async () => {
      try {
        const res = await fetchAccounts(providerId, props.statusFilter || "");
        setAccounts(res.accounts || []);
      } catch {
        // ignore
      }
    };

    useEffect(() => {
      load();
    }, [providerId, props.statusFilter]);

    const runAction = async (
      id: string,
      act: "probe" | "refresh" | "enable" | "disable"
    ) => {
      setLoadingId(`${id}:${act}`);
      try {
        await executeAccountAction(providerId, id, act);
        await load();
        window.dispatchEvent(new Event("pool-updated"));
      } finally {
        setLoadingId(null);
      }
    };

    return (
      <div className="genui-custom-card">
        <div className="genui-custom-header">
          <div>
            <span className="genui-badge-pill">Account Diagnostics</span>
            <h4>
              账号诊断列表 ({accounts.length} 个账号
              {props.statusFilter ? ` · 筛选: ${props.statusFilter}` : ""})
            </h4>
          </div>
          <button
            type="button"
            className="btn-secondary btn-sm"
            onClick={load}
          >
            <RefreshCw size={14} />
            <span>刷新</span>
          </button>
        </div>
        <div className="table-scroll">
          <table className="console-table">
            <thead>
              <tr>
                <th>账号标识</th>
                <th>类型 / 代理</th>
                <th>状态 / 根因归类</th>
                <th>生命周期 / 最近报错</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {accounts.slice(0, 12).map((acc) => {
                const reason = classifyAccountReasonBadge(acc);
                return (
                  <tr key={acc.id}>
                    <td className="mono">
                      <div>{acc.email || acc.chatgpt_account_id || acc.id}</div>
                      {acc.renew_state && (
                        <div className="diag-submeta">
                          续期状态: {acc.renew_state}
                        </div>
                      )}
                    </td>
                    <td>
                      <span className="tag-type">{acc.type}</span>
                      <div className="diag-submeta mono">
                        {maskProxyHost(acc.proxy)}
                      </div>
                    </td>
                    <td>
                      <div className="diag-status-stack">
                        <span className={`status-pill status-${acc.status}`}>
                          <span className="status-dot" />
                          {acc.status}
                        </span>
                        <span className={`diag-reason-pill diag-tone-${reason.tone}`}>
                          {reason.label}
                        </span>
                      </div>
                    </td>
                    <td>
                      <div>
                        {acc.remaining_days !== undefined
                          ? `${acc.remaining_days} 天`
                          : "免登会话"}
                      </div>
                      {acc.last_error && (
                        <div
                          className="diag-error-preview mono"
                          title={acc.last_error}
                        >
                          {acc.last_error.slice(0, 56)}
                          {acc.last_error.length > 56 ? "…" : ""}
                        </div>
                      )}
                    </td>
                    <td className="col-actions">
                      <div className="action-btn-group">
                        <button
                          type="button"
                          className="btn-secondary btn-xs"
                          disabled={loadingId === `${acc.id}:probe`}
                          onClick={() => runAction(acc.id, "probe")}
                        >
                          <Activity size={12} />
                          <span>测活</span>
                        </button>
                        <button
                          type="button"
                          className="btn-secondary btn-xs"
                          disabled={
                            loadingId === `${acc.id}:enable` ||
                            loadingId === `${acc.id}:disable`
                          }
                          onClick={() =>
                            runAction(
                              acc.id,
                              acc.status === "disabled" ? "enable" : "disable"
                            )
                          }
                        >
                          {acc.status === "disabled" ? (
                            <Play size={12} />
                          ) : (
                            <Pause size={12} />
                          )}
                          <span>{acc.status === "disabled" ? "启用" : "停用"}</span>
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
              {accounts.length === 0 && (
                <tr>
                  <td colSpan={5} className="empty-table-cell">
                    当前无匹配账号
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    );
  },
});

/**
 * 3. ImageStudioCard — 在 OpenUI 对话流中直接调用 gpt-image-2.5 生成与预览图像
 */
export const ImageStudioCard = defineComponent({
  name: "ImageStudioCard",
  description:
    "Interactive GPT-Image-2.5 generation studio card. Can generate images directly inside the conversation using gpt-image-2.5-sunburst or gpt-image-2.5-flare.",
  props: z.object({
    prompt: z.string(),
    model: z.string().optional(),
    size: z.string().optional(),
    quality: z.string().optional(),
  }),
  component: ({ props }) => {
    const [prompt, setPrompt] = useState(props.prompt || "");
    const [model, setModel] = useState(
      props.model || "gpt-image-2.5-sunburst"
    );
    const [size, setSize] = useState(props.size || "1024x1024");
    const [quality] = useState(props.quality || "hd");
    const [loading, setLoading] = useState(false);
    const [imageUrl, setImageUrl] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);

    const handleGenerate = async () => {
      if (!prompt.trim() || loading) return;
      setLoading(true);
      setError(null);
      try {
        const res = await generateImage({
          prompt,
          model,
          size,
          quality,
        });
        const item = res.data?.[0];
        if (item?.url) {
          setImageUrl(item.url);
        } else if (item?.b64_json) {
          setImageUrl(`data:image/png;base64,${item.b64_json}`);
        } else {
          setError("未返回有效图像地址");
        }
      } catch (e: any) {
        setError(e?.message || "生图请求失败");
      } finally {
        setLoading(false);
      }
    };

    return (
      <div className="genui-custom-card">
        <div className="genui-custom-header">
          <div>
            <span className="genui-badge-pill">Image Generation</span>
            <h4>GPT-Image 2.5 内嵌生图卡片</h4>
          </div>
          <select
            className="console-select"
            value={model}
            onChange={(e) => setModel(e.target.value)}
          >
            <option value="gpt-image-2.5-sunburst">
              gpt-image-2.5-sunburst (高精)
            </option>
            <option value="gpt-image-2.5-flare">
              gpt-image-2.5-flare (极速)
            </option>
            <option value="gpt-image-2.5">gpt-image-2.5 (标准)</option>
            <option value="gpt-image-2.6">gpt-image-2.6</option>
          </select>
        </div>
        <div className="inline-image-form">
          <input
            className="console-input"
            style={{ flex: 1 }}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="输入图像生成提示词..."
          />
          <select
            className="console-select"
            value={size}
            onChange={(e) => setSize(e.target.value)}
          >
            <option value="1024x1024">1:1 (1024×1024)</option>
            <option value="1792x1024">16:9 (1792×1024)</option>
            <option value="1024x1792">9:16 (1024×1792)</option>
          </select>
          <button
            type="button"
            className="btn-primary"
            onClick={handleGenerate}
            disabled={loading}
          >
            {loading ? (
              <Loader2 size={14} className="spin" />
            ) : (
              <ImageIcon size={14} />
            )}
            <span>{loading ? "生成中..." : "生成图像"}</span>
          </button>
        </div>
        {error && <div className="error-banner">{error}</div>}
        {imageUrl && (
          <div className="embedded-image-preview">
            <img src={imageUrl} alt={prompt} />
            <div className="embedded-image-actions">
              <a
                href={imageUrl}
                download="ai-web-free-image.png"
                target="_blank"
                rel="noreferrer"
                className="btn-secondary btn-xs"
              >
                <Download size={12} />
                <span>下载 / 打开原图</span>
              </a>
            </div>
          </div>
        )}
      </div>
    );
  },
});

const REPAIR_ACTION_META: Record<
  string,
  { label: string; destructive?: boolean }
> = {
  smart_heal: { label: "一键智能自愈" },
  rebind_proxies: { label: "切换健康代理" },
  renew_expired: { label: "续期过期账号" },
  probe_all: { label: "全量测活" },
  topup_free_uuid: { label: "补充匿名 UUID" },
  disable_banned: { label: "停用封禁账号" },
  delete_banned: { label: "清理封禁账号", destructive: true },
};

/**
 * 4. PlatformDiagnosisCard — 平台 5 类根因诊断与一键自愈工作卡
 */
export const PlatformDiagnosisCard = defineComponent({
  name: "PlatformDiagnosisCard",
  description:
    "Displays live 5-category platform root-cause diagnostics (account bans, 403 WAF/proxy blocks, 401 token expiry, 429 rate limits, proxy circuit-breakers) with one-click safe repairs and confirmed banned-account cleanup.",
  props: z.object({
    title: z.string().optional(),
    focus: z
      .enum([
        "all",
        "banned",
        "waf_proxy_block",
        "token_expired",
        "rate_limit",
        "proxy_breaker",
      ])
      .optional(),
  }),
  component: ({ props }) => {
    const [report, setReport] = useState<DiagnosticsReport | null>(null);
    const [loading, setLoading] = useState(true);
    const [repairingAction, setRepairingAction] = useState<string | null>(null);
    const [confirmDelete, setConfirmDelete] = useState(false);
    const [feedback, setFeedback] = useState<{
      ok: boolean;
      text: string;
    } | null>(null);

    const loadDiagnostics = async () => {
      setLoading(true);
      try {
        const res = await fetchDiagnostics();
        setReport(res);
      } catch (err) {
        setFeedback({
          ok: false,
          text:
            err instanceof Error
              ? `诊断拉取失败: ${err.message}`
              : "诊断数据拉取失败",
        });
      } finally {
        setLoading(false);
      }
    };

    useEffect(() => {
      loadDiagnostics();
      const onRefresh = () => {
        void loadDiagnostics();
      };
      window.addEventListener("pool-updated", onRefresh);
      window.addEventListener("ai-web-free:pool-updated", onRefresh);
      return () => {
        window.removeEventListener("pool-updated", onRefresh);
        window.removeEventListener("ai-web-free:pool-updated", onRefresh);
      };
    }, []);

    const triggerRepair = async (action: string, confirm = false) => {
      if (action === "delete_banned" && !confirm) {
        setConfirmDelete(true);
        return;
      }
      setRepairingAction(action);
      setFeedback(null);
      try {
        const resp: DiagnosticRepairResponse = await executeDiagnosticRepair({
          action,
          confirm,
        });
        setReport(resp.diagnostics);
        setConfirmDelete(false);
        setFeedback({ ok: true, text: resp.summary });
        window.dispatchEvent(new Event("pool-updated"));
        window.dispatchEvent(new CustomEvent("ai-web-free:pool-updated"));
      } catch (err) {
        setFeedback({
          ok: false,
          text:
            err instanceof Error ? `修复失败: ${err.message}` : "修复执行失败",
        });
      } finally {
        setRepairingAction(null);
      }
    };

    const focus = props.focus || "all";
    const causes = (report?.root_causes || []).filter(
      (item) => focus === "all" || item.category === focus
    );

    const severityClass =
      report?.overall_severity === "critical"
        ? "diag-sev-critical"
        : report?.overall_severity === "degraded"
          ? "diag-sev-degraded"
          : "diag-sev-healthy";

    return (
      <div className="genui-custom-card diag-card">
        <div className="genui-custom-header">
          <div>
            <div className="diag-title-badges">
              <span className="genui-badge-pill">Platform Diagnostics</span>
              {report && (
                <span className={`diag-score-pill ${severityClass}`}>
                  健康评分 {report.health_score}/100
                </span>
              )}
            </div>
            <h4>{props.title || "全平台异常根因诊断与自愈台"}</h4>
          </div>
          <div className="genui-header-actions">
            <button
              type="button"
              className="btn-secondary btn-sm"
              onClick={loadDiagnostics}
              disabled={loading || Boolean(repairingAction)}
            >
              <RefreshCw size={14} className={loading ? "spin" : ""} />
              <span>刷新诊断</span>
            </button>
            <button
              type="button"
              className="btn-primary btn-sm"
              disabled={Boolean(repairingAction)}
              onClick={() => triggerRepair("smart_heal")}
            >
              {repairingAction === "smart_heal" ? (
                <Loader2 size={14} className="spin" />
              ) : (
                <Wrench size={14} />
              )}
              <span>
                {repairingAction === "smart_heal" ? "自愈中..." : "一键智能自愈"}
              </span>
            </button>
          </div>
        </div>

        {report && (
          <>
            <div className={`diag-headline-banner ${severityClass}`}>
              {report.overall_severity === "healthy" ? (
                <CheckCircle2 size={15} />
              ) : (
                <AlertTriangle size={15} />
              )}
              <span>{report.headline}</span>
            </div>

            <div className="genui-kpi-grid">
              <div className="genui-kpi-tile">
                <span>可用账号</span>
                <strong className="text-ok">
                  {report.pool_stats.active}/{report.pool_stats.total}
                </strong>
              </div>
              <div className="genui-kpi-tile">
                <span>封禁 / 停用</span>
                <strong
                  className={report.pool_stats.banned > 0 ? "text-err" : ""}
                >
                  {report.pool_stats.banned} / {report.pool_stats.disabled}
                </strong>
              </div>
              <div className="genui-kpi-tile">
                <span>过期 / 限流</span>
                <strong
                  className={
                    report.pool_stats.expired + report.pool_stats.rate_limited >
                    0
                      ? "text-warn"
                      : ""
                  }
                >
                  {report.pool_stats.expired} / {report.pool_stats.rate_limited}
                </strong>
              </div>
              <div className="genui-kpi-tile">
                <span>代理健康</span>
                <strong>
                  {report.proxy_summary.active}/{report.proxy_summary.total}
                </strong>
              </div>
            </div>

            {causes.length > 0 ? (
              <div className="diag-causes-list">
                {causes.map((cause) => (
                  <div
                    key={cause.category}
                    className={`diag-cause-item diag-cause-${cause.severity}`}
                  >
                    <div className="diag-cause-top">
                      <div className="diag-cause-title-row">
                        <ShieldAlert size={14} />
                        <strong>{cause.title}</strong>
                        <span className="diag-count-chip">
                          {cause.count} 项
                        </span>
                      </div>
                      <div className="diag-cause-actions">
                        {(cause.suggested_fixes || []).map((act) => {
                          const meta = REPAIR_ACTION_META[act] || {
                            label: act,
                          };
                          return (
                            <button
                              key={act}
                              type="button"
                              className={
                                meta.destructive
                                  ? "btn-danger-ghost btn-xs"
                                  : "btn-secondary btn-xs"
                              }
                              disabled={Boolean(repairingAction)}
                              onClick={() => triggerRepair(act)}
                            >
                              {repairingAction === act && (
                                <Loader2 size={11} className="spin" />
                              )}
                              <span>{meta.label}</span>
                            </button>
                          );
                        })}
                      </div>
                    </div>
                    <p className="diag-cause-summary">{cause.summary}</p>
                    {(cause.account_emails?.length || cause.proxies?.length) && (
                      <div className="diag-chips-row">
                        {cause.account_emails?.slice(0, 4).map((em) => (
                          <span key={em} className="diag-entity-chip mono">
                            {em}
                          </span>
                        ))}
                        {cause.proxies?.slice(0, 3).map((px) => (
                          <span key={px} className="diag-entity-chip mono">
                            {maskProxyHost(px)}
                          </span>
                        ))}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            ) : (
              <div className="diag-empty-ok">
                <CheckCircle2 size={15} />
                <span>
                  {focus === "all"
                    ? "当前未检测到异常根因，所有号池与代理节点处于健康区间。"
                    : `当前未检测到 "${focus}" 类型的故障记录。`}
                </span>
              </div>
            )}

            <div className="diag-repair-toolbar">
              <button
                type="button"
                className="btn-secondary btn-xs"
                disabled={Boolean(repairingAction)}
                onClick={() => triggerRepair("rebind_proxies")}
              >
                切换健康代理
              </button>
              <button
                type="button"
                className="btn-secondary btn-xs"
                disabled={Boolean(repairingAction)}
                onClick={() => triggerRepair("renew_expired")}
              >
                续期过期账号
              </button>
              <button
                type="button"
                className="btn-secondary btn-xs"
                disabled={Boolean(repairingAction)}
                onClick={() => triggerRepair("probe_all")}
              >
                全量测活
              </button>
              <button
                type="button"
                className="btn-secondary btn-xs"
                disabled={Boolean(repairingAction)}
                onClick={() => triggerRepair("topup_free_uuid")}
              >
                补充匿名 UUID
              </button>
              {report.pool_stats.banned > 0 && (
                <>
                  <button
                    type="button"
                    className="btn-secondary btn-xs"
                    disabled={Boolean(repairingAction)}
                    onClick={() => triggerRepair("disable_banned")}
                  >
                    停用封禁账号
                  </button>
                  <button
                    type="button"
                    className="btn-danger-ghost btn-xs"
                    disabled={Boolean(repairingAction)}
                    onClick={() => triggerRepair("delete_banned")}
                  >
                    <Trash2 size={12} />
                    <span>清理删除封禁账号</span>
                  </button>
                </>
              )}
            </div>

            {confirmDelete && (
              <div className="diag-confirm-bar">
                <span>
                  确认从号池永久移除 {report.pool_stats.banned}{" "}
                  个封禁账号？该操作不可撤销。
                </span>
                <div className="diag-confirm-btns">
                  <button
                    type="button"
                    className="btn-danger btn-xs"
                    disabled={Boolean(repairingAction)}
                    onClick={() => triggerRepair("delete_banned", true)}
                  >
                    {repairingAction === "delete_banned"
                      ? "清理中..."
                      : "确认删除"}
                  </button>
                  <button
                    type="button"
                    className="btn-secondary btn-xs"
                    onClick={() => setConfirmDelete(false)}
                  >
                    取消
                  </button>
                </div>
              </div>
            )}
          </>
        )}

        {feedback && (
          <div
            className={`diag-feedback ${feedback.ok ? "is-ok" : "is-err"}`}
          >
            {feedback.text}
          </div>
        )}
      </div>
    );
  },
});

const baseCard = openuiChatLibrary.components.Card as any;
const origChildrenOptions =
  baseCard.props?.shape?.children?._zod?.def?.element?._zod?.def?.options || [];

const ExtendedCard = defineComponent({
  name: "Card",
  description: baseCard.description,
  props: z.object({
    children: z.array(
      z.union([
        ...origChildrenOptions,
        PoolOverviewCard.ref,
        AccountDiagnosticTable.ref,
        ImageStudioCard.ref,
        PlatformDiagnosisCard.ref,
      ])
    ),
    sources: baseCard.props.shape.sources,
  }),
  component: baseCard.component,
});

/**
 * 基于 @openuidev/react-ui 的 openuiChatLibrary 组合构建 aiWebFreeLibrary
 */
export const aiWebFreeLibrary = createLibrary({
  root: "Card",
  components: [
    ...Object.values(openuiChatLibrary.components).filter(
      (c) => c.name !== "Card"
    ),
    ExtendedCard,
    PoolOverviewCard,
    AccountDiagnosticTable,
    ImageStudioCard,
    PlatformDiagnosisCard,
  ],
  componentGroups: [
    ...openuiChatComponentGroups,
    {
      name: "AI-Web-Free Pool & Studio Widgets",
      components: [
        "PoolOverviewCard",
        "AccountDiagnosticTable",
        "ImageStudioCard",
        "PlatformDiagnosisCard",
      ],
    },
  ],
});

export const aiWebFreePromptOptions: PromptOptions = {
  ...openuiChatPromptOptions,
  preamble:
    "You are the AI-Web-Free Generative UI & Operations Copilot powered by OpenUI Lang. When generating OpenUI UI cards, emit valid OpenUI Lang starting with `root = Card([...])`. Combine built-in OpenUI components (Header, TextContent, BarChart, LineChart, PieChart, Table, Tabs, Form, Callout, CodeBlock) and custom AI-Web-Free components (PoolOverviewCard, AccountDiagnosticTable, ImageStudioCard, PlatformDiagnosisCard) to build clean, production-grade interactive interfaces.",
  additionalRules: [
    ...(openuiChatPromptOptions.additionalRules || []),
    'When the user asks to diagnose platform failures, pool circuit-breakers, banned accounts, 403 WAF/Cloudflare blocks, 401 expiry, or proxy meltdowns, include PlatformDiagnosisCard("全平台异常根因诊断与自愈台", "all") inside Card.',
    'When the user asks about account pool status, health metrics, or provider overview, include PoolOverviewCard("chatgpt", "ChatGPT 号池实时健康状态") and/or AccountDiagnosticTable("chatgpt") inside Card.',
    'When the user wants to generate or preview an image, include ImageStudioCard(prompt, "gpt-image-2.5-sunburst", "1024x1024", "hd") inside Card.',
  ],
  examples: [
    ...(openuiChatPromptOptions.examples || []),
    `root = Card([header, diag, table])
header = CardHeader("平台全链路异常诊断", "号池熔断、账号封禁、403 风控与一键自愈")
diag = PlatformDiagnosisCard("全平台根因诊断与修复台", "all")
table = AccountDiagnosticTable("chatgpt")`,
    `root = Card([header, overview, table])
header = CardHeader("ChatGPT Web 免费号池运行状态", "实时号池监控与账号诊断")
overview = PoolOverviewCard("chatgpt", "号池核心指标")
table = AccountDiagnosticTable("chatgpt")`,
  ],
};

export const aiWebFreeToolProvider: Record<
  string,
  (args: Record<string, unknown>) => Promise<unknown>
> = {
  get_pool_overview: async () => fetchOverview(),
  list_accounts: async (args) =>
    fetchAccounts(
      String(args.provider || "chatgpt"),
      String(args.status || ""),
      String(args.type || "")
    ),
  diagnose_platform: async () => fetchDiagnostics(),
  repair_platform: async (args) =>
    executeDiagnosticRepair({
      action: String(args.action || "smart_heal"),
      confirm: Boolean(args.confirm),
      provider: args.provider ? String(args.provider) : "chatgpt",
    }),
};

export function getOpenUISystemPrompt(): string {
  return aiWebFreeLibrary.prompt(aiWebFreePromptOptions);
}
