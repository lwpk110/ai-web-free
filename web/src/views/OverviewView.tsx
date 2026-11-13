import React, { useEffect, useMemo, useState } from "react";
import {
  Activity,
  ArrowRight,
  Check,
  Copy,
  Download,
  RefreshCw,
  Shield,
  Zap,
} from "lucide-react";
import {
  fetchLogs,
  fetchOverview,
  fetchProxies,
  type AuditEntry,
  type OverviewResponse,
  type ProxyNodeStatus,
} from "../api/client";
import {
  DualThroughputChart,
  formatClockTime,
  LogDetailModal,
  NoticeBanner,
  SparklineSVG,
  type RouteKey,
} from "../components/ui";

function buildThroughputSeries(logs: AuditEntry[]) {
  const buckets = 12;
  const chatSeries = new Array<number>(buckets).fill(0);
  const mediaSeries = new Array<number>(buckets).fill(0);
  const latencyTrend = new Array<number>(buckets).fill(0);
  const latencyCounts = new Array<number>(buckets).fill(0);

  if (logs.length === 0) {
    return {
      chatSeries,
      mediaSeries,
      latencyTrend: [0, 0, 0, 0, 0, 0, 0, 0],
      chatTotal: 0,
      mediaTotal: 0,
      p95Ms: 0,
    };
  }

  // Chronological oldest -> newest
  const ordered = [...logs].reverse();
  const durations: number[] = [];
  let chatTotal = 0;
  let mediaTotal = 0;

  ordered.forEach((entry, idx) => {
    const bucketIdx = Math.min(
      buckets - 1,
      Math.floor((idx / Math.max(1, ordered.length)) * buckets)
    );
    const p = entry.path || "";
    const isMedia =
      p.includes("/images/") || p.includes("/audio/");
    if (isMedia) {
      mediaSeries[bucketIdx] += 1;
      mediaTotal += 1;
    } else if (entry.kind !== "event") {
      chatSeries[bucketIdx] += 1;
      chatTotal += 1;
    }

    if (typeof entry.duration_ms === "number" && entry.duration_ms > 0) {
      durations.push(entry.duration_ms);
      latencyTrend[bucketIdx] += entry.duration_ms;
      latencyCounts[bucketIdx] += 1;
    }
  });

  const sortedDur = [...durations].sort((a, b) => a - b);
  const p95Ms =
    sortedDur.length > 0
      ? sortedDur[Math.min(sortedDur.length - 1, Math.floor(sortedDur.length * 0.95))]
      : 0;

  const avgLatSeries = latencyTrend.map((sum, i) =>
    latencyCounts[i] > 0 ? Math.round(sum / latencyCounts[i]) : 0
  );

  return {
    chatSeries,
    mediaSeries,
    latencyTrend: avgLatSeries,
    chatTotal,
    mediaTotal,
    p95Ms,
  };
}

export const OverviewView: React.FC<{
  onNavigate: (route: RouteKey) => void;
  onOpenExport: () => void;
}> = ({ onNavigate, onOpenExport }) => {
  const [overview, setOverview] = useState<OverviewResponse | null>(null);
  const [recentLogs, setRecentLogs] = useState<AuditEntry[]>([]);
  const [trendLogs, setTrendLogs] = useState<AuditEntry[]>([]);
  const [proxyNodes, setProxyNodes] = useState<ProxyNodeStatus[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copiedEndpoint, setCopiedEndpoint] = useState(false);
  const [selectedLog, setSelectedLog] = useState<AuditEntry | null>(null);

  const load = async () => {
    setLoading(true);
    setError(null);
    try {
      const [ov, logsRes, proxiesRes] = await Promise.all([
        fetchOverview(),
        fetchLogs({ filter: "all", limit: 60 }),
        fetchProxies(),
      ]);
      setOverview(ov);
      const entries = logsRes.entries || [];
      setTrendLogs(entries);
      setRecentLogs(entries.slice(0, 6));
      setProxyNodes(proxiesRes.nodes || []);
    } catch (err: any) {
      setError(err?.message || "加载网关总览失败");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
    const timer = window.setInterval(load, 15000);
    const onPoolUpdated = () => load();
    window.addEventListener("pool-updated", onPoolUpdated);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("pool-updated", onPoolUpdated);
    };
  }, []);

  const chatgpt = overview?.providers?.find(
    (p) => p.descriptor.id === "chatgpt"
  );
  const poolStats = chatgpt?.stats;
  const activeCount = poolStats?.active ?? 0;
  const totalCount = poolStats?.total ?? 0;
  const imgLimited = poolStats?.image_limited ?? 0;
  const imgReady = Math.max(0, activeCount - imgLimited);
  const greenBarPct = totalCount > 0 ? (imgReady / totalCount) * 100 : 0;
  const amberBarPct = totalCount > 0 ? (imgLimited / totalCount) * 100 : 0;

  const proxySum = overview?.proxies_summary ?? {
    total: overview?.proxies_count ?? 0,
    active: overview?.proxies_count ?? 0,
    cooling: 0,
  };
  const measuredProxies = proxyNodes.filter((n) => n.latency_ms > 0);
  const avgProxyMs =
    measuredProxies.length > 0
      ? Math.round(
          measuredProxies.reduce((sum, n) => sum + n.latency_ms, 0) /
            measuredProxies.length
        )
      : 0;

  const worker = overview?.kami_worker;
  const auditSum = overview?.audit_summary;

  const totalReqs = auditSum?.total_requests ?? 0;
  const failedReqs = auditSum?.failed_requests ?? 0;
  const successRate =
    totalReqs > 0
      ? `${(((totalReqs - failedReqs) / totalReqs) * 100).toFixed(1)}%`
      : "100.0%";

  const {
    chatSeries,
    mediaSeries,
    latencyTrend,
    chatTotal,
    mediaTotal,
    p95Ms,
  } = useMemo(() => buildThroughputSeries(trendLogs), [trendLogs]);

  const apiEndpoint = `${window.location.origin}/v1`;

  const handleCopyEndpoint = async () => {
    try {
      await navigator.clipboard.writeText(apiEndpoint);
      setCopiedEndpoint(true);
      window.setTimeout(() => setCopiedEndpoint(false), 1600);
    } catch {
      // ignore
    }
  };

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">
            网关运行总览
          </h1>
          <p className="text-xs z-text-muted mt-1">
            实时观测多供应商免费号池健康度、无损 Failover 自愈率、出口代理延迟与多模态能力矩阵。
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2 text-xs">
          <button
            type="button"
            onClick={load}
            disabled={loading}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
          >
            <RefreshCw size={13} className={loading ? "animate-spin" : ""} />
            <span>刷新数据</span>
          </button>
          <button
            type="button"
            onClick={() => onNavigate("logs")}
            className="px-3 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
          >
            Trace 审计
          </button>
          <button
            type="button"
            onClick={onOpenExport}
            className="inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl z-primary-btn font-medium cursor-pointer"
          >
            <Download size={13} />
            <span>导出数据包</span>
          </button>
        </div>
      </div>

      {error && (
        <NoticeBanner
          type="error"
          text={error}
          onClose={() => setError(null)}
        />
      )}

      {/* 4 Telemetry KPI Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3.5">
        <div className="p-card rounded-2xl p-4 space-y-2.5 text-xs">
          <div className="flex items-center justify-between">
            <span className="z-text-muted">号池总可用率 (ChatGPT)</span>
            <span
              className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full border font-medium ${
                activeCount > 0 ? "z-pill-success" : "z-pill-warn"
              }`}
            >
              <span
                className={
                  activeCount > 0 ? "status-dot-green" : "status-dot-amber"
                }
              />
              <span>{activeCount > 0 ? "活跃" : "待补充"}</span>
            </span>
          </div>
          <div className="flex items-baseline justify-between gap-2">
            <div className="flex items-baseline gap-1.5">
              <span className="text-2xl font-semibold font-mono-num tracking-tight">
                {poolStats ? activeCount : "--"}
              </span>
              <span className="font-mono-num z-text-muted">
                / {poolStats ? totalCount : "--"}
              </span>
            </div>
            <span className="font-mono-num z-text-muted">
              429: {poolStats?.rate_limited ?? 0} · 过期{" "}
              {poolStats?.expired ?? 0}
            </span>
          </div>
          <div className="h-1.5 w-full rounded-full overflow-hidden flex gap-0.5 z-muted">
            <div
              className="h-full rounded-l-full"
              style={{ width: `${greenBarPct}%`, background: "#10b981" }}
            />
            {amberBarPct > 0 && (
              <div
                className="h-full rounded-r-full"
                style={{ width: `${amberBarPct}%`, background: "#f59e0b" }}
              />
            )}
          </div>
          <div className="flex items-center justify-between z-text-muted">
            <span>对话可用 {activeCount}</span>
            <span>
              生图 {imgReady} · 冷却 {imgLimited}
            </span>
          </div>
        </div>

        <div className="p-card rounded-2xl p-4 space-y-2.5 text-xs">
          <div className="flex items-center justify-between">
            <span className="z-text-muted">请求成功率 · 自愈</span>
            <span className="px-2 py-0.5 rounded-full border z-pill-info font-mono-num font-medium">
              +{auditSum?.failover_recovered ?? 0} 救回
            </span>
          </div>
          <div className="flex items-end justify-between gap-2">
            <div className="flex items-baseline gap-0.5">
              <span className="text-2xl font-semibold font-mono-num tracking-tight">
                {successRate.replace("%", "")}
              </span>
              <span className="text-sm font-mono-num z-text-muted">%</span>
            </div>
            <SparklineSVG
              values={
                chatSeries.some((v) => v > 0)
                  ? chatSeries
                  : [1, 1, 1, 1, 1, 1]
              }
              color="#10b981"
            />
          </div>
          <div className="z-text-muted font-mono-num">
            共 {totalReqs} 次请求 · 失败 {failedReqs} 次
          </div>
        </div>

        <div className="p-card rounded-2xl p-4 space-y-2.5 text-xs">
          <div className="flex items-center justify-between">
            <span className="z-text-muted">出口代理健康度</span>
            <span
              className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full border font-medium ${
                proxySum.cooling > 0
                  ? "z-pill-warn"
                  : proxySum.total > 0
                    ? "z-pill-success"
                    : "z-muted z-border"
              }`}
            >
              <span
                className={
                  proxySum.cooling > 0
                    ? "status-dot-amber"
                    : proxySum.total > 0
                      ? "status-dot-green"
                      : "status-dot-muted"
                }
              />
              <span>
                {proxySum.total > 0
                  ? `${proxySum.active}/${proxySum.total} 健康`
                  : "直连模式"}
              </span>
            </span>
          </div>
          <div className="flex items-end justify-between gap-2">
            <div className="flex items-baseline gap-1">
              <span className="text-2xl font-semibold font-mono-num tracking-tight">
                {avgProxyMs > 0
                  ? avgProxyMs
                  : proxySum.total > 0
                    ? `${proxySum.active}/${proxySum.total}`
                    : "直连"}
              </span>
              {avgProxyMs > 0 && (
                <span className="font-mono-num z-text-muted">ms</span>
              )}
            </div>
            <SparklineSVG
              values={
                latencyTrend.some((v) => v > 0)
                  ? latencyTrend
                  : measuredProxies.map((p) => p.latency_ms)
              }
              color="#38bdf8"
            />
          </div>
          <div className="z-text-muted font-mono-num truncate">
            {overview?.proxies?.[0] || "粘性绑定 · 连续 3 次异常自动漂移"}
          </div>
        </div>

        <div className="p-card rounded-2xl p-4 space-y-2.5 text-xs">
          <div className="flex items-center justify-between">
            <span className="z-text-muted">卡密续期 Worker</span>
            <span
              className={`px-2 py-0.5 rounded-full border font-medium ${
                worker?.running
                  ? "z-pill-info"
                  : (worker?.queue_length ?? 0) > 0
                    ? "z-pill-warn"
                    : "z-muted z-border z-text-sec"
              }`}
            >
              {worker?.running
                ? "续期执行中"
                : (worker?.queue_length ?? 0) > 0
                  ? "排队待执行"
                  : "串行队列"}
            </span>
          </div>
          <div className="flex items-baseline justify-between gap-2">
            <div className="flex items-baseline gap-1.5">
              <span className="text-2xl font-semibold font-mono-num tracking-tight">
                {worker ? worker.queue_length : 0}
              </span>
              <span className="z-text-muted">排队中</span>
            </div>
            <span className="font-mono-num z-text-muted">
              成功 {worker?.total_succeeded ?? 0} · 失败{" "}
              {worker?.total_failed ?? 0}
            </span>
          </div>
          <div className="z-text-muted truncate font-mono-num">
            {worker?.running && worker.running_email
              ? `当前登录: ${worker.running_email}`
              : "剩余 ≤ 24h 自动触发无头续期"}
          </div>
        </div>
      </div>

      {/* Main Grid: Throughput Area Chart + Event Feed vs Endpoint & Capabilities */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <div className="lg:col-span-2 p-card rounded-2xl p-4 space-y-4">
          <div className="flex items-center justify-between gap-2">
            <div>
              <h2 className="text-sm font-semibold flex items-center gap-1.5">
                <Activity size={14} />
                <span>路由吞吐与 Failover 实时事件</span>
              </h2>
              <p className="text-xs z-text-muted mt-0.5">
                基于最近 {trendLogs.length} 条内存环形缓冲轨迹 · 点击任意条目检视 Span 详情
              </p>
            </div>
            <button
              type="button"
              onClick={() => onNavigate("logs")}
              className="inline-flex items-center gap-1 text-xs z-text-muted hover:underline cursor-pointer shrink-0"
            >
              <span>完整日志</span>
              <ArrowRight size={12} />
            </button>
          </div>

          <DualThroughputChart
            primarySeries={chatSeries}
            secondarySeries={mediaSeries}
            primaryLabel={`对话 / Responses (${chatTotal})`}
            secondaryLabel={`生图 / TTS / STT (${mediaTotal})`}
            rightHint={p95Ms > 0 ? `P95 · ${(p95Ms / 1000).toFixed(2)}s` : "等待实时流量"}
          />

          <div className="divide-y z-border text-xs">
            {recentLogs.length === 0 && (
              <div className="py-6 text-center z-text-muted">
                暂无请求或运维事件记录，发起对话、生图或测活操作后将实时呈现在此处。
              </div>
            )}
            {recentLogs.map((entry) => {
              const isEvent = entry.kind === "event";
              const okStatus = !entry.status || entry.status < 400;
              const dotCls = isEvent
                ? "status-dot-green"
                : entry.failover_trace
                  ? "status-dot-amber"
                  : okStatus
                    ? "status-dot-green"
                    : "status-dot-red";

              return (
                <div
                  key={entry.id}
                  onClick={() => setSelectedLog(entry)}
                  className="py-2.5 px-2 -mx-2 rounded-xl flex items-center justify-between gap-3 z-row-hover cursor-pointer"
                >
                  <div className="flex items-center gap-2.5 min-w-0">
                    <span className={dotCls} />
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-mono-num font-medium truncate">
                          {entry.path || "--"}
                        </span>
                        {entry.failover_trace && (
                          <span className="px-1.5 py-0.5 rounded border z-pill-warn font-mono-num">
                            Failover ×{entry.attempt_count || 2}
                          </span>
                        )}
                        {!isEvent && entry.status && (
                          <span
                            className={`px-1.5 py-0.5 rounded border font-mono-num ${
                              okStatus ? "z-pill-success" : "z-pill-danger"
                            }`}
                          >
                            {entry.status}
                          </span>
                        )}
                      </div>
                      <div className="z-text-muted font-mono-num truncate mt-0.5">
                        {entry.failover_trace ||
                          entry.error ||
                          entry.note ||
                          `${entry.model || entry.kind} · ${
                            entry.account_email || entry.account_id || "匿名会话"
                          }`}
                      </div>
                    </div>
                  </div>
                  <div className="text-right font-mono-num shrink-0">
                    <div>
                      {entry.duration_ms !== undefined
                        ? `${entry.duration_ms}ms`
                        : "--"}
                    </div>
                    <div className="z-text-muted">
                      {formatClockTime(entry.timestamp)}
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        {/* Right Column: Endpoint & Capability Matrix */}
        <div className="p-card rounded-2xl p-4 flex flex-col justify-between gap-4 text-xs">
          <div className="space-y-3.5">
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-semibold flex items-center gap-1.5">
                <Shield size={14} />
                <span>网关接入与能力矩阵</span>
              </h2>
              <span className="font-mono-num px-2 py-0.5 rounded-full border z-border z-muted z-text-muted">
                v{overview?.version || "2.6"} · CGO=0
              </span>
            </div>

            <div className="p-2.5 rounded-xl p-code-box font-mono-num flex items-center justify-between gap-2">
              <span className="truncate">{apiEndpoint}</span>
              <button
                type="button"
                onClick={handleCopyEndpoint}
                className="px-2 py-1 rounded-lg border z-border z-surface z-ghost-btn cursor-pointer shrink-0 inline-flex items-center gap-1"
              >
                {copiedEndpoint ? <Check size={12} /> : <Copy size={12} />}
                <span>{copiedEndpoint ? "已复制" : "复制"}</span>
              </button>
            </div>

            <div className="grid grid-cols-2 gap-2">
              <div
                className="p-2.5 rounded-xl"
                style={{
                  background: "var(--bg-input)",
                  border: "1px solid var(--hairline)",
                }}
              >
                <div className="z-text-muted">对话 / Responses</div>
                <div className="font-medium mt-1 flex items-center gap-1.5">
                  <span className="status-dot-green" />
                  <span>{activeCount} 账号</span>
                </div>
              </div>
              <div
                className="p-2.5 rounded-xl"
                style={{
                  background: "var(--bg-input)",
                  border: "1px solid var(--hairline)",
                }}
              >
                <div className="z-text-muted">Tool Calling</div>
                <div className="font-medium mt-1 flex items-center gap-1.5">
                  <span className="status-dot-green" />
                  <span>就绪</span>
                </div>
              </div>
              <div
                className="p-2.5 rounded-xl"
                style={{
                  background: "var(--bg-input)",
                  border: "1px solid var(--hairline)",
                }}
              >
                <div className="z-text-muted">GPT-Image / DALL-E</div>
                <div className="font-medium mt-1 flex items-center gap-1.5">
                  <span
                    className={
                      imgReady > 0 ? "status-dot-green" : "status-dot-amber"
                    }
                  />
                  <span>{imgReady} 账号</span>
                </div>
              </div>
              <div
                className="p-2.5 rounded-xl"
                style={{
                  background: "var(--bg-input)",
                  border: "1px solid var(--hairline)",
                }}
              >
                <div className="z-text-muted">TTS / Whisper STT</div>
                <div className="font-medium mt-1 flex items-center gap-1.5">
                  <span className="status-dot-green" />
                  <span>{activeCount} 账号</span>
                </div>
              </div>
              <div
                className="p-2.5 rounded-xl"
                style={{
                  background: "var(--bg-input)",
                  border: "1px solid var(--hairline)",
                }}
              >
                <div className="z-text-muted">ADMIN_TOKEN</div>
                <div className="font-medium mt-1 flex items-center gap-1.5">
                  <span
                    className={
                      overview?.admin_auth_required
                        ? "status-dot-green"
                        : "status-dot-amber"
                    }
                  />
                  <span>
                    {overview?.admin_auth_required ? "已锁定保护" : "免密直入"}
                  </span>
                </div>
              </div>
              <div
                className="p-2.5 rounded-xl"
                style={{
                  background: "var(--bg-input)",
                  border: "1px solid var(--hairline)",
                }}
              >
                <div className="z-text-muted">产物持久化</div>
                <div className="font-medium mt-1 flex items-center gap-1.5">
                  <span className="status-dot-green" />
                  <span>IndexedDB 本地库</span>
                </div>
              </div>
            </div>
          </div>

          <div className="space-y-2.5 pt-2 border-t z-border">
            <div className="grid grid-cols-2 gap-2">
              <button
                type="button"
                onClick={() => onNavigate("chat")}
                className="py-2 rounded-xl border z-border z-ghost-btn font-medium cursor-pointer"
              >
                打开 OpenUI 操场
              </button>
              <button
                type="button"
                onClick={() => onNavigate("image")}
                className="py-2 rounded-xl z-primary-btn font-medium inline-flex items-center justify-center gap-1 cursor-pointer"
              >
                <Zap size={12} />
                <span>打开图像工坊</span>
              </button>
            </div>

            <button
              type="button"
              onClick={() => onNavigate("network")}
              className="w-full p-2.5 rounded-xl flex items-start gap-2 text-left cursor-pointer z-ghost-btn"
              style={{
                background: "var(--bg-input)",
                border: "1px solid var(--hairline)",
              }}
            >
              <span className="px-1.5 py-0.5 rounded border z-pill-info font-mono-num shrink-0">
                模型目录
              </span>
              <span className="z-text-muted leading-relaxed">
                已支持 <code className="font-mono-num">?include_builtins=true</code>{" "}
                聚合实时对话 + 生图 + TTS / Whisper 全部模型，点击查看 →
              </span>
            </button>
          </div>
        </div>
      </div>

      <LogDetailModal
        entry={selectedLog}
        onClose={() => setSelectedLog(null)}
      />
    </div>
  );
};
