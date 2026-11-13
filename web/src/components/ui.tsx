import React, { Component, useEffect, useId, useMemo, useState } from "react";
import {
  AlertCircle,
  Check,
  CheckCircle2,
  Copy,
  Download,
  Info,
  KeyRound,
  Loader2,
  Lock,
  Search,
  Sparkles,
  X,
} from "lucide-react";
import {
  exportAccounts,
  fetchAuthStatus,
  setStoredAdminToken,
  type AuditEntry,
} from "../api/client";

export type RouteKey =
  | "overview"
  | "pools"
  | "network"
  | "logs"
  | "chat"
  | "image";

export interface NavItemConfig {
  id: RouteKey;
  group: "ops" | "studio";
  label: string;
  hash: string;
  description: string;
}

export const NAV_ITEMS: NavItemConfig[] = [
  {
    id: "overview",
    group: "ops",
    label: "网关总览",
    hash: "#/overview",
    description: "号池可用率、卡密队列、代理健康度与近期自愈概况",
  },
  {
    id: "pools",
    group: "ops",
    label: "号池与供应商",
    hash: "#/pools",
    description: "分级冷却状态、批量导入导出与 2FA 实时动态码",
  },
  {
    id: "network",
    group: "ops",
    label: "代理与模型目录",
    hash: "#/network",
    description: "粘性代理矩阵、测活校准与 /v1 多语言模型目录",
  },
  {
    id: "logs",
    group: "ops",
    label: "链路审计日志",
    hash: "#/logs",
    description: "内存环形缓冲追踪、Failover 切号 Span 与异常分析",
  },
  {
    id: "chat",
    group: "studio",
    label: "对话 · OpenUI",
    hash: "#/chat",
    description: "Thesys OpenUI 声明式流式界面生成与 IndexedDB 会话持久化",
  },
  {
    id: "image",
    group: "studio",
    label: "图像创作工坊",
    hash: "#/image",
    description: "GPT-Image 文生图与参考图编辑工作台（IndexedDB 本地画廊）",
  },
];

export function formatRemainingCountdown(
  isoString?: string,
  nowMs: number = Date.now()
): string {
  if (!isoString) return "";
  const target = Date.parse(isoString);
  if (Number.isNaN(target) || target <= nowMs) return "";
  const diffSec = Math.max(0, Math.floor((target - nowMs) / 1000));
  if (diffSec < 60) return `${diffSec}s`;
  const mins = Math.floor(diffSec / 60);
  const secs = diffSec % 60;
  if (mins < 60) return `${mins}m ${String(secs).padStart(2, "0")}s`;
  const hours = Math.floor(mins / 60);
  const remMins = mins % 60;
  return `${hours}h ${remMins}m`;
}

export function formatClockTime(isoString?: string): string {
  if (!isoString) return "--";
  const d = new Date(isoString);
  if (Number.isNaN(d.getTime())) return isoString;
  return d.toLocaleTimeString("zh-CN", { hour12: false });
}

export function triggerTextDownload(
  filename: string,
  content: string,
  contentType = "text/plain;charset=utf-8"
): void {
  const blob = new Blob([content], { type: contentType });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

const STATUS_STYLE_MAP: Record<
  string,
  { label: string; cls: string; dot: string }
> = {
  active: { label: "在线可用", cls: "z-pill-success", dot: "status-dot-green" },
  image_limited: {
    label: "生图冷却",
    cls: "z-pill-warn",
    dot: "status-dot-amber",
  },
  rate_limited: {
    label: "429 全局限流",
    cls: "z-pill-danger",
    dot: "status-dot-red",
  },
  pending: {
    label: "待登录激活",
    cls: "z-pill-warn",
    dot: "status-dot-amber",
  },
  expired: {
    label: "Token 过期",
    cls: "z-pill-danger",
    dot: "status-dot-red",
  },
  disabled: {
    label: "已停用",
    cls: "z-muted z-border z-text-muted",
    dot: "status-dot-muted",
  },
  banned: { label: "已封禁", cls: "z-pill-danger", dot: "status-dot-red" },
  cooling: { label: "熔断冷却", cls: "z-pill-danger", dot: "status-dot-red" },
  unknown: {
    label: "未探测",
    cls: "z-muted z-border z-text-muted",
    dot: "status-dot-muted",
  },
};

export const StatusBadge: React.FC<{
  status: string;
  imageLimited?: boolean;
  renewState?: string;
}> = ({ status, imageLimited, renewState }) => {
  const effectiveKey =
    imageLimited && status === "active" ? "image_limited" : status;
  const spec = STATUS_STYLE_MAP[effectiveKey] || {
    label: status || "未知",
    cls: "z-muted z-border z-text-muted",
    dot: "status-dot-muted",
  };
  return (
    <div className="inline-flex flex-wrap items-center gap-1.5">
      <span
        className={`inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full border text-xs font-medium whitespace-nowrap ${spec.cls}`}
      >
        <span className={spec.dot} />
        <span>{spec.label}</span>
      </span>
      {(renewState === "queued" || renewState === "running") && (
        <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full border z-pill-info text-xs font-medium whitespace-nowrap">
          <Loader2 size={11} className="animate-spin" />
          <span>{renewState === "running" ? "卡密续期中" : "续期排队"}</span>
        </span>
      )}
      {renewState === "failed" && (
        <span className="inline-flex items-center px-2 py-0.5 rounded-full border z-pill-danger text-xs font-medium whitespace-nowrap">
          续期退避
        </span>
      )}
    </div>
  );
};

export const NoticeBanner: React.FC<{
  type: "info" | "success" | "error";
  text: string;
  onClose?: () => void;
}> = ({ type, text, onClose }) => {
  const cls =
    type === "error"
      ? "z-pill-danger"
      : type === "success"
        ? "z-pill-success"
        : "z-pill-info";
  const Icon =
    type === "error" ? AlertCircle : type === "success" ? CheckCircle2 : Info;
  return (
    <div
      className={`flex items-center justify-between gap-3 px-3.5 py-2.5 rounded-xl border text-xs ${cls}`}
    >
      <div className="flex items-center gap-2 min-w-0">
        <Icon size={14} className="shrink-0" />
        <span className="break-words">{text}</span>
      </div>
      {onClose && (
        <button
          type="button"
          onClick={onClose}
          className="p-0.5 rounded opacity-80 hover:opacity-100 cursor-pointer shrink-0"
          aria-label="关闭通知"
        >
          <X size={13} />
        </button>
      )}
    </div>
  );
};

/** Compact micro-sparkline SVG for KPI telemetry cards */
export const SparklineSVG: React.FC<{
  values: number[];
  color?: string;
  width?: number;
  height?: number;
}> = ({ values, color = "#10b981", width = 88, height = 28 }) => {
  const gradId = useId();
  const points = useMemo(() => {
    const clean =
      values.length >= 2 ? values : [values[0] ?? 50, values[0] ?? 50];
    const min = Math.min(...clean);
    const max = Math.max(...clean);
    const span = max - min || 1;
    const pad = 4;
    return clean.map((v, i) => {
      const x = (i / (clean.length - 1)) * width;
      const y = height - pad - ((v - min) / span) * (height - pad * 2);
      return [Number(x.toFixed(1)), Number(y.toFixed(1))] as const;
    });
  }, [values, width, height]);

  const polyline = points.map((p) => `${p[0]},${p[1]}`).join(" ");
  const area = `M0,${height} L${points.map((p) => `${p[0]},${p[1]}`).join(" L")} L${width},${height} Z`;

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      fill="none"
      className="shrink-0 overflow-visible"
      aria-hidden="true"
    >
      <defs>
        <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.28" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={area} fill={`url(#${gradId})`} />
      <polyline
        fill="none"
        stroke={color}
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
        points={polyline}
      />
    </svg>
  );
};

/** Smooth dual-channel SVG area chart derived from recent audit log buckets */
export const DualThroughputChart: React.FC<{
  primarySeries: number[];
  secondarySeries: number[];
  primaryLabel: string;
  secondaryLabel: string;
  rightHint?: string;
}> = ({
  primarySeries,
  secondarySeries,
  primaryLabel,
  secondaryLabel,
  rightHint,
}) => {
  const g1 = useId();
  const g2 = useId();
  const w = 600;
  const h = 112;

  const buildPath = (arr: number[], maxVal: number) => {
    const safe = arr.length >= 2 ? arr : [0, 0];
    const pts = safe.map((v, idx) => {
      const x = (idx / (safe.length - 1)) * w;
      const y = h - 10 - (Math.max(0, v) / maxVal) * (h - 24);
      return [x, y] as const;
    });
    let line = `M${pts[0][0].toFixed(1)},${pts[0][1].toFixed(1)}`;
    for (let i = 1; i < pts.length; i++) {
      const prev = pts[i - 1];
      const curr = pts[i];
      const cx = ((prev[0] + curr[0]) / 2).toFixed(1);
      line += ` Q${prev[0].toFixed(1)},${prev[1].toFixed(1)} ${cx},${(
        (prev[1] + curr[1]) /
        2
      ).toFixed(1)}`;
      if (i === pts.length - 1) {
        line += ` T${curr[0].toFixed(1)},${curr[1].toFixed(1)}`;
      }
    }
    const area = `${line} L${w},${h} L0,${h} Z`;
    return { line, area };
  };

  const globalMax = Math.max(1, ...primarySeries, ...secondarySeries);
  const s1 = buildPath(primarySeries, globalMax);
  const s2 = buildPath(secondarySeries, globalMax);

  return (
    <div
      className="p-3.5 rounded-xl"
      style={{
        background: "var(--bg-input)",
        border: "1px solid var(--hairline)",
      }}
    >
      <div className="flex flex-wrap items-center justify-between gap-2 mb-2 text-xs">
        <div className="flex flex-wrap items-center gap-4">
          <span
            className="inline-flex items-center gap-1.5"
            style={{ color: "var(--text-secondary)" }}
          >
            <span className="status-dot-green" />
            <span>{primaryLabel}</span>
          </span>
          <span
            className="inline-flex items-center gap-1.5"
            style={{ color: "var(--text-secondary)" }}
          >
            <span
              className="w-2 h-2 rounded-full inline-block"
              style={{
                background: "#38bdf8",
                boxShadow: "0 0 8px rgba(56,189,248,0.5)",
              }}
            />
            <span>{secondaryLabel}</span>
          </span>
        </div>
        {rightHint && (
          <span
            className="font-mono-num"
            style={{ color: "var(--text-muted)" }}
          >
            {rightHint}
          </span>
        )}
      </div>

      <svg
        viewBox={`0 0 ${w} ${h}`}
        className="w-full h-24 overflow-visible"
        preserveAspectRatio="none"
        aria-label="吞吐趋势图"
      >
        <defs>
          <linearGradient id={g1} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#10b981" stopOpacity="0.24" />
            <stop offset="100%" stopColor="#10b981" stopOpacity="0" />
          </linearGradient>
          <linearGradient id={g2} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#38bdf8" stopOpacity="0.18" />
            <stop offset="100%" stopColor="#38bdf8" stopOpacity="0" />
          </linearGradient>
        </defs>
        <line
          x1="0"
          y1="28"
          x2={w}
          y2="28"
          stroke="var(--hairline)"
          strokeDasharray="3 4"
        />
        <line
          x1="0"
          y1="62"
          x2={w}
          y2="62"
          stroke="var(--hairline)"
          strokeDasharray="3 4"
        />
        <line
          x1="0"
          y1="96"
          x2={w}
          y2="96"
          stroke="var(--hairline)"
          strokeDasharray="3 4"
        />
        <path d={s1.area} fill={`url(#${g1})`} />
        <path d={s1.line} fill="none" stroke="#10b981" strokeWidth="2" />
        <path d={s2.area} fill={`url(#${g2})`} />
        <path d={s2.line} fill="none" stroke="#38bdf8" strokeWidth="1.75" />
      </svg>
    </div>
  );
};

interface SpanStep {
  title: string;
  detail: string;
  badge: string;
  badgeCls: string;
}

function buildTraceSpans(entry: AuditEntry): SpanStep[] {
  if (entry.kind === "event") {
    return [
      {
        title: "1. 网关运维事件触发",
        detail: `${entry.account_email || "System"} · ${entry.note || entry.path || "PoolEvent"}`,
        badge: entry.error ? "异常" : "Event",
        badgeCls: entry.error ? "z-pill-danger" : "z-pill-info",
      },
    ];
  }

  const hops = (entry.failover_trace || "")
    .split("->")
    .map((s) => s.trim())
    .filter(Boolean);

  if (hops.length > 1) {
    return hops.map((hop, idx) => {
      const isLast = idx === hops.length - 1;
      const isErr = !isLast || Boolean(entry.error) || (entry.status ?? 200) >= 400;
      return {
        title:
          idx === 0
            ? `1. 初始账号派发`
            : isLast
              ? `${idx + 1}. 无损 Failover 终跳`
              : `${idx + 1}. 自动切号重试`,
        detail: hop,
        badge: isErr ? "切号 / 重试" : `${entry.status || 200} OK`,
        badgeCls: isErr ? "z-pill-warn" : "z-pill-success",
      };
    });
  }

  const ok = !entry.status || entry.status < 400;
  return [
    {
      title: "1. 账号选取与 TLS 粘性路由",
      detail: `${entry.account_email || entry.account_id || "匿名会话"}${
        entry.proxy ? ` · via ${entry.proxy}` : " · 直连"
      }`,
      badge: `${entry.attempt_count || 1} 次尝试`,
      badgeCls: "z-pill-info",
    },
    {
      title: ok ? "2. 上游响应成功" : "2. 响应异常反馈",
      detail:
        entry.error ||
        entry.failover_trace ||
        entry.note ||
        `HTTP ${entry.status || 200} · 耗时 ${entry.duration_ms ?? 0}ms`,
      badge: ok ? `${entry.status || 200} OK` : `HTTP ${entry.status || 500}`,
      badgeCls: ok ? "z-pill-success" : "z-pill-danger",
    },
  ];
}

export const TraceInspectorPanel: React.FC<{
  entry: AuditEntry | null;
}> = ({ entry }) => {
  if (!entry) {
    return (
      <div className="p-card rounded-2xl p-5 text-xs z-text-muted text-center">
        在左侧点击任意一条请求或运维记录，即可检视完整 Trace 与 Failover Span 轨迹。
      </div>
    );
  }

  const ok = !entry.status || entry.status < 400;
  const spans = buildTraceSpans(entry);

  return (
    <div className="p-card rounded-2xl p-4 space-y-4 text-xs">
      <div className="flex items-start justify-between gap-2 pb-3 border-b z-border">
        <div className="min-w-0">
          <div className="font-mono-num text-[11px] uppercase tracking-wider z-text-muted">
            TRACE INSPECTOR · #{entry.id}
          </div>
          <h3 className="text-sm font-semibold font-mono-num mt-0.5 truncate">
            {entry.method ? `${entry.method} ` : ""}
            {entry.path || "--"}
          </h3>
          <div className="font-mono-num z-text-muted mt-0.5">
            {formatClockTime(entry.timestamp)} · {entry.kind}
          </div>
        </div>
        {entry.kind === "event" ? (
          <span className="px-2 py-0.5 rounded-full border z-pill-info font-mono-num shrink-0">
            EVENT
          </span>
        ) : (
          <span
            className={`px-2 py-0.5 rounded-full border font-mono-num shrink-0 ${
              ok ? "z-pill-success" : "z-pill-danger"
            }`}
          >
            {entry.status || 200} {ok ? "OK" : "ERR"}
          </span>
        )}
      </div>

      <div className="grid grid-cols-2 gap-2.5">
        <div
          className="p-2.5 rounded-xl"
          style={{
            background: "var(--bg-input)",
            border: "1px solid var(--hairline)",
          }}
        >
          <div className="z-text-muted">目标模型</div>
          <div className="font-mono-num font-semibold mt-0.5 truncate">
            {entry.model || entry.kind || "--"}
          </div>
        </div>
        <div
          className="p-2.5 rounded-xl"
          style={{
            background: "var(--bg-input)",
            border: "1px solid var(--hairline)",
          }}
        >
          <div className="z-text-muted">总耗时 / 尝试</div>
          <div className="font-mono-num font-semibold mt-0.5">
            {entry.duration_ms !== undefined ? `${entry.duration_ms}ms` : "--"}{" "}
            · {entry.attempt_count || 1} 次
          </div>
        </div>
        <div
          className="col-span-2 p-2.5 rounded-xl"
          style={{
            background: "var(--bg-input)",
            border: "1px solid var(--hairline)",
          }}
        >
          <div className="z-text-muted">最终命中账号 &amp; 粘性代理</div>
          <div className="font-mono-num font-medium mt-0.5 break-all">
            {entry.account_email || entry.account_id || "系统 / 匿名"}
          </div>
          <div className="font-mono-num z-text-muted mt-0.5 break-all">
            {entry.proxy || "直连出口"}
          </div>
        </div>
      </div>

      <div className="space-y-2">
        <div className="font-medium z-text-sec">执行 Span 轨迹</div>
        {spans.map((step, i) => (
          <div
            key={`${step.title}-${i}`}
            className="p-2.5 rounded-xl space-y-1"
            style={{
              background: "var(--bg-input)",
              border: "1px solid var(--hairline)",
            }}
          >
            <div className="flex items-center justify-between gap-2">
              <span className="font-medium">{step.title}</span>
              <span
                className={`px-1.5 py-0.5 rounded border font-mono-num text-[11px] ${step.badgeCls}`}
              >
                {step.badge}
              </span>
            </div>
            <div className="font-mono-num z-text-muted break-all">
              {step.detail}
            </div>
          </div>
        ))}
      </div>

      {entry.error && (
        <div className="p-3 rounded-xl border z-pill-danger space-y-1">
          <div className="font-semibold">异常详情</div>
          <div className="font-mono-num break-all">{entry.error}</div>
        </div>
      )}
    </div>
  );
};

/** Animated SVG ring 2FA TOTP Modal */
export const TotpRingModal: React.FC<{
  info: {
    accountId: string;
    email: string;
    code: string;
    remainingSeconds: number;
  } | null;
  copied: boolean;
  onCopy: () => void;
  onClose: () => void;
}> = ({ info, copied, onCopy, onClose }) => {
  if (!info) return null;

  const circumference = 175.9;
  const pct = Math.max(0, Math.min(30, info.remainingSeconds)) / 30;
  const offset = circumference * (1 - pct);

  return (
    <div
      className="fixed inset-0 z-50 bg-black/55 backdrop-blur-xs flex items-center justify-center p-4"
      onClick={onClose}
    >
      <div
        className="p-surface z-text rounded-2xl max-w-sm w-full p-5 shadow-2xl space-y-4 text-xs"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-2 pb-3 border-b z-border">
          <div>
            <h3 className="text-sm font-semibold">实时 2FA TOTP 动态码</h3>
            <p className="font-mono-num z-text-muted mt-0.5 truncate max-w-[260px]">
              {info.email}
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-lg border z-border z-ghost-btn cursor-pointer"
            aria-label="关闭 TOTP 弹窗"
          >
            <X size={14} />
          </button>
        </div>

        <div
          className="p-5 rounded-xl flex flex-col items-center gap-3"
          style={{
            background: "var(--bg-input)",
            border: "1px solid var(--hairline)",
          }}
        >
          <div className="relative w-16 h-16 flex items-center justify-center">
            <svg width="64" height="64" className="-rotate-90">
              <circle
                cx="32"
                cy="32"
                r="28"
                stroke="var(--hairline-strong)"
                strokeWidth="3.5"
                fill="none"
              />
              <circle
                cx="32"
                cy="32"
                r="28"
                stroke="#10b981"
                strokeWidth="3.5"
                fill="none"
                strokeLinecap="round"
                strokeDasharray={circumference}
                strokeDashoffset={offset}
                style={{ transition: "stroke-dashoffset 0.8s linear" }}
              />
            </svg>
            <span className="absolute font-mono-num font-semibold text-sm">
              {info.remainingSeconds}s
            </span>
          </div>

          <div className="font-mono-num text-3xl font-bold tracking-[0.26em] pl-[0.26em]">
            {info.code}
          </div>
          <div className="z-text-muted">
            RFC 6238 SHA-1 实时对齐（归零自动更新）
          </div>
        </div>

        <button
          type="button"
          onClick={onCopy}
          className="w-full py-2.5 rounded-xl z-primary-btn font-medium inline-flex items-center justify-center gap-1.5 cursor-pointer"
        >
          {copied ? <Check size={14} /> : <Copy size={14} />}
          <span>{copied ? "已复制 6 位动态码" : "复制 6 位动态码"}</span>
        </button>
      </div>
    </div>
  );
};

export const ExportPoolModal: React.FC<{
  open: boolean;
  provider?: string;
  onClose: () => void;
}> = ({ open, provider = "chatgpt", onClose }) => {
  const [includeSecrets, setIncludeSecrets] = useState(false);
  const [loadingFmt, setLoadingFmt] = useState<string | null>(null);
  const [preview, setPreview] = useState<{
    filename: string;
    content: string;
    redacted: boolean;
  } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!open) {
      setPreview(null);
      setError(null);
      setCopied(false);
    }
  }, [open]);

  if (!open) return null;

  const handleRun = async (
    format: "sub2api_json" | "access_tokens" | "kami",
    download: boolean
  ) => {
    setLoadingFmt(`${format}:${download ? "dl" : "prev"}`);
    setError(null);
    try {
      const res = await exportAccounts({
        provider,
        format,
        includeSecrets,
        download,
      });
      setPreview({
        filename: res.filename,
        content: res.content,
        redacted: res.redacted,
      });
      if (download) {
        triggerTextDownload(res.filename, res.content, res.contentType);
      }
    } catch (err: any) {
      setError(err?.message || "导出失败");
    } finally {
      setLoadingFmt(null);
    }
  };

  const handleCopy = async () => {
    if (!preview) return;
    try {
      await navigator.clipboard.writeText(preview.content);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      // ignore
    }
  };

  const options: Array<{
    format: "sub2api_json" | "access_tokens" | "kami";
    title: string;
    desc: string;
    actionLabel: string;
  }> = [
    {
      format: "sub2api_json",
      title: "Sub2API 标准数据包 (sub2api_json)",
      desc: "兼容 Sub2API 导入，包含 access_token、过期时间与账号扩展元数据。",
      actionLabel: "下载 JSON",
    },
    {
      format: "access_tokens",
      title: "Access Tokens 纯净行 (access_tokens.txt)",
      desc: "每行一条有效 JWT，可直接同步给其他反代节点或 CLI。",
      actionLabel: "导出 TXT",
    },
    {
      format: "kami",
      title: "干净卡密清单 (kami_accounts.txt)",
      desc: "统一格式为 email----password----totp_secret（支持默认脱敏）。",
      actionLabel: "导出卡密",
    },
  ];

  return (
    <div
      className="fixed inset-0 z-50 bg-black/55 backdrop-blur-xs flex items-center justify-center p-4"
      onClick={onClose}
    >
      <div
        className="p-surface z-text rounded-2xl max-w-xl w-full p-5 shadow-2xl space-y-4 text-xs"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-3 pb-3 border-b z-border">
          <div>
            <h3 className="text-sm font-semibold">
              导出号池配置 · {provider.toUpperCase()}
            </h3>
            <p className="z-text-muted mt-0.5">
              默认自动脱敏密码与 TOTP 密钥，若需跨机迁移可勾选明文导出。
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-lg border z-border z-ghost-btn cursor-pointer"
            aria-label="关闭导出弹窗"
          >
            <X size={14} />
          </button>
        </div>

        <label className="flex items-center gap-2 px-3 py-2.5 rounded-xl border z-border z-muted cursor-pointer select-none">
          <input
            type="checkbox"
            checked={includeSecrets}
            onChange={(e) => setIncludeSecrets(e.target.checked)}
            className="accent-zinc-900 dark:accent-zinc-100"
          />
          <span>包含明文密码与 2FA TOTP 密钥（include_secrets=true）</span>
        </label>

        {error && <NoticeBanner type="error" text={error} />}

        <div className="space-y-2">
          {options.map((item) => (
            <div
              key={item.format}
              className="p-3 rounded-xl border z-border flex flex-wrap items-center justify-between gap-3"
              style={{ background: "var(--bg-input)" }}
            >
              <div className="min-w-0 flex-1">
                <div className="font-semibold">{item.title}</div>
                <div className="z-text-muted mt-0.5">{item.desc}</div>
              </div>
              <div className="flex items-center gap-1.5 shrink-0">
                <button
                  type="button"
                  disabled={loadingFmt !== null}
                  onClick={() => handleRun(item.format, false)}
                  className="px-2.5 py-1.5 rounded-lg border z-border z-ghost-btn font-medium cursor-pointer"
                >
                  {loadingFmt === `${item.format}:prev` ? "加载中..." : "预览"}
                </button>
                <button
                  type="button"
                  disabled={loadingFmt !== null}
                  onClick={() => handleRun(item.format, true)}
                  className="inline-flex items-center gap-1 px-3 py-1.5 rounded-lg z-primary-btn font-medium cursor-pointer"
                >
                  <Download size={12} />
                  <span>
                    {loadingFmt === `${item.format}:dl`
                      ? "导出中..."
                      : item.actionLabel}
                  </span>
                </button>
              </div>
            </div>
          ))}
        </div>

        {preview && (
          <div className="space-y-1.5 pt-1 border-t z-border">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <span className="font-mono-num font-semibold">
                  {preview.filename}
                </span>
                {preview.redacted && (
                  <span className="px-2 py-0.5 rounded-full border z-pill-info">
                    密钥已脱敏
                  </span>
                )}
              </div>
              <button
                type="button"
                onClick={handleCopy}
                className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg border z-border z-ghost-btn cursor-pointer"
              >
                {copied ? <Check size={12} /> : <Copy size={12} />}
                <span>{copied ? "已复制" : "复制内容"}</span>
              </button>
            </div>
            <pre className="p-3 rounded-xl p-code-box font-mono-num max-h-44 overflow-auto text-xs leading-relaxed">
              {preview.content || "（当前号池无该格式数据）"}
            </pre>
          </div>
        )}
      </div>
    </div>
  );
};

export const AdminLockOverlay: React.FC<{
  open: boolean;
  onUnlocked: () => void;
}> = ({ open, onUnlocked }) => {
  const [token, setToken] = useState("");
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!open) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = token.trim();
    if (!trimmed) {
      setError("请输入管理员 ADMIN_TOKEN");
      return;
    }
    setChecking(true);
    setError(null);
    try {
      const status = await fetchAuthStatus(trimmed);
      if (status.unlocked) {
        setStoredAdminToken(trimmed);
        setToken("");
        onUnlocked();
      } else {
        setError("ADMIN_TOKEN 验证未通过，请检查环境变量配置");
      }
    } catch (err: any) {
      setError(err?.message || "校验失败");
    } finally {
      setChecking(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 bg-black/55 backdrop-blur-sm flex items-center justify-center p-4">
      <form
        onSubmit={handleSubmit}
        className="p-surface z-text rounded-2xl p-6 max-w-sm w-full shadow-2xl space-y-4 text-xs"
      >
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-xl z-muted border z-border flex items-center justify-center shrink-0">
            <Lock size={18} />
          </div>
          <div>
            <h2 className="text-sm font-semibold">控制台已启用安全锁</h2>
            <p className="z-text-muted mt-0.5">
              服务端配置了 ADMIN_TOKEN，请输入密钥以解锁 /admin/api/v1 接口。
            </p>
          </div>
        </div>

        {error && <NoticeBanner type="error" text={error} />}

        <div className="space-y-1.5">
          <label className="font-medium z-text-sec flex items-center gap-1.5">
            <KeyRound size={12} />
            <span>管理员令牌 (ADMIN_TOKEN)</span>
          </label>
          <input
            type="password"
            autoFocus
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="输入 X-Admin-Token..."
            className="w-full px-3 py-2 rounded-xl p-input font-mono-num text-xs"
          />
        </div>

        <button
          type="submit"
          disabled={checking}
          className="w-full py-2.5 rounded-xl z-primary-btn font-medium inline-flex items-center justify-center gap-1.5 cursor-pointer"
        >
          {checking && <Loader2 size={14} className="animate-spin" />}
          <span>{checking ? "验证中..." : "验证并解锁控制台"}</span>
        </button>
      </form>
    </div>
  );
};

export const CommandPaletteModal: React.FC<{
  open: boolean;
  onClose: () => void;
  onNavigate: (route: RouteKey) => void;
  onProbeProxies: () => void;
  onToggleTheme: () => void;
  onOpenExport: () => void;
}> = ({
  open,
  onClose,
  onNavigate,
  onProbeProxies,
  onToggleTheme,
  onOpenExport,
}) => {
  const [query, setQuery] = useState("");

  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  const actions = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list: Array<{
      id: string;
      group: string;
      label: string;
      hint: string;
      run: () => void;
    }> = [
      ...NAV_ITEMS.map((item) => ({
        id: `nav-${item.id}`,
        group: "页面跳转",
        label: item.label,
        hint: item.hash,
        run: () => {
          onNavigate(item.id);
          onClose();
        },
      })),
      {
        id: "act-probe-proxies",
        group: "快捷运维",
        label: "跳转代理页面并触发全量节点测活",
        hint: "/proxies/probe",
        run: () => {
          onClose();
          onProbeProxies();
        },
      },
      {
        id: "act-export-pool",
        group: "快捷运维",
        label: "导出号池配置（Sub2API JSON / Tokens / 卡密）",
        hint: "/export",
        run: () => {
          onClose();
          onOpenExport();
        },
      },
      {
        id: "act-toggle-theme",
        group: "系统设置",
        label: "切换 Obsidian 暗色 / Porcelain 浅色主题",
        hint: "Theme",
        run: () => {
          onToggleTheme();
          onClose();
        },
      },
    ];
    if (!q) return list;
    return list.filter(
      (item) =>
        item.label.toLowerCase().includes(q) ||
        item.hint.toLowerCase().includes(q) ||
        item.group.toLowerCase().includes(q)
    );
  }, [query, onClose, onNavigate, onOpenExport, onProbeProxies, onToggleTheme]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 bg-black/55 backdrop-blur-xs flex items-start justify-center pt-20 p-4"
      onClick={onClose}
    >
      <div
        className="p-surface z-text rounded-2xl max-w-lg w-full shadow-2xl overflow-hidden text-xs"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2.5 px-4 py-3 border-b z-border">
          <Search size={15} className="z-text-muted shrink-0" />
          <input
            type="text"
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") onClose();
              if (e.key === "Enter" && actions.length > 0) {
                e.preventDefault();
                actions[0].run();
              }
            }}
            placeholder="搜索视图、#/路由或快捷运维指令..."
            className="w-full bg-transparent text-xs focus:outline-none"
          />
          <button
            type="button"
            onClick={onClose}
            className="px-2 py-0.5 rounded-md border z-border z-text-muted font-mono-num cursor-pointer"
          >
            ESC
          </button>
        </div>

        <div className="max-h-80 overflow-y-auto p-2 space-y-1">
          {actions.map((item) => (
            <button
              key={item.id}
              type="button"
              onClick={item.run}
              className="w-full flex items-center justify-between gap-2 px-3 py-2.5 rounded-xl z-row-hover text-left cursor-pointer"
            >
              <div className="flex items-center gap-2.5 min-w-0">
                <Sparkles size={13} className="z-text-muted shrink-0" />
                <span className="truncate font-medium">{item.label}</span>
              </div>
              <span className="font-mono-num z-text-muted shrink-0">
                {item.hint}
              </span>
            </button>
          ))}
          {actions.length === 0 && (
            <div className="py-8 text-center z-text-muted">未匹配到相关命令</div>
          )}
        </div>
      </div>
    </div>
  );
};

export const LogDetailModal: React.FC<{
  entry: AuditEntry | null;
  onClose: () => void;
}> = ({ entry, onClose }) => {
  if (!entry) return null;

  return (
    <div
      className="fixed inset-0 z-50 bg-black/55 backdrop-blur-xs flex items-center justify-center p-4"
      onClick={onClose}
    >
      <div
        className="max-w-lg w-full relative"
        onClick={(e) => e.stopPropagation()}
      >
        <button
          type="button"
          onClick={onClose}
          className="absolute top-3.5 right-3.5 z-10 p-1 rounded-lg border z-border z-surface z-ghost-btn cursor-pointer"
          aria-label="关闭 Trace 详情"
        >
          <X size={14} />
        </button>
        <TraceInspectorPanel entry={entry} />
      </div>
    </div>
  );
};

export class ErrorBoundary extends Component<
  { children: React.ReactNode },
  { hasError: boolean; message: string }
> {
  constructor(props: { children: React.ReactNode }) {
    super(props);
    this.state = { hasError: false, message: "" };
  }

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, message: error?.message || "组件渲染异常" };
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="p-6 max-w-md mx-auto mt-10 rounded-2xl p-surface space-y-3 text-xs">
          <div className="font-semibold text-sm">页面模块发生渲染异常</div>
          <p className="z-text-muted">{this.state.message}</p>
          <button
            type="button"
            onClick={() => this.setState({ hasError: false, message: "" })}
            className="px-3.5 py-2 rounded-xl z-primary-btn font-medium cursor-pointer"
          >
            重新加载视图
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
